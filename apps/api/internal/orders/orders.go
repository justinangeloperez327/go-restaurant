package orders

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "regexp"
 "strconv"
 "strings"
 "time"

 "github.com/go-chi/chi/v5"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
)
type Service struct {
 DB *pgxpool.Pool
 Auth *auth.Service
 WebOrigin string
 Stripe StripeGateway
 AllowCOD bool
 TaxBPS int64
 DeliveryFeeMinor int64
}
type StripeGateway interface {
 CreateCheckout(context.Context, StripeCheckoutRequest) (StripeCheckoutSession,error)
}
type StripeCheckoutRequest struct {
 OrderID string
 AmountMinor int64
 Currency string
 IdempotencyKey string
 SuccessURL string
 CancelURL string
}
type StripeCheckoutSession struct {
 ID string `json:"id"`
 URL string `json:"url"`
}
type CheckoutRequest struct {
 AddressID string `json:"address_id"`
 PaymentMethod string `json:"payment_method"`
 Notes string `json:"notes"`
}
type Quote struct {
 RestaurantID string `json:"restaurant_id"`
 Currency string `json:"currency"`
 SubtotalMinor int64 `json:"subtotal_minor"`
 DeliveryFeeMinor int64 `json:"delivery_fee_minor"`
 TaxMinor int64 `json:"tax_minor"`
 TotalMinor int64 `json:"total_minor"`
 Items []OrderLine `json:"items"`
}
type OrderLine struct {
 MealID string `json:"meal_id"`
 MealName string `json:"meal_name"`
 Quantity int `json:"quantity"`
 BasePriceMinor int64 `json:"base_price_minor"`
 UnitPriceMinor int64 `json:"unit_price_minor"`
 LineTotalMinor int64 `json:"line_total_minor"`
 Modifiers []customer.OptionSummary `json:"selected_modifiers"`
}
type Order struct {
 ID string `json:"id"`
 RestaurantID string `json:"restaurant_id"`
 RestaurantName string `json:"restaurant_name"`
 PaymentMethod string `json:"payment_method"`
 OrderStatus string `json:"order_status"`
 PaymentStatus string `json:"payment_status"`
 Currency string `json:"currency"`
 SubtotalMinor int64 `json:"subtotal_minor"`
 DeliveryFeeMinor int64 `json:"delivery_fee_minor"`
 TaxMinor int64 `json:"tax_minor"`
 TotalMinor int64 `json:"total_minor"`
 Notes string `json:"notes"`
 DeliveryAddress json.RawMessage `json:"delivery_address"`
 Items []OrderLine `json:"items"`
 CheckoutURL string `json:"checkout_url,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}
var uuidRE=regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var keyRE=regexp.MustCompile(`^[a-zA-Z0-9_-]{12,128}$`)
var ErrInvalidCart=errors.New("cart is empty, changed or contains unavailable items")
var ErrAddress=errors.New("delivery address is unavailable")
func respond(w http.ResponseWriter,code int,v any) {
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 w.WriteHeader(code)
 _=json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter,code int,message string) {respond(w,code,map[string]string{"error":message})}
func parse(w http.ResponseWriter,r *http.Request,v any) error {
 if r.Header.Get("Content-Type")!="application/json" {return errors.New("expected application/json")}
 r.Body=http.MaxBytesReader(w,r.Body,8192)
 d:=json.NewDecoder(r.Body);d.DisallowUnknownFields()
 if err:=d.Decode(v);err!=nil{return err}
 if err:=d.Decode(&struct{}{});err!=io.EOF{return errors.New("extra JSON")}
 return nil
}
func owner(r *http.Request) string {u,_:=auth.UserFromContext(r.Context());return u.ID}
func (s Service) safeOrigin(r *http.Request) bool {return s.WebOrigin!=""&&r.Header.Get("Origin")==s.WebOrigin}
func (s Service) Mount(r chi.Router) {
 r.With(s.Auth.Authenticated).Get("/checkout/config",s.methods)
 r.With(s.Auth.Authenticated).Get("/checkout/quote",s.quote)
 r.With(s.Auth.Authenticated).Post("/checkout",s.checkout)
 r.With(s.Auth.Authenticated).Get("/orders",s.listOrders)
 r.With(s.Auth.Authenticated).Get("/orders/{orderID}",s.getOrder)
 // Stripe webhooks are authenticated by their signature, not user cookies.
 r.Post("/webhooks/stripe",s.handleStripeWebhook)
}
func (s Service) methods(w http.ResponseWriter,r *http.Request) {
 respond(w,200,map[string]bool{"cod":s.AllowCOD,"stripe":s.Stripe!=nil})
}
func (s Service) loadQuote(ctx context.Context,tx pgx.Tx,userID,addressID string) (Quote,json.RawMessage,error) {
 q:=Quote{Items:[]OrderLine{}}
 if !uuidRE.MatchString(addressID) {return q,nil,ErrAddress}
 var address []byte
 err:=tx.QueryRow(ctx,`SELECT to_jsonb(a)-'id'-'user_id'-'is_default'-'created_at'-'updated_at'
 FROM delivery_addresses a WHERE a.id=$1::uuid AND a.user_id=$2::uuid
 AND a.country_code='AE'`,addressID,userID).Scan(&address)
 if errors.Is(err,pgx.ErrNoRows){return q,nil,ErrAddress}
 if err!=nil{return q,nil,err}
 err=tx.QueryRow(ctx,`SELECT c.restaurant_id::text,r.currency
 FROM customer_carts c JOIN restaurants r ON r.id=c.restaurant_id
 WHERE c.user_id=$1::uuid AND r.is_active`,userID).Scan(&q.RestaurantID,&q.Currency)
 if errors.Is(err,pgx.ErrNoRows){return q,nil,ErrInvalidCart}
 if err!=nil{return q,nil,err}
 if q.Currency!="AED" {return q,nil,ErrInvalidCart} // AED-only pricing/tax/Stripe integration.
 rows,err:=tx.Query(ctx,`SELECT ci.meal_id::text,m.name,m.price_minor,ci.quantity,ci.id::text
 FROM cart_items ci JOIN meals m ON m.id=ci.meal_id AND m.restaurant_id=ci.restaurant_id
 WHERE ci.user_id=$1::uuid ORDER BY ci.created_at,ci.id`,userID)
 if err!=nil{return q,nil,err}
 type cartRow struct{mealID,name,id string;base int64;qty int}
 var lines []cartRow
 for rows.Next(){
  var x cartRow
  if err:=rows.Scan(&x.mealID,&x.name,&x.base,&x.qty,&x.id);err!=nil{rows.Close();return q,nil,err}
  lines=append(lines,x)
 }
 err=rows.Err();rows.Close()
 if err!=nil{return q,nil,err}
 if len(lines)==0 ||len(lines)>40{return q,nil,ErrInvalidCart}
 count:=0
 for _,line:=range lines {
  count+=line.qty
  if line.qty<1||line.qty>50||count>100{return q,nil,ErrInvalidCart}
  rows,err:=tx.Query(ctx,`SELECT option_id::text FROM cart_item_options WHERE cart_item_id=$1::uuid ORDER BY option_id`,line.id)
  if err!=nil{return q,nil,err}
  ids:=[]string{}
  for rows.Next(){var v string;if err:=rows.Scan(&v);err!=nil{rows.Close();return q,nil,err};ids=append(ids,v)}
  err=rows.Err();rows.Close()
  if err!=nil{return q,nil,err}
  if len(ids)>30{return q,nil,ErrInvalidCart}
  // Revalidate and price every line within the checkout transaction.
  unit,mods,err:=customer.PriceSelection(ctx,tx,q.RestaurantID,line.mealID,ids)
  if customer.IsUnavailable(err){return q,nil,ErrInvalidCart}
  if err!=nil{return q,nil,err}
  if unit<0||unit>35000000000{return q,nil,ErrInvalidCart}
  q.Items=append(q.Items,OrderLine{MealID:line.mealID,MealName:line.name,Quantity:line.qty,
   BasePriceMinor:line.base,UnitPriceMinor:unit,LineTotalMinor:unit*int64(line.qty),Modifiers:mods})
  q.SubtotalMinor+=unit*int64(line.qty)
 }
 if q.SubtotalMinor<1{return q,nil,ErrInvalidCart}
 q.DeliveryFeeMinor=s.DeliveryFeeMinor
 q.TaxMinor=(q.SubtotalMinor+q.DeliveryFeeMinor)*s.TaxBPS/10000
 q.TotalMinor=q.SubtotalMinor+q.DeliveryFeeMinor+q.TaxMinor
 if q.TotalMinor>99999999999999{return q,nil,ErrInvalidCart}
 return q,address,nil
}
func (s Service) quote(w http.ResponseWriter,r *http.Request) {
 addressID:=r.URL.Query().Get("address_id")
 if !uuidRE.MatchString(addressID){fail(w,400,"choose a valid delivery address");return}
 tx,err:=s.DB.BeginTx(r.Context(),pgx.TxOptions{IsoLevel:pgx.RepeatableRead,AccessMode:pgx.ReadOnly})
 if err!=nil {fail(w,503,"database unavailable");return}
 defer func(){_=tx.Rollback(r.Context())}()
 q,_,err:=s.loadQuote(r.Context(),tx,owner(r),addressID)
 if err!=nil {s.quoteError(w,err);return}
 if err:=tx.Commit(r.Context());err!=nil {fail(w,503,"database unavailable");return}
 respond(w,200,q)
}
func (s Service) quoteError(w http.ResponseWriter,err error){
 switch {
 case errors.Is(err,ErrAddress):fail(w,404,"address not found or unsupported")
 case errors.Is(err,ErrInvalidCart):fail(w,409,"cart empty, invalid or unavailable")
 default:fail(w,503,"cannot calculate checkout")
 }
}
func (s Service) checkout(w http.ResponseWriter,r *http.Request){
 if !s.safeOrigin(r){fail(w,403,"invalid origin");return}
 key:=r.Header.Get("Idempotency-Key")
 if !keyRE.MatchString(key){fail(w,400,"Idempotency-Key must be 12-128 alphanumeric, dash or underscore characters");return}
 var input CheckoutRequest
 if err:=parse(w,r,&input);err!=nil||!uuidRE.MatchString(input.AddressID)||len([]rune(input.Notes))>500||
 (input.PaymentMethod!="cod"&&input.PaymentMethod!="stripe"){fail(w,400,"invalid checkout request");return}
 if input.PaymentMethod=="cod"&&!s.AllowCOD{fail(w,503,"cash on delivery disabled");return}
 if input.PaymentMethod=="stripe"&&s.Stripe==nil{fail(w,503,"online payments not configured");return}
 ctx:=r.Context()
 tx,err:=s.DB.Begin(ctx)
 if err!=nil{fail(w,503,"database unavailable");return}
 defer func(){_=tx.Rollback(ctx)}()
 userID:=owner(r)
 // User row serializes identical and different idempotency keys, including cart clearing.
 var active string
 err=tx.QueryRow(ctx,"SELECT id::text FROM users WHERE id=$1::uuid AND is_active FOR UPDATE",userID).Scan(&active)
 if err!=nil{fail(w,401,"account unavailable");return}
 var id,addr,method,notes,checkoutURL string
 var total int64
 err=tx.QueryRow(ctx,`SELECT id::text,COALESCE(address_id::text,''),payment_method,order_status,
 stripe_checkout_url,total_minor,notes FROM orders WHERE user_id=$1::uuid AND idempotency_key=$2`,userID,key).
 Scan(&id,&addr,&method,&checkoutURL,&total,&notes)
 if err==nil {
  if addr!=input.AddressID||method!=input.PaymentMethod||notes!=input.Notes {fail(w,409,"idempotency key already used for different checkout");return}
  if err=tx.Commit(ctx);err!=nil{fail(w,503,"database unavailable");return}
  s.finishCheckout(w,r,id,input.PaymentMethod,total,checkoutURL)
  return
 }
 if !errors.Is(err,pgx.ErrNoRows){fail(w,503,"database unavailable");return}
 var restaurantID string
 err=tx.QueryRow(ctx,"SELECT restaurant_id::text FROM customer_carts WHERE user_id=$1::uuid FOR UPDATE",userID).Scan(&restaurantID)
 if errors.Is(err,pgx.ErrNoRows){fail(w,409,"cart is empty");return}
 if err!=nil{fail(w,503,"database unavailable");return}
 q,address,err:=s.loadQuote(ctx,tx,userID,input.AddressID)
 if err!=nil{s.quoteError(w,err);return}
 // Stripe UAE minimum amount; reject before persisting an unpayable order.
 if input.PaymentMethod=="stripe"&&q.TotalMinor<200 {fail(w,409,"online payment minimum is AED 2.00");return}
 orderStatus,paymentStatus:="placed","unpaid"
 if input.PaymentMethod=="stripe" {orderStatus,paymentStatus="pending_payment","pending"}
 err=tx.QueryRow(ctx,`INSERT INTO orders (user_id,restaurant_id,address_id,delivery_address,notes,idempotency_key,
 payment_method,order_status,payment_status,currency,subtotal_minor,delivery_fee_minor,tax_minor,total_minor)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id::text`,
 userID,q.RestaurantID,input.AddressID,address,input.Notes,key,input.PaymentMethod,orderStatus,paymentStatus,
 q.Currency,q.SubtotalMinor,q.DeliveryFeeMinor,q.TaxMinor,q.TotalMinor).Scan(&id)
 if err!=nil{fail(w,503,"unable to create order");return}
 for _,line:=range q.Items {
  mods,err:=json.Marshal(line.Modifiers);if err!=nil{fail(w,503,"unable to snapshot order");return}
  _,err=tx.Exec(ctx,`INSERT INTO order_items(order_id,meal_id,meal_name,quantity,base_price_minor,
  unit_price_minor,line_total_minor,selected_modifiers)
  VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8::jsonb)`,
  id,line.MealID,line.MealName,line.Quantity,line.BasePriceMinor,line.UnitPriceMinor,line.LineTotalMinor,mods)
  if err!=nil{fail(w,503,"unable to snapshot order");return}
 }
 if _,err=tx.Exec(ctx,"DELETE FROM customer_carts WHERE user_id=$1::uuid",userID);err!=nil{fail(w,503,"unable to finalize cart");return}
 if err=tx.Commit(ctx);err!=nil{fail(w,503,"unable to finalize checkout");return}
 s.finishCheckout(w,r,id,input.PaymentMethod,q.TotalMinor,"")
}
func (s Service) finishCheckout(w http.ResponseWriter,r *http.Request,id,method string,total int64,url string) {
 if method=="cod" {respond(w,200,map[string]any{"order_id":id,"payment_method":"cod","payment_status":"unpaid","redirect_url":""});return}
 if url!="" {respond(w,200,map[string]any{"order_id":id,"payment_method":"stripe","payment_status":"pending","redirect_url":url});return}
 session,err:=s.Stripe.CreateCheckout(r.Context(),StripeCheckoutRequest{
  OrderID:id,AmountMinor:total,Currency:"aed",IdempotencyKey:"go-restaurant-"+id,
  SuccessURL:s.WebOrigin+"/orders/"+id+"?payment=returned",
  CancelURL:s.WebOrigin+"/orders/"+id+"?payment=cancelled",
 })
 if err!=nil {fail(w,502,"payment provider unavailable; retry checkout with the same idempotency key");return}
 if !strings.HasPrefix(session.URL,"https://checkout.stripe.com/")||session.ID==""{
  fail(w,502,"invalid payment provider session");return
 }
 _,err=s.DB.Exec(r.Context(),`UPDATE orders SET stripe_session_id=$2,stripe_checkout_url=$3,updated_at=now()
 WHERE id=$1::uuid AND payment_method='stripe' AND payment_status='pending'
 AND (stripe_session_id IS NULL OR stripe_session_id=$2)`,id,session.ID,session.URL)
 if err!=nil {fail(w,503,"cannot save payment session; retry same idempotency key");return}
 respond(w,200,map[string]any{"order_id":id,"payment_method":"stripe","payment_status":"pending","redirect_url":session.URL})
}
func (s Service) listOrders(w http.ResponseWriter,r *http.Request){
 rows,err:=s.DB.Query(r.Context(),`SELECT o.id::text,o.restaurant_id::text,r.name,o.order_status,
 o.payment_method,o.payment_status,o.currency,o.total_minor,o.created_at FROM orders o
 JOIN restaurants r ON r.id=o.restaurant_id
 WHERE o.user_id=$1::uuid ORDER BY o.created_at DESC,o.id DESC LIMIT 50`,owner(r))
 if err!=nil{fail(w,503,"orders unavailable");return}
 defer rows.Close()
 type summary struct{
  ID string `json:"id"`;RestaurantID string `json:"restaurant_id"`;RestaurantName string `json:"restaurant_name"`
  OrderStatus string `json:"order_status"`;PaymentMethod string `json:"payment_method"`
  PaymentStatus string `json:"payment_status"`;Currency string `json:"currency"`
  TotalMinor int64 `json:"total_minor"`;CreatedAt time.Time `json:"created_at"`
 }
 items:=[]summary{}
 for rows.Next(){var v summary
  if err:=rows.Scan(&v.ID,&v.RestaurantID,&v.RestaurantName,&v.OrderStatus,&v.PaymentMethod,&v.PaymentStatus,&v.Currency,&v.TotalMinor,&v.CreatedAt);err!=nil{fail(w,503,"orders unavailable");return}
  items=append(items,v)
 }
 if err:=rows.Err();err!=nil{fail(w,503,"orders unavailable");return}
 respond(w,200,map[string]any{"orders":items})
}
func (s Service) getOrder(w http.ResponseWriter,r *http.Request) {
 id:=chi.URLParam(r,"orderID");if !uuidRE.MatchString(id){fail(w,400,"invalid order ID");return}
 var o Order
 err:=s.DB.QueryRow(r.Context(),`SELECT o.id::text,o.restaurant_id::text,r.name,o.payment_method,
 o.order_status,o.payment_status,o.currency,o.subtotal_minor,o.delivery_fee_minor,o.tax_minor,
 o.total_minor,o.notes,o.delivery_address,o.created_at
 FROM orders o JOIN restaurants r ON r.id=o.restaurant_id
 WHERE o.id=$1::uuid AND o.user_id=$2::uuid`,id,owner(r)).Scan(
 &o.ID,&o.RestaurantID,&o.RestaurantName,&o.PaymentMethod,&o.OrderStatus,&o.PaymentStatus,
 &o.Currency,&o.SubtotalMinor,&o.DeliveryFeeMinor,&o.TaxMinor,&o.TotalMinor,&o.Notes,&o.DeliveryAddress,&o.CreatedAt)
 if errors.Is(err,pgx.ErrNoRows){fail(w,404,"order not found");return}
 if err!=nil{fail(w,503,"order unavailable");return}
 var data []byte
 err=s.DB.QueryRow(r.Context(),`SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'meal_id',meal_id,'meal_name',meal_name,'quantity',quantity,'base_price_minor',base_price_minor,
 'unit_price_minor',unit_price_minor,'line_total_minor',line_total_minor,
 'selected_modifiers',selected_modifiers) ORDER BY id),'[]'::jsonb)
 FROM order_items WHERE order_id=$1::uuid`,id).Scan(&data)
 if err!=nil{fail(w,503,"order items unavailable");return}
 if err:=json.Unmarshal(data,&o.Items);err!=nil{fail(w,503,"invalid order snapshot");return}
 respond(w,200,o)
}
func (s Service) validate() error {
 if s.TaxBPS<0||s.TaxBPS>10000||s.DeliveryFeeMinor<0||s.DeliveryFeeMinor>100000000{return fmt.Errorf("invalid pricing configuration")}
 return nil
}
func ParseNonNegative(raw string,maximum int64)(int64,error){
 if raw==""{return 0,nil}
 n,err:=strconv.ParseInt(raw,10,64)
 if err!=nil||n<0||n>maximum{return 0,fmt.Errorf("invalid nonnegative integer configuration")}
 return n,nil
}
func (s Service) dbError(w http.ResponseWriter,err error){
 var pg *pgconn.PgError
 if errors.As(err,&pg)&&pg.Code=="23505"{fail(w,409,"duplicate checkout");return}
 fail(w,503,"checkout database unavailable")
}
