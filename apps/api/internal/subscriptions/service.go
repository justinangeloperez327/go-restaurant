package subscriptions

import (
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "regexp"
 "strings"
 "time"

 "github.com/go-chi/chi/v5"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
)

const cutoff = 24*time.Hour
var dubai = time.FixedZone("Asia/Dubai", 4*60*60)
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var clockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
var errUnavailable = errors.New("cart or menu is unavailable")
var errAddress = errors.New("delivery address unavailable")

type Service struct {
 DB *pgxpool.Pool
 Auth *auth.Service
 WebOrigin string
 AllowCOD bool
 TaxBPS int64
 DeliveryFeeMinor int64
}

type CreateInput struct {
 AddressID string `json:"address_id"`
 StartDate string `json:"start_date"`
 DeliveryTime string `json:"delivery_time"`
 Weekdays []int `json:"weekdays"`
 MaxTotalMinor int64 `json:"max_total_minor"`
 AutoRenew bool `json:"auto_renew"`
 Notes string `json:"notes"`
}
type UpdateInput struct {
 AutoRenew bool `json:"auto_renew"`
 MaxTotalMinor int64 `json:"max_total_minor"`
}
type SkipInput struct {Date string `json:"date"`}
type Subscription struct {
 ID string `json:"id"`
 RestaurantID string `json:"restaurant_id"`
 RestaurantName string `json:"restaurant_name"`
 AddressID string `json:"address_id"`
 Status string `json:"status"`
 Weekdays []int `json:"weekdays"`
 DeliveryTime string `json:"delivery_time"`
 NextDeliveryDate string `json:"next_delivery_date"`
 CurrentPeriodEnd string `json:"current_period_end"`
 AutoRenew bool `json:"auto_renew"`
 MaxTotalMinor int64 `json:"max_total_minor"`
 Notes string `json:"notes"`
 CreatedAt time.Time `json:"created_at"`
}
type occurrence struct {
 Date string `json:"date"`
 Status string `json:"status"`
 OrderID *string `json:"order_id"`
 Reason string `json:"reason"`
}

func dateOnly(t time.Time) time.Time {
 t=t.In(dubai);return time.Date(t.Year(),t.Month(),t.Day(),0,0,0,0,dubai)
}
func parseDate(s string)(time.Time,error) {
 if len(s)!=10{return time.Time{},errors.New("invalid date")}
 t,err:=time.ParseInLocation("2006-01-02",s,dubai)
 if err!=nil||t.Format("2006-01-02")!=s{return time.Time{},errors.New("invalid date")}
 return t,nil
}
func maskFromDays(days []int)(int,error){
 if len(days)==0||len(days)>7{return 0,errors.New("select weekdays")}
 mask:=0
 for _,d:=range days {
  if d<0||d>6||mask&(1<<d)!=0{return 0,errors.New("duplicate or invalid weekday")}
  mask|=1<<d
 }
 return mask,nil
}
func maskDays(mask int)[]int{
 days:=[]int{}
 for i:=0;i<7;i++{if mask&(1<<i)!=0{days=append(days,i)}}
 return days
}
func nextDate(start time.Time,mask int) time.Time {
 day:=dateOnly(start)
 for i:=0;i<7;i++{
  if mask&(1<<int(day.Weekday()))!=0{return day}
  day=day.AddDate(0,0,1)
 }
 return time.Time{}
}
func deliveryAt(date time.Time,clock string)time.Time{
 h:=(int(clock[0]-'0')*10)+int(clock[1]-'0')
 m:=(int(clock[3]-'0')*10)+int(clock[4]-'0')
 return dateOnly(date).Add(time.Duration(h)*time.Hour+time.Duration(m)*time.Minute)
}
func firstEligible(start time.Time,mask int,clock string,now time.Time)time.Time {
 day:=nextDate(start,mask)
 for i:=0;i<8;i++{
  if deliveryAt(day,clock).Add(-cutoff).After(now){return day}
  day=nextDate(day.AddDate(0,0,1),mask)
 }
 return day
}
func validMax(v int64)bool{return v>=1&&v<=1000000000}

func response(w http.ResponseWriter,code int,v any){
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.WriteHeader(code);_=json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter,code int,msg string){response(w,code,map[string]string{"error":msg})}
func parse(w http.ResponseWriter,r *http.Request,x any)error{
 if r.Header.Get("Content-Type")!="application/json"{return errors.New("expected JSON")}
 r.Body=http.MaxBytesReader(w,r.Body,16384)
 d:=json.NewDecoder(r.Body);d.DisallowUnknownFields()
 if err:=d.Decode(x);err!=nil{return err}
 if err:=d.Decode(&struct{}{});err!=io.EOF{return errors.New("trailing JSON")}
 return nil
}
func (s Service) actor(r *http.Request)string{
 u,_:=auth.UserFromContext(r.Context());return u.ID
}
func (s Service) requireOrigin(w http.ResponseWriter,r *http.Request)bool{
 if s.WebOrigin==""||r.Header.Get("Origin")!=s.WebOrigin{fail(w,403,"invalid origin");return false}
 return true
}
func (s Service) Mount(r chi.Router){
 r.With(s.Auth.Authenticated).Get("/subscriptions",s.list)
 r.With(s.Auth.Authenticated).Post("/subscriptions",s.create)
 r.With(s.Auth.Authenticated).Get("/subscriptions/{subscriptionID}",s.detail)
 r.With(s.Auth.Authenticated).Put("/subscriptions/{subscriptionID}",s.update)
 r.With(s.Auth.Authenticated).Post("/subscriptions/{subscriptionID}/pause",s.pause)
 r.With(s.Auth.Authenticated).Post("/subscriptions/{subscriptionID}/resume",s.resume)
 r.With(s.Auth.Authenticated).Post("/subscriptions/{subscriptionID}/cancel",s.cancel)
 r.With(s.Auth.Authenticated).Post("/subscriptions/{subscriptionID}/skips",s.skip)
 r.With(s.Auth.Authenticated).Get("/subscriptions/{subscriptionID}/occurrences",s.occurrences)
}
func scanSubscription(rows pgx.Rows)([]Subscription,error) {
 result:=[]Subscription{}
 for rows.Next(){
  var x Subscription
  var mask int
  var dt,period time.Time
  if err:=rows.Scan(&x.ID,&x.RestaurantID,&x.RestaurantName,&x.AddressID,&x.Status,&mask,
   &x.DeliveryTime,&dt,&period,&x.AutoRenew,&x.MaxTotalMinor,&x.Notes,&x.CreatedAt);err!=nil{return nil,err}
  x.Weekdays=maskDays(mask)
  x.NextDeliveryDate=dt.Format("2006-01-02")
  x.CurrentPeriodEnd=period.Format("2006-01-02")
  result=append(result,x)
 }
 return result,rows.Err()
}
const selectionSQL = `SELECT s.id::text,s.restaurant_id::text,r.name,s.address_id::text,s.status,
 s.weekday_mask,s.delivery_time,s.next_delivery_date,s.current_period_end,s.auto_renew,
 s.max_total_minor,s.notes,s.created_at
 FROM meal_subscriptions s JOIN restaurants r ON r.id=s.restaurant_id`
func (s Service) list(w http.ResponseWriter,r *http.Request){
 rows,err:=s.DB.Query(r.Context(),selectionSQL+` WHERE s.user_id=$1::uuid
 ORDER BY s.created_at DESC,s.id DESC LIMIT 100`,s.actor(r))
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 defer rows.Close()
 items,err:=scanSubscription(rows)
 if err!=nil{fail(w,503,"subscriptions unavailable");return}
 response(w,200,map[string]any{"subscriptions":items})
}
func (s Service) detail(w http.ResponseWriter,r *http.Request){
 id:=chi.URLParam(r,"subscriptionID")
 if !uuidRE.MatchString(id){fail(w,400,"invalid subscription ID");return}
 rows,err:=s.DB.Query(r.Context(),selectionSQL+` WHERE s.user_id=$1::uuid AND s.id=$2::uuid`,s.actor(r),id)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 items,err:=scanSubscription(rows);rows.Close()
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if len(items)==0{fail(w,404,"subscription not found");return}
 response(w,200,items[0])
}
func (s Service) occurrences(w http.ResponseWriter,r *http.Request){
 id:=chi.URLParam(r,"subscriptionID")
 if !uuidRE.MatchString(id){fail(w,400,"invalid subscription ID");return}
 var exists bool
 err:=s.DB.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM meal_subscriptions
 WHERE id=$1::uuid AND user_id=$2::uuid)`,id,s.actor(r)).Scan(&exists)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if !exists{fail(w,404,"subscription not found");return}
 rows,err:=s.DB.Query(r.Context(),`SELECT delivery_date,status,order_id::text,reason
 FROM meal_subscription_occurrences WHERE subscription_id=$1::uuid
 ORDER BY delivery_date DESC LIMIT 100`,id)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 defer rows.Close()
 out:=[]occurrence{}
 for rows.Next(){
  var day time.Time
  var o occurrence
  if err:=rows.Scan(&day,&o.Status,&o.OrderID,&o.Reason);err!=nil{fail(w,503,"subscription database unavailable");return}
  o.Date=day.Format("2006-01-02");out=append(out,o)
 }
 if err:=rows.Err();err!=nil{fail(w,503,"subscription database unavailable");return}
 response(w,200,map[string]any{"occurrences":out})
}

func (s Service) create(w http.ResponseWriter,r *http.Request){
 if !s.requireOrigin(w,r){return}
 if !s.AllowCOD{fail(w,503,"subscriptions require enabled cash on delivery");return}
 var x CreateInput
 if err:=parse(w,r,&x);err!=nil||!uuidRE.MatchString(x.AddressID)||!clockRE.MatchString(x.DeliveryTime)||
  !validMax(x.MaxTotalMinor)||len([]rune(x.Notes))>500{
  fail(w,400,"invalid subscription request");return
 }
 mask,err:=maskFromDays(x.Weekdays)
 if err!=nil{fail(w,400,"invalid weekday selection");return}
 start,err:=parseDate(x.StartDate)
 now:=time.Now()
 if err!=nil||start.Before(dateOnly(now))||start.After(dateOnly(now).AddDate(0,0,60)){
  fail(w,400,"start date must be within the next 60 days");return
 }
 next:=firstEligible(start,mask,x.DeliveryTime,now)
 periodEnd:=dateOnly(start).AddDate(0,0,7)
 // Initial week is at most seven calendar days; if the first eligible slot is
 // beyond the first week, use the next seven-day subscription cycle.
 if !next.Before(periodEnd){periodEnd=next.AddDate(0,0,7)}
 ctx:=r.Context()
 tx,err:=s.DB.Begin(ctx)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 defer func(){_=tx.Rollback(ctx)}()
 user:=s.actor(r)
 var active string
 err=tx.QueryRow(ctx,`SELECT id::text FROM users WHERE id=$1::uuid AND is_active FOR UPDATE`,user).Scan(&active)
 if err!=nil{fail(w,401,"account unavailable");return}
 var rid,currency string
 err=tx.QueryRow(ctx,`SELECT c.restaurant_id::text,r.currency FROM customer_carts c
 JOIN restaurants r ON r.id=c.restaurant_id WHERE c.user_id=$1::uuid
 AND r.is_active FOR UPDATE OF c`,user).Scan(&rid,&currency)
 if errors.Is(err,pgx.ErrNoRows){fail(w,409,"add meals to your cart first");return}
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if currency!="AED"{fail(w,409,"subscriptions currently require AED");return}
 var addressValid bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM delivery_addresses WHERE id=$1::uuid
 AND user_id=$2::uuid AND country_code='AE')`,x.AddressID,user).Scan(&addressValid)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if !addressValid{fail(w,404,"address not found or unsupported");return}
 lines,subtotal,err:=cartSnapshot(ctx,tx,user,rid)
 if errors.Is(err,errUnavailable){fail(w,409,"cart contains unavailable meals or modifiers");return}
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 total:=s.total(subtotal)
 if total<1||total>x.MaxTotalMinor{fail(w,409,"price cap must cover current menu total");return}
 var id string
 err=tx.QueryRow(ctx,`INSERT INTO meal_subscriptions(user_id,restaurant_id,address_id,weekday_mask,
 delivery_time,next_delivery_date,next_run_at,current_period_end,auto_renew,max_total_minor,notes)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6::date,$7,$8::date,$9,$10,$11)
 RETURNING id::text`,user,rid,x.AddressID,mask,x.DeliveryTime,next.Format("2006-01-02"),
 deliveryAt(next,x.DeliveryTime).Add(-cutoff),periodEnd.Format("2006-01-02"),
 x.AutoRenew,x.MaxTotalMinor,strings.TrimSpace(x.Notes)).Scan(&id)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 for _,line:=range lines {
  var itemID string
  err=tx.QueryRow(ctx,`INSERT INTO meal_subscription_items(subscription_id,meal_id,quantity)
  VALUES($1::uuid,$2::uuid,$3) RETURNING id::text`,id,line.MealID,line.Quantity).Scan(&itemID)
  if err!=nil{fail(w,503,"subscription database unavailable");return}
  for _,opt:=range line.Options {
   if _,err=tx.Exec(ctx,`INSERT INTO meal_subscription_item_options(subscription_item_id,option_id)
    VALUES($1::uuid,$2::uuid)`,itemID,opt);err!=nil{fail(w,503,"subscription database unavailable");return}
  }
 }
 if err=tx.Commit(ctx);err!=nil{fail(w,503,"subscription database unavailable");return}
 response(w,201,map[string]any{"id":id,"next_delivery_date":next.Format("2006-01-02")})
}
func (s Service) total(subtotal int64)int64{
 tax:=(subtotal+s.DeliveryFeeMinor)*s.TaxBPS/10000
 return subtotal+s.DeliveryFeeMinor+tax
}

func (s Service) action(w http.ResponseWriter,r *http.Request,name string){
 if !s.requireOrigin(w,r){return}
 id:=chi.URLParam(r,"subscriptionID")
 if !uuidRE.MatchString(id){fail(w,400,"invalid subscription ID");return}
 ctx:=r.Context();tx,err:=s.DB.Begin(ctx)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 defer func(){_=tx.Rollback(ctx)}()
 var status string
 var mask int
 var clock string
 var next,period time.Time
 var renew bool
 err=tx.QueryRow(ctx,`SELECT status,weekday_mask,delivery_time,next_delivery_date,current_period_end,auto_renew
 FROM meal_subscriptions WHERE id=$1::uuid AND user_id=$2::uuid FOR UPDATE`,id,s.actor(r)).
 Scan(&status,&mask,&clock,&next,&period,&renew)
 if errors.Is(err,pgx.ErrNoRows){fail(w,404,"subscription not found");return}
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 // PostgreSQL DATE scans represent a calendar date, not a UAE midnight instant.
 // Normalize both sides before comparing weekly period boundaries.
 next=dateOnly(next)
 period=dateOnly(period)
 switch name {
 case "pause":
  if status!="active"{fail(w,409,"only active subscriptions can be paused");return}
  status="paused"
 case "resume":
  if status!="paused"{fail(w,409,"only paused subscriptions can be resumed");return}
  next=firstEligible(dateOnly(time.Now()),mask,clock,time.Now())
  if !next.Before(period){
   if !renew{status="ended"}else {
    for !next.Before(period){period=period.AddDate(0,0,7)}
    status="active"
   }
  }else{status="active"}
 case "cancel":
  if status=="cancelled"||status=="ended"{fail(w,409,"subscription already inactive");return}
  status="cancelled"
 }
 _,err=tx.Exec(ctx,`UPDATE meal_subscriptions SET status=$3,next_delivery_date=$4::date,
 next_run_at=$5,current_period_end=$6::date,updated_at=now()
 WHERE id=$1::uuid AND user_id=$2::uuid`,
 id,s.actor(r),status,next.Format("2006-01-02"),deliveryAt(next,clock).Add(-cutoff),period.Format("2006-01-02"))
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if err=tx.Commit(ctx);err!=nil{fail(w,503,"subscription database unavailable");return}
 response(w,200,map[string]string{"status":status})
}
func (s Service) pause(w http.ResponseWriter,r *http.Request){s.action(w,r,"pause")}
func (s Service) resume(w http.ResponseWriter,r *http.Request){s.action(w,r,"resume")}
func (s Service) cancel(w http.ResponseWriter,r *http.Request){s.action(w,r,"cancel")}
func (s Service) update(w http.ResponseWriter,r *http.Request){
 if !s.requireOrigin(w,r){return}
 id:=chi.URLParam(r,"subscriptionID")
 if !uuidRE.MatchString(id){fail(w,400,"invalid subscription ID");return}
 var x UpdateInput
 if err:=parse(w,r,&x);err!=nil||!validMax(x.MaxTotalMinor){fail(w,400,"invalid settings");return}
 tag,err:=s.DB.Exec(r.Context(),`UPDATE meal_subscriptions SET auto_renew=$3,max_total_minor=$4,
 updated_at=now() WHERE id=$1::uuid AND user_id=$2::uuid AND status IN ('active','paused')`,
 id,s.actor(r),x.AutoRenew,x.MaxTotalMinor)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if tag.RowsAffected()==0{fail(w,404,"active subscription not found");return}
 response(w,200,map[string]any{"auto_renew":x.AutoRenew,"max_total_minor":x.MaxTotalMinor})
}
func (s Service) skip(w http.ResponseWriter,r *http.Request){
 if !s.requireOrigin(w,r){return}
 id:=chi.URLParam(r,"subscriptionID")
 if !uuidRE.MatchString(id){fail(w,400,"invalid subscription ID");return}
 var x SkipInput
 if err:=parse(w,r,&x);err!=nil{fail(w,400,"invalid request");return}
 day,err:=parseDate(x.Date)
 if err!=nil||day.Before(dateOnly(time.Now()))||day.After(dateOnly(time.Now()).AddDate(0,0,60)){
  fail(w,400,"skip date must be within 60 days");return
 }
 ctx:=r.Context();tx,err:=s.DB.Begin(ctx)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 defer func(){_=tx.Rollback(ctx)}()
 var mask int
 var clock,status string
 err=tx.QueryRow(ctx,`SELECT weekday_mask,delivery_time,status
 FROM meal_subscriptions WHERE id=$1::uuid AND user_id=$2::uuid FOR UPDATE`,id,s.actor(r)).
 Scan(&mask,&clock,&status)
 if errors.Is(err,pgx.ErrNoRows){fail(w,404,"subscription not found");return}
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if status!="active"&&status!="paused"{fail(w,409,"subscription not active");return}
 if mask&(1<<int(day.Weekday()))==0||!deliveryAt(day,clock).Add(-cutoff).After(time.Now()){
  fail(w,409,"delivery date is not skippable after cutoff");return
 }
 tag,err:=tx.Exec(ctx,`INSERT INTO meal_subscription_skips(subscription_id,delivery_date)
 SELECT $1::uuid,$2::date WHERE NOT EXISTS(
 SELECT 1 FROM meal_subscription_occurrences
 WHERE subscription_id=$1::uuid AND delivery_date=$2::date)
 ON CONFLICT DO NOTHING`,id,x.Date)
 if err!=nil{fail(w,503,"subscription database unavailable");return}
 if tag.RowsAffected()==0{fail(w,409,"delivery already processed or skipped");return}
 if err=tx.Commit(ctx);err!=nil{fail(w,503,"subscription database unavailable");return}
 response(w,201,map[string]string{"date":x.Date,"status":"skip_requested"})
}
