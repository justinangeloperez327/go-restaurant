package customer_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
)

type fixture struct {
	t *testing.T
	db *pgxpool.Pool
	handler http.Handler
	alice, bob string
	at, bt string
	restaurantA, restaurantB, category, meal, groupID, optionID string
}
func random(t *testing.T) string {
	t.Helper()
	b:=make([]byte,32)
	if _,err:=rand.Read(b);err!=nil { t.Fatal(err) }
	return hex.EncodeToString(b)
}
func makeFixture(t *testing.T) *fixture {
	t.Helper()
	dsn:=os.Getenv("DATABASE_URL")
	if dsn=="" {t.Skip("PostgreSQL integration test requires DATABASE_URL")}
	ctx:=context.Background()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil {t.Fatal(err)}
	t.Cleanup(func(){pool.Close()})
	if err=db.Migrate(ctx,pool);err!=nil {t.Fatal(err)}
	as,err:=auth.NewService(config.Config{WebOrigin:"http://localhost:3000"},pool)
	if err!=nil {t.Fatal(err)}
	f:=&fixture{t:t,db:pool,handler:httpapi.NewCustomerAppRouter(pool.Ping,as,"http://localhost:3000",&customer.Service{DB:pool,Auth:as,WebOrigin:"http://localhost:3000"})}
	store:=auth.Store{DB:pool}
	for _,u:=range []struct{id,token *string}{{&f.alice,&f.at},{&f.bob,&f.bt}} {
		id,err:=store.ResolveIdentity(ctx,"google",random(t),"","customer")
		if err!=nil {t.Fatal(err)}
		*u.id=id
		t.Cleanup(func(){_,_=pool.Exec(context.Background(),"DELETE FROM users WHERE id=$1::uuid",id)})
		*u.token=random(t)
		if err=store.NewSession(ctx,id,*u.token);err!=nil {t.Fatal(err)}
	}
	for _,slot:=range []*string{&f.restaurantA,&f.restaurantB} {
		err=pool.QueryRow(ctx,`INSERT INTO restaurants(name,slug,currency,is_active)
			VALUES('Kitchen',$1,'AED',true) RETURNING id::text`, "test-"+random(t)[:12]).Scan(slot)
		if err!=nil {t.Fatal(err)}
		id:=*slot
		t.Cleanup(func(){_,_=pool.Exec(context.Background(),"DELETE FROM restaurants WHERE id=$1::uuid",id)})
	}
	if err=pool.QueryRow(ctx,`INSERT INTO menu_categories(restaurant_id,name,is_active)
		VALUES($1::uuid,'Lunch',true) RETURNING id::text`,f.restaurantA).Scan(&f.category);err!=nil {t.Fatal(err)}
	if err=pool.QueryRow(ctx,`INSERT INTO meals(restaurant_id,category_id,name,price_minor,is_active,is_available)
		VALUES($1::uuid,$2::uuid,'Chicken bowl',2450,true,true) RETURNING id::text`,
		f.restaurantA,f.category).Scan(&f.meal);err!=nil {t.Fatal(err)}
	if err=pool.QueryRow(ctx,`INSERT INTO modifier_groups(restaurant_id,meal_id,name,min_select,max_select)
		VALUES($1::uuid,$2::uuid,'Rice',1,1) RETURNING id::text`,
		f.restaurantA,f.meal).Scan(&f.groupID);err!=nil {t.Fatal(err)}
	if err=pool.QueryRow(ctx,`INSERT INTO modifier_options(restaurant_id,group_id,name,price_delta_minor)
		VALUES($1::uuid,$2::uuid,'Brown rice',250) RETURNING id::text`,
		f.restaurantA,f.groupID).Scan(&f.optionID);err!=nil {t.Fatal(err)}
	return f
}
func (f *fixture) call(method,path,token,origin string,body any) (int,map[string]any) {
	f.t.Helper()
	var b []byte
	if body!=nil {var err error;b,err=json.Marshal(body);if err!=nil {f.t.Fatal(err)}}
	req:=httptest.NewRequest(method,path,bytes.NewReader(b))
	if body!=nil {req.Header.Set("Content-Type","application/json")}
	if origin!="" {req.Header.Set("Origin",origin)}
	if token!="" {req.AddCookie(&http.Cookie{Name:"gr_session",Value:token})}
	rec:=httptest.NewRecorder()
	f.handler.ServeHTTP(rec,req)
	var out map[string]any
	_=json.Unmarshal(rec.Body.Bytes(),&out)
	return rec.Code,out
}
func (f *fixture) expect(method,path,token string,body any, want int) map[string]any {
	f.t.Helper()
	status,res:=f.call(method,path,token,"http://localhost:3000",body)
	if status!=want {f.t.Fatalf("%s %s got %d want %d: %v",method,path,status,want,res)}
	return res
}
func addr(label string,def bool) map[string]any {
	return map[string]any{
		"label":label,"recipient_name":"Alice","phone":"+971 50 1234567","country_code":"AE",
		"city":"Abu Dhabi","area":"Al Reem","street":"Main street","building":"Tower",
		"apartment":"201","instructions":"Call first","is_default":def,
	}
}

func TestProfilesAndAddressIsolation(t *testing.T) {
	f:=makeFixture(t)
	p:=f.expect("GET","/api/v1/me/profile",f.at,nil,200)
	if p["full_name"]!="" {t.Fatalf("profile should be empty: %v",p)}
	f.expect("PUT","/api/v1/me/profile",f.at,map[string]any{"full_name":"Alice Tester","phone":"+971501234567"},200)
	f.expect("PUT","/api/v1/me/profile",f.at,map[string]any{"full_name":"Alice","phone":"invalid"},400)
	f.expect("PUT","/api/v1/me/profile",f.bt,map[string]any{"full_name":"Bob","phone":""},200)
	p=f.expect("GET","/api/v1/me/profile",f.at,nil,200)
	if p["full_name"]!="Alice Tester" {t.Fatalf("profile changed by other user: %v",p)}
	f.expect("GET","/api/v1/me/profile","",nil,401)

	first:=f.expect("POST","/api/v1/me/addresses",f.at,addr("Home",false),201)["id"].(string)
	second:=f.expect("POST","/api/v1/me/addresses",f.at,addr("Office",true),201)["id"].(string)
	status,res:=f.call("GET","/api/v1/me/addresses",f.at,"",nil)
	if status!=200 {t.Fatalf("addresses got %d: %v",status,res)}
	list:=readArray(f,"/api/v1/me/addresses",f.at)
	if len(list)!=2 || list[0]["id"]!=second || list[0]["is_default"]!=true {
		t.Fatalf("default address must be first: %v",list)
	}
	f.expect("PUT","/api/v1/me/addresses/"+first,f.bt,addr("Stolen",true),404)
	f.expect("DELETE","/api/v1/me/addresses/"+first,f.bt,nil,404)
	f.expect("PUT","/api/v1/me/addresses/"+first,f.at,addr("New home",true),200)
	list=readArray(f,"/api/v1/me/addresses",f.at)
	if list[0]["id"]!=first {t.Fatalf("default switch failed: %v",list)}
	f.expect("DELETE","/api/v1/me/addresses/"+first,f.at,nil,204)
	list=readArray(f,"/api/v1/me/addresses",f.at)
	if len(list)!=1 || list[0]["id"]!=second || list[0]["is_default"]!=true {t.Fatalf("default fallback failed: %v",list)}
	f.expect("POST","/api/v1/me/addresses",f.at,addr("Bad",false),201)
	bad:=addr("Invalid",false);bad["phone"]="bad"
	f.expect("POST","/api/v1/me/addresses",f.at,bad,400)
	if code,_:=f.call("POST","/api/v1/me/addresses",f.at,"https://attacker.example",addr("CSRF",true));code!=403 {
		t.Fatalf("origin CSRF protection failed: %d",code)
	}
}
func readArray(f *fixture,path,token string) []map[string]any {
	f.t.Helper()
	req:=httptest.NewRequest("GET",path,nil)
	req.AddCookie(&http.Cookie{Name:"gr_session",Value:token})
	rec:=httptest.NewRecorder()
	f.handler.ServeHTTP(rec,req)
	if rec.Code!=200 {f.t.Fatalf("read array status %d: %s",rec.Code,rec.Body.String())}
	var out []map[string]any
	if err:=json.Unmarshal(rec.Body.Bytes(),&out);err!=nil {f.t.Fatal(err)}
	return out
}

func TestCartPricingAndIsolation(t *testing.T) {
	f:=makeFixture(t)
	add:=map[string]any{"meal_id":f.meal,"quantity":2,"option_ids":[]string{f.optionID}}
	f.expect("POST","/api/v1/cart/items",f.at,map[string]any{"meal_id":f.meal,"quantity":1,"option_ids":[]string{}},409)
	f.expect("POST","/api/v1/cart/items",f.at,map[string]any{"meal_id":f.meal,"quantity":1,"option_ids":[]string{f.optionID,f.optionID}},400)
	id:=f.expect("POST","/api/v1/cart/items",f.at,add,201)["id"].(string)
	cart:=f.expect("GET","/api/v1/cart",f.at,nil,200)
	if cart["subtotal_minor"]!=float64(5400) || cart["can_checkout"]!=true {t.Fatalf("server price error: %v",cart)}
	items:=cart["items"].([]any)
	if len(items)!=1 || items[0].(map[string]any)["unit_price_minor"]!=float64(2700) {t.Fatalf("cart items: %v",items)}
	other:=f.expect("GET","/api/v1/cart",f.bt,nil,200)
	if len(other["items"].([]any))!=0 {t.Fatalf("cart leaked across customers: %v",other)}
	f.expect("PUT","/api/v1/cart/items/"+id,f.bt,map[string]any{"quantity":4},404)
	f.expect("DELETE","/api/v1/cart/items/"+id,f.bt,nil,404)
	f.expect("PUT","/api/v1/cart/items/"+id,f.at,map[string]any{"quantity":3},200)
	f.expect("PUT","/api/v1/cart/items/"+id,f.at,map[string]any{"quantity":0},400)
	_,err:=f.db.Exec(context.Background(),"UPDATE meals SET price_minor=3000 WHERE id=$1::uuid",f.meal)
	if err!=nil {t.Fatal(err)}
	cart=f.expect("GET","/api/v1/cart",f.at,nil,200)
	if cart["subtotal_minor"]!=float64(9750) {t.Fatalf("cart did not reprice from database: %v",cart)}
	_,err=f.db.Exec(context.Background(),"UPDATE modifier_options SET is_available=false WHERE id=$1::uuid",f.optionID)
	if err!=nil {t.Fatal(err)}
	cart=f.expect("GET","/api/v1/cart",f.at,nil,200)
	if cart["can_checkout"]!=false {t.Fatal("unavailable modifier incorrectly checkout eligible")}
	f.expect("POST","/api/v1/cart/items",f.at,add,409)
	f.expect("DELETE","/api/v1/cart/items/"+id,f.at,nil,204)
	cart=f.expect("GET","/api/v1/cart",f.at,nil,200)
	if cart["restaurant"]!=nil || len(cart["items"].([]any))!=0 {t.Fatalf("cart not cleared after final item: %v",cart)}
	_,err=f.db.Exec(context.Background(),"UPDATE modifier_options SET is_available=true WHERE id=$1::uuid",f.optionID)
	if err!=nil {t.Fatal(err)}
	f.expect("POST","/api/v1/cart/items",f.at,add,201)
	f.expect("DELETE","/api/v1/cart",f.at,nil,204)
	cart=f.expect("GET","/api/v1/cart",f.at,nil,200)
	if len(cart["items"].([]any))!=0 {t.Fatal("cart clear failed")}
	if code,_:=f.call("POST","/api/v1/cart/items",f.at,"https://attacker.example",add);code!=403 {
		t.Fatalf("cross-origin cart mutation accepted: %d",code)
	}
}
func TestCartOneRestaurant(t *testing.T) {
	f:=makeFixture(t)
	var otherMeal string
	ctx:=context.Background()
	var category string
	if err:=f.db.QueryRow(ctx,`INSERT INTO menu_categories(restaurant_id,name,is_active)
		VALUES($1::uuid,'Other',true) RETURNING id::text`,f.restaurantB).Scan(&category);err!=nil {t.Fatal(err)}
	if err:=f.db.QueryRow(ctx,`INSERT INTO meals(restaurant_id,category_id,name,price_minor,is_active,is_available)
		VALUES($1::uuid,$2::uuid,'Other',1000,true,true) RETURNING id::text`,
		f.restaurantB,category).Scan(&otherMeal);err!=nil {t.Fatal(err)}
	f.expect("POST","/api/v1/cart/items",f.at,map[string]any{"meal_id":f.meal,"quantity":1,"option_ids":[]string{f.optionID}},201)
	code,body:=f.call("POST","/api/v1/cart/items",f.at,"http://localhost:3000",map[string]any{"meal_id":otherMeal,"quantity":1,"option_ids":[]string{}})
	if code!=409 {t.Fatalf("mixed cart allowed: %d %v",code,body)}
	f.expect("DELETE","/api/v1/cart",f.at,nil,204)
	f.expect("POST","/api/v1/cart/items",f.at,map[string]any{"meal_id":otherMeal,"quantity":1,"option_ids":[]string{}},201)
	if cart:=f.expect("GET","/api/v1/cart",f.at,nil,200);cart["subtotal_minor"]!=float64(1000) {
		t.Fatalf("wrong restaurant cart: %v",cart)
	}
}
func TestCartOwnershipDatabaseConstraint(t *testing.T) {
	f:=makeFixture(t)
	ctx:=context.Background()
	_,err:=f.db.Exec(ctx,"INSERT INTO customer_carts(user_id,restaurant_id) VALUES($1::uuid,$2::uuid)",f.alice,f.restaurantA)
	if err!=nil {t.Fatal(err)}
	_,err=f.db.Exec(ctx,`INSERT INTO cart_items(user_id,restaurant_id,meal_id,quantity)
		VALUES($1::uuid,$2::uuid,$3::uuid,1)`,f.alice,f.restaurantB,f.meal)
	if err==nil {t.Fatal("mismatched restaurant cart FK allowed")}
}
