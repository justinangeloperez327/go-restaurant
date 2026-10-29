package delivery_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/delivery"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
)

type fixture struct {
	t *testing.T
	db *pgxpool.Pool
	router http.Handler
	manager, manager2, admin, rider1, rider2, customer, stranger string
	mt, m2t, at, r1t, r2t, ct, st string
	restaurant, otherRestaurant, order, pendingOrder, otherOrder string
}
func random(t *testing.T) string {
	t.Helper()
	b:=make([]byte,20)
	if _,err:=rand.Read(b);err!=nil{t.Fatal(err)}
	return hex.EncodeToString(b)
}
func makeFixture(t *testing.T)*fixture {
	t.Helper()
	dsn:=os.Getenv("DATABASE_URL")
	if dsn==""{t.Skip("DATABASE_URL required for delivery integration tests")}
	dbp,err:=pgxpool.New(context.Background(),dsn)
	if err!=nil{t.Fatal(err)}
	t.Cleanup(dbp.Close)
	ctx:=context.Background()
	if err:=db.Migrate(ctx,dbp);err!=nil{t.Fatal(err)}
	as,err:=auth.NewService(config.Config{WebOrigin:"http://localhost:3000"},dbp)
	if err!=nil{t.Fatal(err)}
	f:=&fixture{t:t,db:dbp,router:httpapi.NewDeliveryAppRouter(dbp.Ping,as,"http://localhost:3000",nil,nil,nil,
		&delivery.Service{DB:dbp,Auth:as,WebOrigin:"http://localhost:3000"})}
	store:=auth.Store{DB:dbp}
	for _,u:=range []struct{id,token *string;role string}{
		{&f.manager,&f.mt,"restaurant_manager"},{&f.manager2,&f.m2t,"restaurant_manager"},
		{&f.admin,&f.at,"admin"},{&f.rider1,&f.r1t,"rider"},{&f.rider2,&f.r2t,"rider"},
		{&f.customer,&f.ct,"customer"},{&f.stranger,&f.st,"customer"},
	}{
		id,err:=store.ResolveIdentity(ctx,"google",random(t),"","Test "+u.role)
		if err!=nil{t.Fatal(err)}
		*u.id=id
		*u.token=random(t)
		if err=store.NewSession(ctx,id,*u.token);err!=nil{t.Fatal(err)}
		if u.role!="customer"{
			_,err=dbp.Exec(ctx,"INSERT INTO user_roles(user_id,role_name) VALUES($1::uuid,$2)",id,u.role)
			if err!=nil{t.Fatal(err)}
		}
	}
	for _,p:=range []*string{&f.restaurant,&f.otherRestaurant}{
		err=dbp.QueryRow(ctx,`INSERT INTO restaurants(name,slug,is_active,currency)
		VALUES('Dispatch Kitchen',$1,true,'AED') RETURNING id::text`,"dispatch-"+random(t)[:15]).Scan(p)
		if err!=nil{t.Fatal(err)}
	}
	_,err=dbp.Exec(ctx,`INSERT INTO restaurant_staff(restaurant_id,user_id)
		VALUES($1::uuid,$2::uuid),($3::uuid,$4::uuid)`,
		f.restaurant,f.manager,f.otherRestaurant,f.manager2)
	if err!=nil{t.Fatal(err)}
	insertOrder:=func(owner,restaurant,status,method,payment string)string{
		t.Helper()
		var id string
		err=dbp.QueryRow(ctx,`INSERT INTO orders(
		user_id,restaurant_id,delivery_address,idempotency_key,payment_method,
		order_status,payment_status,currency,subtotal_minor,delivery_fee_minor,tax_minor,total_minor)
		VALUES($1::uuid,$2::uuid,'{"recipient_name":"Customer","phone":"+971501234567","city":"Abu Dhabi","street":"Test Street"}'::jsonb,
		$3,$4,$5,$6,'AED',2500,0,0,2500) RETURNING id::text`,
		owner,restaurant,"test-"+random(t),method,status,payment).Scan(&id)
		if err!=nil{t.Fatal(err)}
		return id
	}
	f.order=insertOrder(f.customer,f.restaurant,"placed","cod","unpaid")
	f.pendingOrder=insertOrder(f.customer,f.restaurant,"pending_payment","stripe","pending")
	f.otherOrder=insertOrder(f.stranger,f.otherRestaurant,"placed","cod","unpaid")
	t.Cleanup(func(){
		ctx:=context.Background()
		ids:=[]string{f.order,f.pendingOrder,f.otherOrder}
		for _,id:=range ids{
			_,_=dbp.Exec(ctx,`DELETE FROM delivery_events WHERE delivery_id IN
			 (SELECT id FROM delivery_jobs WHERE order_id=$1::uuid)`,id)
			_,_=dbp.Exec(ctx,"DELETE FROM delivery_jobs WHERE order_id=$1::uuid",id)
			_,_=dbp.Exec(ctx,"DELETE FROM order_items WHERE order_id=$1::uuid",id)
			_,_=dbp.Exec(ctx,"DELETE FROM orders WHERE id=$1::uuid",id)
		}
		for _,id:=range []string{f.manager,f.manager2,f.admin,f.rider1,f.rider2,f.customer,f.stranger}{
			_,_=dbp.Exec(ctx,"DELETE FROM users WHERE id=$1::uuid",id)
		}
		for _,id:=range []string{f.restaurant,f.otherRestaurant}{
			_,_=dbp.Exec(ctx,"DELETE FROM restaurants WHERE id=$1::uuid",id)
		}
	})
	return f
}
func (f *fixture) request(method,path,token string,body any,origin string)(int,map[string]any){
	f.t.Helper()
	var b []byte
	if body!=nil{var err error;b,err=json.Marshal(body);if err!=nil{f.t.Fatal(err)}}
	req:=httptest.NewRequest(method,path,bytes.NewReader(b))
	if token!=""{req.AddCookie(&http.Cookie{Name:"gr_session",Value:token})}
	if body!=nil{req.Header.Set("Content-Type","application/json")}
	if origin!=""{req.Header.Set("Origin",origin)}
	rec:=httptest.NewRecorder();f.router.ServeHTTP(rec,req)
	var out map[string]any
	_=json.Unmarshal(rec.Body.Bytes(),&out)
	return rec.Code,out
}
func(f *fixture) expect(method,path,token string,body any,want int)map[string]any{
	f.t.Helper()
	code,output:=f.request(method,path,token,body,"http://localhost:3000")
	if code!=want{f.t.Fatalf("%s %s: got %d want %d: %v",method,path,code,want,output)}
	return output
}
func (f *fixture) assign(order,rider,token string)map[string]any{
	f.t.Helper()
	return f.expect("PUT","/api/v1/admin/deliveries/orders/"+order+"/assignment",token,
		map[string]string{"rider_id":rider},200)
}

func TestDispatchPermissionsAndPaymentGate(t *testing.T){
	f:=makeFixture(t)
	root:="/api/v1/admin/deliveries"
	p:="/api/v1/admin/deliveries/orders/"+f.order+"/assignment"
	b:=map[string]string{"rider_id":f.rider1}
	f.expect("PUT",p,f.mt,map[string]string{"rider_id":f.customer},409)
	f.expect("PUT",p,f.m2t,b,403)
	f.expect("PUT",p,f.ct,b,403)
	f.expect("PUT",p,f.r1t,b,403)
	f.expect("PUT","/api/v1/admin/deliveries/orders/"+f.pendingOrder+"/assignment",f.mt,b,409)
	f.expect("GET",root+"?restaurant_id="+f.otherRestaurant,f.mt,nil,403)
	f.expect("GET",root+"?restaurant_id="+f.restaurant,f.mt,nil,200)
	f.expect("GET",root+"/eligible-orders?restaurant_id="+f.restaurant,f.mt,nil,200)
	f.expect("GET",root+"/riders?restaurant_id="+f.restaurant,f.mt,nil,200)
	j:=f.assign(f.order,f.rider1,f.mt)
	if j["status"]!="assigned"{t.Fatalf("assignment wrong status: %v",j)}
	id:=j["id"].(string)
	again:=f.assign(f.order,f.rider1,f.mt)
	if again["id"]!=id{t.Fatal("same rider reassignment must be idempotent")}
	var events int
	if err:=f.db.QueryRow(context.Background(),"SELECT count(*) FROM delivery_events WHERE delivery_id=$1::uuid",id).Scan(&events);err!=nil||events!=1{
		t.Fatalf("repeated assignment wrote events: %d, %v",events,err)
	}
	f.expect("GET","/api/v1/orders/"+f.order+"/delivery",f.ct,nil,200)
	f.expect("GET","/api/v1/orders/"+f.order+"/delivery",f.st,nil,404)
	f.expect("GET","/api/v1/deliveries/"+id,f.m2t,nil,404)
	f.expect("GET","/api/v1/deliveries/"+id,f.mt,nil,200)
	// A verified Stripe-paid order is eligible even though an unverified one is not.
	_,err:=f.db.Exec(context.Background(),`UPDATE orders SET order_status='placed',payment_status='paid'
	WHERE id=$1::uuid`,f.pendingOrder)
	if err!=nil{t.Fatal(err)}
	f.assign(f.pendingOrder,f.rider2,f.at)
	// A non-eligible origin must fail regardless of valid actor credentials.
	if status,_:=f.request("PUT",p,f.mt,b,"https://attacker.example");status!=403{t.Fatalf("origin bypass: %d",status)}
}
func TestRiderLocationTransitionAndRevocation(t *testing.T){
	f:=makeFixture(t)
	j:=f.assign(f.order,f.rider1,f.mt);id:=j["id"].(string)
	path:="/api/v1/rider/deliveries/"+id
	loc:=map[string]any{"latitude":24.4539,"longitude":54.3773,"accuracy_m":12.5}
	f.expect("PUT",path+"/location",f.r2t,loc,404)
	f.expect("PUT",path+"/location",f.ct,loc,404)
	f.expect("PUT",path+"/status",f.r1t,map[string]string{"status":"delivered","reason":""},409)
	f.expect("PUT",path+"/location",f.r1t,map[string]any{"latitude":100.0,"longitude":54.0,"accuracy_m":10},400)
	f.expect("PUT",path+"/location",f.r1t,loc,204)
	f.expect("PUT",path+"/location",f.r1t,loc,429)
	owner:=f.expect("GET","/api/v1/orders/"+f.order+"/delivery",f.ct,nil,200)
	if owner["latitude"]!=float64(24.4539){t.Fatalf("customer missing active GPS: %v",owner)}
	f.expect("PUT",path+"/status",f.r1t,map[string]string{"status":"picked_up","reason":""},200)
	f.expect("PUT","/api/v1/admin/deliveries/orders/"+f.order+"/assignment",f.mt,
		map[string]string{"rider_id":f.rider2},409)
	f.expect("PUT",path+"/status",f.r1t,map[string]string{"status":"en_route","reason":""},200)
	f.expect("PUT",path+"/status",f.r1t,map[string]string{"status":"delivered","reason":""},200)
	f.expect("PUT",path+"/status",f.r1t,map[string]string{"status":"delivered","reason":""},409)
	f.expect("PUT",path+"/location",f.r1t,loc,409)
	owner=f.expect("GET","/api/v1/orders/"+f.order+"/delivery",f.ct,nil,200)
	if owner["latitude"]!=nil||owner["longitude"]!=nil||owner["status"]!="delivered" {
		t.Fatalf("terminal delivery leaked coordinates: %v",owner)
	}
	var gpsCount int
	err:=f.db.QueryRow(context.Background(),"SELECT count(*) FROM delivery_events WHERE delivery_id=$1::uuid AND event_type='location'",id).Scan(&gpsCount)
	if err!=nil||gpsCount!=1{t.Fatalf("unexpected GPS history events: %d %v",gpsCount,err)}
}
func TestReassignmentAndFailedDelivery(t *testing.T){
	f:=makeFixture(t)
	j:=f.assign(f.order,f.rider1,f.mt);id:=j["id"].(string)
	loc:=map[string]any{"latitude":24.4,"longitude":54.35,"accuracy_m":22}
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/location",f.r1t,loc,204)
	j=f.assign(f.order,f.rider2,f.mt)
	if j["latitude"]!=nil||j["rider_id"]!=f.rider2{t.Fatalf("reassigned rider inherited GPS: %v",j)}
	f.expect("GET","/api/v1/deliveries/"+id,f.r1t,nil,404)
	f.expect("GET","/api/v1/deliveries/"+id,f.r2t,nil,200)
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/status",f.r1t,
		map[string]string{"status":"picked_up","reason":""},404)
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/status",f.r2t,
		map[string]string{"status":"failed","reason":""},400)
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/status",f.r2t,
		map[string]string{"status":"failed","reason":"Customer not available"},200)
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/status",f.r2t,
		map[string]string{"status":"picked_up","reason":""},409)
	owner:=f.expect("GET","/api/v1/orders/"+f.order+"/delivery",f.ct,nil,200)
	if owner["status"]!="failed"||owner["failure_reason"]!="Customer not available"{
		t.Fatalf("failed status not persisted: %v",owner)
	}
}
func TestSSERequiresOwnershipAndSignals(t *testing.T){
	f:=makeFixture(t)
	j:=f.assign(f.order,f.rider1,f.mt)
	id:=j["id"].(string)
	p:="/api/v1/deliveries/"+id+"/events"
	f.expect("GET",p,f.st,nil,404)
	f.expect("GET",p,f.r2t,nil,404)
	srv:=httptest.NewServer(f.router);defer srv.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),7*time.Second);defer cancel()
	req,err:=http.NewRequestWithContext(ctx,"GET",srv.URL+p,nil)
	if err!=nil{t.Fatal(err)}
	req.AddCookie(&http.Cookie{Name:"gr_session",Value:f.ct})
	res,err:=srv.Client().Do(req);if err!=nil{t.Fatal(err)}
	defer res.Body.Close()
	if res.StatusCode!=200||!strings.Contains(res.Header.Get("Content-Type"),"text/event-stream"){
		t.Fatalf("SSE unavailable: %d",res.StatusCode)
	}
	reader:=bufio.NewReader(res.Body)
	line,err:=reader.ReadString('\n');if err!=nil||!strings.Contains(line,"retry:"){t.Fatalf("SSE handshake: %q %v",line,err)}
	f.expect("PUT","/api/v1/rider/deliveries/"+id+"/status",f.r1t,
		map[string]string{"status":"picked_up","reason":""},200)
	found:=false
	for !found {
		line,err=reader.ReadString('\n')
		if err!=nil{break}
		if strings.HasPrefix(line,"event: delivery-update"){found=true}
		if strings.Contains(line,"latitude")||strings.Contains(line,"longitude"){
			t.Fatalf("private coordinates leaked into SSE: %q",line)
		}
	}
	if !found{t.Fatalf("no SSE update received: %v",err)}
}
func TestInactiveRiderCannotBeAssigned(t *testing.T){
	f:=makeFixture(t)
	_,err:=f.db.Exec(context.Background(),"UPDATE users SET is_active=false WHERE id=$1::uuid",f.rider1)
	if err!=nil{t.Fatal(err)}
	f.expect("PUT","/api/v1/admin/deliveries/orders/"+f.order+"/assignment",f.mt,
		map[string]string{"rider_id":f.rider1},409)
}
