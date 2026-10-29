package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
)

type Service struct {
	DB *pgxpool.Pool
	Auth *auth.Service
	WebOrigin string
}
var uuidRE = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control","no-store")
	w.Header().Set("Content-Type","application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, reason string) {
	write(w,status,map[string]string{"error":reason})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Header.Get("Content-Type")!="application/json" { return errors.New("expected application/json") }
	r.Body = http.MaxBytesReader(w,r.Body,8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err:=d.Decode(dst); err!=nil {return err}
	if err:=d.Decode(&struct{}{}); err!=io.EOF{return errors.New("multiple JSON documents")}
	return nil
}
func (s Service) mutation(w http.ResponseWriter,r *http.Request) bool {
	if s.WebOrigin=="" || r.Header.Get("Origin")!=s.WebOrigin {
		fail(w,403,"invalid origin")
		return false
	}
	return true
}
func actor(r *http.Request) string { user,_:=auth.UserFromContext(r.Context());return user.ID }
func databaseError(w http.ResponseWriter,err error) {
	var pg *pgconn.PgError
	if errors.As(err,&pg) {
		switch pg.Code {
		case "23505", "23503", "23001":fail(w,409,"delivery state conflict");return
		case "23502","23514","22P02","22001":fail(w,400,"invalid delivery data");return
		}
	}
	fail(w,503,"delivery database unavailable")
}
func (s Service) Mount(r chi.Router) {
	r.With(s.Auth.Authenticated).Get("/orders/{orderID}/delivery",s.forOrder)
	r.With(s.Auth.Authenticated).Get("/deliveries/{deliveryID}",s.get)
	r.With(s.Auth.Authenticated).Get("/deliveries/{deliveryID}/events",s.stream)
	r.With(s.Auth.Authenticated).Get("/rider/deliveries",s.riderList)
	r.With(s.Auth.Authenticated).Put("/rider/deliveries/{deliveryID}/status",s.updateStatus)
	r.With(s.Auth.Authenticated).Put("/rider/deliveries/{deliveryID}/location",s.updateLocation)
	r.With(s.Auth.Authenticated).Get("/admin/deliveries",s.managerList)
	r.With(s.Auth.Authenticated).Get("/admin/deliveries/riders",s.riderChoices)
	r.With(s.Auth.Authenticated).Get("/admin/deliveries/eligible-orders",s.eligibleOrders)
	r.With(s.Auth.Authenticated).Put("/admin/deliveries/orders/{orderID}/assignment",s.assign)
}

const managerScope = `EXISTS (
 SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id
 JOIN role_permissions rp ON rp.role_name=ur.role_name
 WHERE u.id=$2::uuid AND u.is_active AND (
   (ur.role_name='admin' AND rp.permission_name='delivery.manage')
   OR (ur.role_name='restaurant_manager' AND rp.permission_name='orders.manage'
     AND EXISTS(SELECT 1 FROM restaurant_staff st WHERE st.user_id=u.id AND st.restaurant_id=$1::uuid))
 ))`
const riderScope = `($1::uuid IS NOT NULL AND EXISTS (
 SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id
 JOIN role_permissions rp ON rp.role_name=ur.role_name
 WHERE u.id=$2::uuid AND u.is_active
 AND ur.role_name='rider' AND rp.permission_name='delivery.manage'
 ))`
var readerScope = `(
 o.user_id=$2::uuid
 OR (d.rider_id=$2::uuid AND `+riderScope+`)
 OR `+strings.ReplaceAll(managerScope,"$1::uuid","d.restaurant_id")+`
)`

type Job struct {
	ID string `json:"id"`
	OrderID string `json:"order_id"`
	RestaurantID string `json:"restaurant_id"`
	RestaurantName string `json:"restaurant_name"`
	RiderID string `json:"rider_id"`
	RiderName string `json:"rider_name"`
	Status string `json:"status"`
	FailureReason string `json:"failure_reason"`
	Latitude *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	AccuracyM *float64 `json:"accuracy_m"`
	LocationAt *time.Time `json:"location_at"`
	AssignedAt time.Time `json:"assigned_at"`
	PickedUpAt *time.Time `json:"picked_up_at"`
	EnRouteAt *time.Time `json:"en_route_at"`
	CompletedAt *time.Time `json:"completed_at"`
	DeliveryAddress json.RawMessage `json:"delivery_address"`
	CreatedAt time.Time `json:"created_at"`
}
const jobSelect = `SELECT d.id::text,d.order_id::text,d.restaurant_id::text,rest.name,
 d.rider_id::text,COALESCE(NULLIF(u.display_name,''),'Rider'),d.status,d.failure_reason,
 CASE WHEN d.status IN ('assigned','picked_up','en_route') THEN d.latitude::float8 END,
 CASE WHEN d.status IN ('assigned','picked_up','en_route') THEN d.longitude::float8 END,
 CASE WHEN d.status IN ('assigned','picked_up','en_route') THEN d.location_accuracy_m::float8 END,
 CASE WHEN d.status IN ('assigned','picked_up','en_route') THEN d.location_at END,
 d.assigned_at,d.picked_up_at,d.en_route_at,d.completed_at,o.delivery_address,d.created_at
 FROM delivery_jobs d JOIN orders o ON o.id=d.order_id
 JOIN restaurants rest ON rest.id=d.restaurant_id
 JOIN users u ON u.id=d.rider_id`
func collect(rows pgx.Rows)([]Job,error){
	jobs:=[]Job{}
	for rows.Next(){
		var j Job
		if err:=rows.Scan(&j.ID,&j.OrderID,&j.RestaurantID,&j.RestaurantName,
			&j.RiderID,&j.RiderName,&j.Status,&j.FailureReason,&j.Latitude,
			&j.Longitude,&j.AccuracyM,&j.LocationAt,&j.AssignedAt,&j.PickedUpAt,
			&j.EnRouteAt,&j.CompletedAt,&j.DeliveryAddress,&j.CreatedAt);err!=nil {
			return nil,err
		}
		jobs=append(jobs,j)
	}
	return jobs,rows.Err()
}
func (s Service) readOne(ctx context.Context,id,user string)(Job,error){
	var j Job
	err:=s.DB.QueryRow(ctx,jobSelect+` WHERE d.id=$1::uuid AND `+readerScope,id,user).Scan(
		&j.ID,&j.OrderID,&j.RestaurantID,&j.RestaurantName,&j.RiderID,&j.RiderName,
		&j.Status,&j.FailureReason,&j.Latitude,&j.Longitude,&j.AccuracyM,&j.LocationAt,
		&j.AssignedAt,&j.PickedUpAt,&j.EnRouteAt,&j.CompletedAt,&j.DeliveryAddress,&j.CreatedAt)
	return j,err
}
func (s Service) get(w http.ResponseWriter,r *http.Request) {
	id:=chi.URLParam(r,"deliveryID")
	if !uuidRE.MatchString(id){fail(w,400,"invalid delivery ID");return}
	j,err:=s.readOne(r.Context(),id,actor(r))
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"delivery not found");return}
	if err!=nil{databaseError(w,err);return}
	write(w,200,j)
}
func (s Service) forOrder(w http.ResponseWriter,r *http.Request) {
	id:=chi.URLParam(r,"orderID")
	if !uuidRE.MatchString(id){fail(w,400,"invalid order ID");return}
	var deliveryID string
	err:=s.DB.QueryRow(r.Context(),`SELECT d.id::text FROM delivery_jobs d
 JOIN orders o ON o.id=d.order_id WHERE o.id=$1::uuid AND o.user_id=$2::uuid`,
 id,actor(r)).Scan(&deliveryID)
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"no delivery assignment yet");return}
	if err!=nil{databaseError(w,err);return}
	j,err:=s.readOne(r.Context(),deliveryID,actor(r))
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"delivery unavailable");return}
	if err!=nil{databaseError(w,err);return}
	write(w,200,j)
}
func (s Service) riderList(w http.ResponseWriter,r *http.Request) {
	owner:=actor(r)
	rows,err:=s.DB.Query(r.Context(),jobSelect+` WHERE d.rider_id=$2::uuid AND `+riderScope+`
	ORDER BY (d.status IN ('assigned','picked_up','en_route')) DESC,d.assigned_at DESC LIMIT 100`,
	"00000000-0000-0000-0000-000000000000",owner)
	if err!=nil{databaseError(w,err);return}
	jobs,err:=collect(rows);rows.Close()
	if err!=nil{databaseError(w,err);return}
	write(w,200,map[string]any{"deliveries":jobs})
}
func (s Service) managerRestaurant(w http.ResponseWriter,r *http.Request)(string,bool){
	id:=r.URL.Query().Get("restaurant_id")
	if !uuidRE.MatchString(id){fail(w,400,"restaurant_id UUID required");return "",false}
	var allowed bool
	err:=s.DB.QueryRow(r.Context(),"SELECT "+managerScope,id,actor(r)).Scan(&allowed)
	if err!=nil{databaseError(w,err);return "",false}
	if !allowed{fail(w,403,"restaurant management access denied");return "",false}
	return id,true
}
func (s Service) managerList(w http.ResponseWriter,r *http.Request) {
	id,ok:=s.managerRestaurant(w,r);if !ok{return}
	rows,err:=s.DB.Query(r.Context(),jobSelect+` WHERE d.restaurant_id=$1::uuid AND `+managerScope+`
	ORDER BY d.created_at DESC,d.id DESC LIMIT 100`,id,actor(r))
	if err!=nil{databaseError(w,err);return}
	jobs,err:=collect(rows);rows.Close()
	if err!=nil{databaseError(w,err);return}
	write(w,200,map[string]any{"deliveries":jobs})
}
func (s Service) eligibleOrders(w http.ResponseWriter,r *http.Request) {
	id,ok:=s.managerRestaurant(w,r);if !ok{return}
	rows,err:=s.DB.Query(r.Context(),`SELECT o.id::text,o.created_at,o.payment_method,
	o.payment_status,o.delivery_address FROM orders o
	WHERE o.restaurant_id=$1::uuid AND `+managerScope+`
	AND o.order_status='placed'
	AND ((o.payment_method='cod' AND o.payment_status='unpaid')
	OR (o.payment_method='stripe' AND o.payment_status='paid'))
	AND NOT EXISTS(SELECT 1 FROM delivery_jobs d WHERE d.order_id=o.id)
	ORDER BY o.created_at DESC,o.id DESC LIMIT 100`,id,actor(r))
	if err!=nil{databaseError(w,err);return}
	defer rows.Close()
	type item struct{ ID string `json:"id"`; CreatedAt time.Time `json:"created_at"`
		PaymentMethod string `json:"payment_method"`;PaymentStatus string `json:"payment_status"`
		Address json.RawMessage `json:"address"` }
	out:=[]item{}
	for rows.Next(){
		var x item
		if err:=rows.Scan(&x.ID,&x.CreatedAt,&x.PaymentMethod,&x.PaymentStatus,&x.Address);err!=nil{databaseError(w,err);return}
		out=append(out,x)
	}
	if err:=rows.Err();err!=nil{databaseError(w,err);return}
	write(w,200,map[string]any{"orders":out})
}
func (s Service) riderChoices(w http.ResponseWriter,r *http.Request){
	_,ok:=s.managerRestaurant(w,r);if !ok{return}
	rows,err:=s.DB.Query(r.Context(),`SELECT DISTINCT u.id::text,COALESCE(NULLIF(u.display_name,''),'Rider')
	FROM users u JOIN user_roles ur ON ur.user_id=u.id
	JOIN role_permissions rp ON rp.role_name=ur.role_name
	WHERE u.is_active AND ur.role_name='rider' AND rp.permission_name='delivery.manage'
	ORDER BY 2,1 LIMIT 200`)
	if err!=nil{databaseError(w,err);return}
	defer rows.Close()
	type rider struct{ID string `json:"id"`;Name string `json:"name"`}
	out:=[]rider{}
	for rows.Next(){var v rider;if err:=rows.Scan(&v.ID,&v.Name);err!=nil{databaseError(w,err);return};out=append(out,v)}
	if err:=rows.Err();err!=nil{databaseError(w,err);return}
	write(w,200,map[string]any{"riders":out})
}
