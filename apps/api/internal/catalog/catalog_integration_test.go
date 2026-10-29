package catalog_test

import (
	"bytes"
	cryptoRand "crypto/rand"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/catalog"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
)

type fixture struct {
	t *testing.T
	pool *pgxpool.Pool
	router http.Handler
	admin, manager, outsider, customer string
	adminToken, managerToken, outsiderToken, customerToken string
}
func (f *fixture) request(method,path,token string, body any, origin string) (int, map[string]any) {
	f.t.Helper()
	var payload []byte
	if body!=nil {
		var err error
		payload,err=json.Marshal(body)
		if err!=nil { f.t.Fatal(err) }
	}
	req:=httptest.NewRequest(method,path,bytes.NewReader(payload))
	if body!=nil {req.Header.Set("Content-Type","application/json")}
	if origin!="" {req.Header.Set("Origin",origin)}
	if token!="" {req.AddCookie(&http.Cookie{Name:"gr_session",Value:token})}
	rec:=httptest.NewRecorder()
	f.router.ServeHTTP(rec,req)
	var output map[string]any
	if len(rec.Body.Bytes())>0 && rec.Header().Get("Content-Type")=="application/json; charset=utf-8" {
		_ = json.Unmarshal(rec.Body.Bytes(),&output)
	}
	return rec.Code,output
}
func (f *fixture) do(method,path,token string, body any,expected int) map[string]any {
	f.t.Helper()
	code,out:=f.request(method,path,token,body,"http://localhost:3000")
	if code!=expected {f.t.Fatalf("%s %s: status %d want %d; body=%v",method,path,code,expected,out)}
	return out
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn:=os.Getenv("DATABASE_URL")
	if dsn=="" {t.Skip("DATABASE_URL required for catalog PostgreSQL integration tests")}
	ctx:=context.Background()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil {t.Fatal(err)}
	t.Cleanup(func(){pool.Close()})
	if err:=db.Migrate(ctx,pool);err!=nil {t.Fatal(err)}
	authSvc,err:=auth.NewService(config.Config{WebOrigin:"http://localhost:3000"},pool)
	if err!=nil {t.Fatal(err)}
	f:=&fixture{t:t,pool:pool,router:httpapi.NewAppRouter(pool.Ping,authSvc,"http://localhost:3000",&catalog.Service{DB:pool,Auth:authSvc,WebOrigin:"http://localhost:3000"})}
	store:=auth.Store{DB:pool}
	for _,entry:=range []struct{label string;id *string;token *string}{
		{"admin",&f.admin,&f.adminToken},
		{"manager",&f.manager,&f.managerToken},
		{"outsider",&f.outsider,&f.outsiderToken},
		{"customer",&f.customer,&f.customerToken},
	} {
		subject:=fmt.Sprintf("catalog-%s-%s",entry.label,t.Name())
		// The provider identity uses a random string to keep repeated CI runs isolated.
		nonce,err:=authRandom()
		if err!=nil {t.Fatal(err)}
		id,err:=store.ResolveIdentity(ctx,"google",subject+nonce,"",entry.label)
		if err!=nil {t.Fatal(err)}
		*entry.id=id
		t.Cleanup(func(){ _,_ = pool.Exec(context.Background(),"DELETE FROM users WHERE id=$1::uuid",id) })
		*entry.token=nonce
		if err:=store.NewSession(ctx,id,nonce);err!=nil {t.Fatal(err)}
	}
	_,err=pool.Exec(ctx,"INSERT INTO user_roles(user_id,role_name) VALUES ($1::uuid,'admin'),($2::uuid,'restaurant_manager'),($3::uuid,'restaurant_manager')",f.admin,f.manager,f.outsider)
	if err!=nil {t.Fatal(err)}
	return f
}
func authRandom() (string,error) {
	// Unique test values are independent of identity provider credentials.
	b:=make([]byte,32)
	if _,err:=cryptoRand.Read(b);err!=nil {return "",err}
	return fmt.Sprintf("%x",b),nil
}
func TestCatalogLifecycleAndTenantIsolation(t *testing.T) {
	f:=newFixture(t)
	newRestaurant:=func(name,slug string) string {
		t.Helper()
		out:=f.do("POST","/api/v1/admin/restaurants/",f.adminToken,map[string]any{
			"name":name,"slug":slug,"description":"Meal plans","currency":"AED","image_url":"","is_active":true,
		},http.StatusCreated)
		return out["id"].(string)
	}
	if code,_:=f.request("POST","/api/v1/admin/restaurants/",f.managerToken,map[string]any{
		"name":"Forbidden","slug":"forbidden","currency":"AED","is_active":true,
	},"http://localhost:3000");code!=403 {t.Fatalf("manager created restaurant: %d",code)}
	a:=newRestaurant("Main Kitchen","main-"+f.admin[:8])
	b:=newRestaurant("Other Kitchen","other-"+f.manager[:8])
	t.Cleanup(func(){_,_ = f.pool.Exec(context.Background(),"DELETE FROM restaurants WHERE id IN ($1::uuid,$2::uuid)",a,b)})
	root:="/api/v1/admin/restaurants/"+a
	categoryBody:=map[string]any{"name":"Lunch","sort_order":1,"is_active":true}
	f.do("POST",root+"/categories/",f.managerToken,categoryBody,403)
	f.do("PUT",root+"/managers/"+f.manager,f.adminToken,nil,204)
	f.do("POST",root+"/categories/",f.outsiderToken,categoryBody,403)
	category:=f.do("POST",root+"/categories/",f.managerToken,categoryBody,201)["id"].(string)
	otherCat:=f.do("POST","/api/v1/admin/restaurants/"+b+"/categories/",f.adminToken,categoryBody,201)["id"].(string)
	badMeal:=map[string]any{"category_id":otherCat,"name":"Cross tenant","description":"","price_minor":1000,"image_url":"","is_active":true,"is_available":true,"sort_order":0}
	f.do("POST",root+"/meals/",f.managerToken,badMeal,409)
	goodMeal:=map[string]any{"category_id":category,"name":"Chicken bowl","description":"Grilled chicken","price_minor":2450,"image_url":"https://example.org/lunch.jpg","is_active":true,"is_available":true,"sort_order":1}
	meal:=f.do("POST",root+"/meals/",f.managerToken,goodMeal,201)["id"].(string)
	badMeal["category_id"]=category
	badMeal["image_url"]="javascript:alert(1)"
	f.do("POST",root+"/meals/",f.managerToken,badMeal,400)
	groupPath:=root+"/meals/"+meal+"/modifier-groups/"
	group:=f.do("POST",groupPath,f.managerToken,map[string]any{"name":"Choose rice","min_select":1,"max_select":2,"sort_order":0},201)["id"].(string)
	optionPath:=groupPath+group+"/options/"
	option:=f.do("POST",optionPath,f.managerToken,map[string]any{"name":"Brown rice","price_delta_minor":250,"is_available":true,"sort_order":0},201)["id"].(string)
	if option=="" {t.Fatal("option ID missing")}
	if status,_:=f.request("DELETE",root+"/categories/"+category,f.managerToken,nil,"http://localhost:3000");status!=409 {t.Fatalf("category deletion with meals must conflict: %d",status)}
	code,menu:=f.request("GET","/api/v1/restaurants/"+a+"/menu","",nil,"")
	if code!=200 {t.Fatalf("public menu: %d %v",code,menu)}
	for _,key:=range []string{"categories","meals","modifier_groups","modifier_options"} {
		data,ok:=menu[key].([]any)
		if !ok||len(data)!=1 {t.Fatalf("expected one %s, got %v",key,menu[key])}
	}
	mealUpdate:=map[string]any{"category_id":category,"name":"Chicken bowl","description":"","price_minor":2450,"image_url":"","is_active":false,"is_available":false,"sort_order":1}
	f.do("PUT",root+"/meals/"+meal,f.managerToken,mealUpdate,200)
	_,hidden:=f.request("GET","/api/v1/restaurants/"+a+"/menu","",nil,"")
	if len(hidden["meals"].([]any))!=0 {t.Fatalf("draft meal leaked publicly: %v",hidden["meals"])}
	_,adminCatalog:=f.request("GET",root+"/catalog",f.managerToken,nil,"")
	if len(adminCatalog["meals"].([]any))!=1 {t.Fatal("manager cannot see drafts")}
	if code,_:=f.request("GET",root+"/catalog",f.outsiderToken,nil,"");code!=403 {t.Fatalf("outsider read catalog: %d",code)}
	code,_=f.request("GET","/api/v1/admin/restaurants/",f.managerToken,nil,"")
	if code!=200 {t.Fatalf("manager restaurants not accessible: %d",code)}
	f.do("DELETE",root+"/managers/"+f.manager,f.adminToken,nil,204)
	f.do("POST",root+"/categories/",f.managerToken,categoryBody,403)
	f.do("DELETE",groupPath+group,f.adminToken,nil,204)
	f.do("DELETE",root+"/meals/"+meal,f.adminToken,nil,204)
	f.do("DELETE",root+"/categories/"+category,f.adminToken,nil,204)
	f.do("DELETE",root,f.adminToken,nil,204)
	if code,_:=f.request("GET","/api/v1/restaurants/"+a+"/menu","",nil,"");code!=404 {t.Fatalf("archived restaurant still public: %d",code)}
}

func TestPublicAndUnauthenticatedCatalog(t *testing.T) {
	f:=newFixture(t)
	code,_:=f.request("GET","/api/v1/restaurants","",nil,"")
	if code!=200 {t.Fatalf("public restaurants unavailable: %d",code)}
	code,_=f.request("GET","/api/v1/admin/restaurants/","",nil,"")
	if code!=401 {t.Fatalf("anonymous admin access: %d",code)}
	code,_=f.request("POST","/api/v1/admin/restaurants/",f.customerToken,map[string]any{
		"name":"No access","slug":"noaccess","currency":"AED",
	},"http://localhost:3000")
	if code!=403 {t.Fatalf("customer created restaurant: %d",code)}
	code,_=f.request("POST","/api/v1/admin/restaurants/",f.adminToken,map[string]any{
		"name":"No CSRF protection","slug":"csrf","currency":"AED",
	},"https://malicious.example")
	if code!=403 {t.Fatalf("cross-site mutation accepted: %d",code)}
}
