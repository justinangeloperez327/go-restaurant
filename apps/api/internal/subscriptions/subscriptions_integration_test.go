package subscriptions_test

import (
 "bytes"
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "sync"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/subscriptions"
)
type fixture struct {
 t *testing.T
 pool *pgxpool.Pool
 handler http.Handler
 service *subscriptions.Service
 alice,bob,aliceToken,bobToken string
 restaurant,meal,option,aliceAddress,bobAddress string
 start time.Time
}
var zone=time.FixedZone("Asia/Dubai",4*3600)
func random(t *testing.T)string{
 t.Helper()
 b:=make([]byte,20)
 if _,err:=rand.Read(b);err!=nil{t.Fatal(err)}
 return hex.EncodeToString(b)
}
func newFixture(t *testing.T)*fixture{
 t.Helper()
 dsn:=os.Getenv("DATABASE_URL")
 if dsn==""{t.Skip("DATABASE_URL required")}
 ctx:=context.Background()
 pool,err:=pgxpool.New(ctx,dsn)
 if err!=nil{t.Fatal(err)}
 t.Cleanup(pool.Close)
 if err=db.Migrate(ctx,pool);err!=nil{t.Fatal(err)}
 a,err:=auth.NewService(config.Config{WebOrigin:"http://localhost:3000"},pool)
 if err!=nil{t.Fatal(err)}
 f:=&fixture{t:t,pool:pool}
 f.service=&subscriptions.Service{DB:pool,Auth:a,WebOrigin:"http://localhost:3000",AllowCOD:true,TaxBPS:0,DeliveryFeeMinor:0}
 f.handler=httpapi.NewSubscriptionsAppRouter(pool.Ping,a,"http://localhost:3000",
 &customer.Service{DB:pool,Auth:a,WebOrigin:"http://localhost:3000"},nil,f.service)
 store:=auth.Store{DB:pool}
 for _,it:=range []struct{user,token *string}{{&f.alice,&f.aliceToken},{&f.bob,&f.bobToken}}{
  id,err:=store.ResolveIdentity(ctx,"google",random(t),"","Customer")
  if err!=nil{t.Fatal(err)}
  *it.user=id
  *it.token=random(t)
  if err=store.NewSession(ctx,id,*it.token);err!=nil{t.Fatal(err)}
 }
 err=pool.QueryRow(ctx,`INSERT INTO restaurants(name,slug,currency,is_active)
 VALUES('Subscription Kitchen',$1,'AED',true) RETURNING id::text`,
 "subscription-"+random(t)[:12]).Scan(&f.restaurant)
 if err!=nil{t.Fatal(err)}
 var cat string
 err=pool.QueryRow(ctx,`INSERT INTO menu_categories(restaurant_id,name,is_active)
 VALUES($1::uuid,'Meals',true) RETURNING id::text`,f.restaurant).Scan(&cat)
 if err!=nil{t.Fatal(err)}
 err=pool.QueryRow(ctx,`INSERT INTO meals(restaurant_id,category_id,name,price_minor,is_active,is_available)
 VALUES($1::uuid,$2::uuid,'Lunch',2450,true,true) RETURNING id::text`,f.restaurant,cat).Scan(&f.meal)
 if err!=nil{t.Fatal(err)}
 var group string
 err=pool.QueryRow(ctx,`INSERT INTO modifier_groups(restaurant_id,meal_id,name,min_select,max_select)
 VALUES($1::uuid,$2::uuid,'Rice',1,1) RETURNING id::text`,f.restaurant,f.meal).Scan(&group)
 if err!=nil{t.Fatal(err)}
 err=pool.QueryRow(ctx,`INSERT INTO modifier_options(restaurant_id,group_id,name,price_delta_minor,is_available)
 VALUES($1::uuid,$2::uuid,'Brown rice',250,true) RETURNING id::text`,f.restaurant,group).Scan(&f.option)
 if err!=nil{t.Fatal(err)}
 for _,it:=range []struct{user,addr *string}{{&f.alice,&f.aliceAddress},{&f.bob,&f.bobAddress}}{
  err=pool.QueryRow(ctx,`INSERT INTO delivery_addresses
  (user_id,label,recipient_name,phone,country_code,city,area,street,building,apartment,instructions,is_default)
  VALUES($1::uuid,'Home','Customer','+971501234567','AE','Abu Dhabi','','Main Street','','','',true) RETURNING id::text`,
  *it.user).Scan(it.addr)
  if err!=nil{t.Fatal(err)}
 }
 // Cart creation is independent of the subscription template and never consumed.
 f.expect("POST","/api/v1/cart/items",f.aliceToken,map[string]any{
  "meal_id":f.meal,"quantity":2,"option_ids":[]string{f.option},
 },201)
 local:=time.Now().In(zone)
 f.start=time.Date(local.Year(),local.Month(),local.Day(),0,0,0,0,zone).AddDate(0,0,3)
 t.Cleanup(func(){
  ctx:=context.Background()
  _,_=pool.Exec(ctx,`DELETE FROM meal_subscription_occurrences WHERE subscription_id IN
  (SELECT id FROM meal_subscriptions WHERE user_id IN ($1::uuid,$2::uuid))`,f.alice,f.bob)
  _,_=pool.Exec(ctx,`DELETE FROM payment_webhook_events WHERE order_id IN
  (SELECT id FROM orders WHERE user_id IN ($1::uuid,$2::uuid))`,f.alice,f.bob)
  _,_=pool.Exec(ctx,`DELETE FROM order_items WHERE order_id IN
  (SELECT id FROM orders WHERE user_id IN ($1::uuid,$2::uuid))`,f.alice,f.bob)
  _,_=pool.Exec(ctx,`DELETE FROM orders WHERE user_id IN ($1::uuid,$2::uuid)`,f.alice,f.bob)
  _,_=pool.Exec(ctx,`DELETE FROM meal_subscriptions WHERE user_id IN ($1::uuid,$2::uuid)`,f.alice,f.bob)
  _,_=pool.Exec(ctx,"DELETE FROM users WHERE id IN ($1::uuid,$2::uuid)",f.alice,f.bob)
  _,_=pool.Exec(ctx,"DELETE FROM restaurants WHERE id=$1::uuid",f.restaurant)
 })
 return f
}
func (f *fixture) call(method,path,token string,body any)(int,map[string]any) {
 f.t.Helper()
 var raw []byte
 if body!=nil{var err error;raw,err=json.Marshal(body);if err!=nil{f.t.Fatal(err)}}
 req:=httptest.NewRequest(method,path,bytes.NewReader(raw))
 if method!="GET"{req.Header.Set("Origin","http://localhost:3000")}
 if body!=nil{req.Header.Set("Content-Type","application/json")}
 if token!=""{req.AddCookie(&http.Cookie{Name:"gr_session",Value:token})}
 rec:=httptest.NewRecorder()
 f.handler.ServeHTTP(rec,req)
 var result map[string]any
 _=json.Unmarshal(rec.Body.Bytes(),&result)
 return rec.Code,result
}
func (f *fixture) expect(method,path,token string,body any,status int)map[string]any{
 f.t.Helper()
 code,result:=f.call(method,path,token,body)
 if code!=status{f.t.Fatalf("%s %s got %d want %d; body=%v",method,path,code,status,result)}
 return result
}
func (f *fixture) payload(renew bool,cap int64)map[string]any{
 return map[string]any{
  "address_id":f.aliceAddress,"start_date":f.start.Format("2006-01-02"),
  "delivery_time":"12:00","weekdays":[]int{int(f.start.Weekday())},
  "auto_renew":renew,"max_total_minor":cap,"notes":"Leave at reception",
 }
}
func (f *fixture) create(renew bool,cap int64)string{
 f.t.Helper()
 result:=f.expect("POST","/api/v1/subscriptions",f.aliceToken,f.payload(renew,cap),201)
 return result["id"].(string)
}
func (f *fixture) run(at time.Time)(int,error){return f.service.RunDue(context.Background(),at,50)}
func (f *fixture) dueAt(day time.Time)time.Time{
 return day.Add(12*time.Hour).Add(-23*time.Hour)
}
func (f *fixture) countOrders()int{
 f.t.Helper()
 var n int
 if err:=f.pool.QueryRow(context.Background(),`SELECT count(*) FROM orders
 WHERE subscription_id IN (SELECT id FROM meal_subscriptions WHERE user_id=$1::uuid)`,f.alice).Scan(&n);err!=nil{f.t.Fatal(err)}
 return n
}
func TestRecurringRenewalOrderSnapshotAndOwnership(t *testing.T){
 f:=newFixture(t)
 id:=f.create(true,5500) // two meals at 2700 fils = AED 54.00
 url:="/api/v1/subscriptions/"+id
 f.expect("GET",url,f.bobToken,nil,404)
 f.expect("POST",url+"/pause",f.bobToken,nil,404)
 if code,_:=f.call("GET","/api/v1/subscriptions",f.bobToken,nil);code!=200{t.Fatalf("not isolated listing %d",code)}
 count,err:=f.run(f.dueAt(f.start))
 if err!=nil||count!=1{t.Fatalf("worker first due count=%d err=%v",count,err)}
 if f.countOrders()!=1{t.Fatal("expected one generated order")}
 count,err=f.run(f.dueAt(f.start))
 if err!=nil||count!=0{t.Fatalf("duplicate scheduling: count=%d err=%v",count,err)}
 history:=f.expect("GET",url+"/occurrences",f.aliceToken,nil,200)
 occurrences:=history["occurrences"].([]any)
 if len(occurrences)!=1||occurrences[0].(map[string]any)["status"]!="created"{t.Fatalf("missing created occurrence: %v",history)}
 orderID:=occurrences[0].(map[string]any)["order_id"].(string)
 var savedTotal int64
 var payment, orderStatus string
 var scheduled time.Time
 err=f.pool.QueryRow(context.Background(),`SELECT total_minor,payment_status,order_status,delivery_scheduled_at
 FROM orders WHERE id=$1::uuid AND user_id=$2::uuid AND subscription_id=$3::uuid`,orderID,f.alice,id).
 Scan(&savedTotal,&payment,&orderStatus,&scheduled)
 if err!=nil||savedTotal!=5400||payment!="unpaid"||orderStatus!="placed"{
  t.Fatalf("invalid subscription order snapshot total=%d payment=%s status=%s err=%v",savedTotal,payment,orderStatus,err)
 }
 if scheduled.In(zone).Format("2006-01-02")!=f.start.Format("2006-01-02"){t.Fatal("wrong scheduled delivery date")}
 if _,err=f.pool.Exec(context.Background(),"UPDATE meals SET price_minor=4000 WHERE id=$1::uuid",f.meal);err!=nil{t.Fatal(err)}
 if err=f.pool.QueryRow(context.Background(),"SELECT total_minor FROM orders WHERE id=$1::uuid",orderID).Scan(&savedTotal);err!=nil||savedTotal!=5400{t.Fatal("order snapshot changed")}
}
func TestPauseResumeSkipAndNonRenewal(t *testing.T){
 f:=newFixture(t)
 id:=f.create(false,5500)
 root:="/api/v1/subscriptions/"+id
 f.expect("POST",root+"/pause",f.aliceToken,nil,200)
 n,err:=f.run(f.dueAt(f.start))
 if err!=nil||n!=0{t.Fatalf("paused subscription was processed: %d %v",n,err)}
 f.expect("POST",root+"/resume",f.aliceToken,nil,200)
 f.expect("POST",root+"/skips",f.aliceToken,map[string]string{"date":f.start.Format("2006-01-02")},201)
 f.expect("POST",root+"/skips",f.aliceToken,map[string]string{"date":f.start.Format("2006-01-02")},409)
 n,err=f.run(f.dueAt(f.start))
 if err!=nil||n!=1{t.Fatalf("skipped generation failed: %d %v",n,err)}
 if f.countOrders()!=0{t.Fatal("skipped day generated an order")}
 sub:=f.expect("GET",root,f.aliceToken,nil,200)
 if sub["status"]!="ended"{t.Fatalf("nonrenewing subscription continued: %v",sub)}
 f.expect("POST",root+"/resume",f.aliceToken,nil,409)
}
func TestPriceCapAvailabilityAndMissedWindow(t *testing.T){
 f:=newFixture(t)
 id:=f.create(true,5500)
 _,err:=f.pool.Exec(context.Background(),"UPDATE meals SET price_minor=4000 WHERE id=$1::uuid",f.meal)
 if err!=nil{t.Fatal(err)}
 n,err:=f.run(f.dueAt(f.start))
 if err!=nil||n!=1{t.Fatalf("price cap run: %d %v",n,err)}
 if f.countOrders()!=0{t.Fatal("price cap was bypassed")}
 history:=f.expect("GET","/api/v1/subscriptions/"+id+"/occurrences",f.aliceToken,nil,200)
 occ:=history["occurrences"].([]any)[0].(map[string]any)
 if occ["reason"]!="price_cap_exceeded"{t.Fatalf("missing price rejection: %v",occ)}
 // Next week the meal may be available at original price again.
 _,err=f.pool.Exec(context.Background(),"UPDATE meals SET price_minor=2450 WHERE id=$1::uuid",f.meal)
 if err!=nil{t.Fatal(err)}
 n,err=f.run(f.dueAt(f.start.AddDate(0,0,7)))
 if err!=nil||n!=1{t.Fatalf("renewed order run: %d %v",n,err)}
 if f.countOrders()!=1{t.Fatal("renewal did not create one COD order")}
 // Advance far enough that a missed delivery would be backdated; it must skip.
 n,err=f.run(f.start.AddDate(0,0,14).Add(13*time.Hour))
 if err!=nil||n<1{t.Fatalf("missed schedule did not advance: %d %v",n,err)}
 if f.countOrders()!=1{t.Fatal("backdated order created")}
}
func TestConcurrentWorkerSingleDeliveryAndCancellation(t *testing.T){
 f:=newFixture(t)
 id:=f.create(true,5500)
 due:=f.dueAt(f.start)
 var wg sync.WaitGroup
 errCh:=make(chan error,4)
 for i:=0;i<4;i++{
  wg.Add(1)
  go func(){defer wg.Done();_,err:=f.run(due);errCh<-err}()
 }
 wg.Wait();close(errCh)
 for err:=range errCh{if err!=nil{t.Fatalf("concurrent worker: %v",err)}}
 if f.countOrders()!=1{t.Fatalf("duplicate orders from concurrent worker: %d",f.countOrders())}
 f.expect("POST","/api/v1/subscriptions/"+id+"/cancel",f.aliceToken,nil,200)
 n,err:=f.run(f.dueAt(f.start.AddDate(0,0,7)))
 if err!=nil||n!=0{t.Fatalf("cancelled subscription processed: %d %v",n,err)}
}
func TestStartAndTenantValidation(t *testing.T){
 f:=newFixture(t)
 bad:=f.payload(true,100) // below existing live cart total
 f.expect("POST","/api/v1/subscriptions",f.aliceToken,bad,409)
 bad=f.payload(true,5500);bad["address_id"]=f.bobAddress
 f.expect("POST","/api/v1/subscriptions",f.aliceToken,bad,404)
 bad=f.payload(true,5500);bad["weekdays"]=[]int{1,1}
 f.expect("POST","/api/v1/subscriptions",f.aliceToken,bad,400)
 bad=f.payload(true,5500);bad["start_date"]="2025-01-01"
 f.expect("POST","/api/v1/subscriptions",f.aliceToken,bad,400)
 id:=f.create(true,5500)
 f.expect("PUT","/api/v1/subscriptions/"+id,f.bobToken,map[string]any{"auto_renew":false,"max_total_minor":10000},404)
 f.expect("POST","/api/v1/subscriptions/"+id+"/skips",f.bobToken,map[string]any{"date":f.start.Format("2006-01-02")},404)
 f.expect("PUT","/api/v1/subscriptions/"+id,f.aliceToken,map[string]any{"auto_renew":false,"max_total_minor":5000},200)
 if code,_:=f.call("GET","/api/v1/subscriptions/"+id,f.aliceToken,nil);code!=200{t.Fatalf("updated subscription unreadable: %d",code)}
}
