package operations

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
)

type Service struct {
	DB *pgxpool.Pool
	Auth *auth.Service
	WebOrigin string
}
var uuidRE=regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var local=time.FixedZone("Asia/Dubai",4*3600)

type Day struct {
	Date string `json:"date"`
	Orders int64 `json:"orders"`
	BookedMinor int64 `json:"booked_minor"`
	PaidMinor int64 `json:"paid_minor"`
	CODUncollectedMinor int64 `json:"cod_uncollected_minor"`
}
type Metrics struct {
	OrdersTotal int64 `json:"orders_total"`
	OrdersPlaced int64 `json:"orders_placed"`
	OrdersPending int64 `json:"orders_pending"`
	OrdersFailed int64 `json:"orders_failed"`
	BookedMinor int64 `json:"booked_minor"`
	PaidMinor int64 `json:"paid_minor"`
	CODUncollectedMinor int64 `json:"cod_uncollected_minor"`
	DeliveryAssigned int64 `json:"delivery_assigned"`
	DeliveryInProgress int64 `json:"delivery_in_progress"`
	DeliveryDelivered int64 `json:"delivery_delivered"`
	DeliveryFailed int64 `json:"delivery_failed"`
	SubscriptionsActive int64 `json:"subscriptions_active"`
	SubscriptionsPaused int64 `json:"subscriptions_paused"`
	SubscriptionFailures int64 `json:"subscription_failures"`
	UnassignedOrders int64 `json:"unassigned_orders"`
}
type Report struct {
	RestaurantID string `json:"restaurant_id"`
	Currency string `json:"currency"`
	From string `json:"from"`
	To string `json:"to"`
	Timezone string `json:"timezone"`
	Metrics Metrics `json:"metrics"`
	Days []Day `json:"days"`
}

func respond(w http.ResponseWriter,status int,val any){
	w.Header().Set("Cache-Control","no-store")
	w.Header().Set("Content-Type","application/json; charset=utf-8")
	w.WriteHeader(status)
	_=json.NewEncoder(w).Encode(val)
}
func fail(w http.ResponseWriter,status int,message string){respond(w,status,map[string]string{"error":message})}
func actor(r *http.Request)string{u,_:=auth.UserFromContext(r.Context());return u.ID}
const scopeSQL = `SELECT EXISTS(
 SELECT 1 FROM restaurants rest JOIN users u ON u.id=$2::uuid
 WHERE rest.id=$1::uuid AND u.is_active AND EXISTS(
 SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_name=ur.role_name
 WHERE ur.user_id=u.id AND (
 (ur.role_name='admin' AND rp.permission_name='restaurants.manage')
 OR (ur.role_name='restaurant_manager' AND rp.permission_name='orders.manage' AND
 EXISTS(SELECT 1 FROM restaurant_staff rs WHERE rs.restaurant_id=rest.id AND rs.user_id=u.id))
 ))
)`
func (s Service) restaurant(w http.ResponseWriter,r *http.Request)(string,bool){
	id:=r.URL.Query().Get("restaurant_id")
	if !uuidRE.MatchString(id){fail(w,400,"restaurant_id UUID required");return "",false}
	ok,err:=s.allowed(r.Context(),id,actor(r))
	if err!=nil{fail(w,503,"authorization unavailable");return "",false}
	if !ok{fail(w,403,"restaurant access denied");return "",false}
	return id,true
}
func (s Service) allowed(ctx context.Context,restaurant,user string)(bool,error){
	var allowed bool
	err:=s.DB.QueryRow(ctx,scopeSQL,restaurant,user).Scan(&allowed)
	return allowed,err
}
func parseRange(r *http.Request)(time.Time,time.Time,string,string,error){
	from:=r.URL.Query().Get("from")
	to:=r.URL.Query().Get("to")
	now:=time.Now().In(local)
	if to==""{to=now.Format("2006-01-02")}
	if from==""{from=now.AddDate(0,0,-29).Format("2006-01-02")}
	start,err:=time.ParseInLocation("2006-01-02",from,local)
	if err!=nil||start.Format("2006-01-02")!=from{return time.Time{},time.Time{},"","",errors.New("invalid from date")}
	end,err:=time.ParseInLocation("2006-01-02",to,local)
	if err!=nil||end.Format("2006-01-02")!=to{return time.Time{},time.Time{},"","",errors.New("invalid to date")}
	if end.Before(start)||end.Sub(start)>89*24*time.Hour||end.After(now.AddDate(0,0,1)){
		return time.Time{},time.Time{},"","",errors.New("report range must be within 90 days and cannot extend into future")
	}
	return start,end.AddDate(0,0,1),from,to,nil
}
func (s Service) Mount(r chi.Router){
	r.With(s.Auth.Authenticated).Get("/admin/operations/report",s.report)
	r.With(s.Auth.Authenticated).Get("/admin/operations/report.csv",s.reportCSV)
	r.With(s.Auth.Authenticated).Get("/admin/operations/alerts",s.alerts)
	r.With(s.Auth.Authenticated).Put("/admin/operations/alerts/{alertID}/read",s.markRead)
}
const metricSQL=`SELECT
 (SELECT count(*) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT count(*) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status='placed' AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT count(*) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status='pending_payment' AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT count(*) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status IN ('payment_failed','cancelled') AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT COALESCE(sum(total_minor),0) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status='placed' AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT COALESCE(sum(total_minor),0) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.payment_status='paid' AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT COALESCE(sum(total_minor),0) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status='placed' AND o.payment_method='cod' AND o.payment_status='unpaid' AND o.created_at>=$2 AND o.created_at<$3),
 (SELECT count(*) FROM delivery_jobs d WHERE d.restaurant_id=$1::uuid AND d.status='assigned' AND d.created_at>=$2 AND d.created_at<$3),
 (SELECT count(*) FROM delivery_jobs d WHERE d.restaurant_id=$1::uuid AND d.status IN ('picked_up','en_route') AND d.created_at>=$2 AND d.created_at<$3),
 (SELECT count(*) FROM delivery_jobs d WHERE d.restaurant_id=$1::uuid AND d.status='delivered' AND d.created_at>=$2 AND d.created_at<$3),
 (SELECT count(*) FROM delivery_jobs d WHERE d.restaurant_id=$1::uuid AND d.status='failed' AND d.created_at>=$2 AND d.created_at<$3),
 (SELECT count(*) FROM meal_subscriptions s WHERE s.restaurant_id=$1::uuid AND s.status='active'),
 (SELECT count(*) FROM meal_subscriptions s WHERE s.restaurant_id=$1::uuid AND s.status='paused'),
 (SELECT count(*) FROM meal_subscription_occurrences e JOIN meal_subscriptions s ON s.id=e.subscription_id
   WHERE s.restaurant_id=$1::uuid AND e.status='failed' AND e.created_at>=$2 AND e.created_at<$3),
 (SELECT count(*) FROM orders o WHERE o.restaurant_id=$1::uuid AND o.order_status='placed'
  AND ((o.payment_method='cod' AND o.payment_status='unpaid') OR (o.payment_method='stripe' AND o.payment_status='paid'))
  AND NOT EXISTS(SELECT 1 FROM delivery_jobs d WHERE d.order_id=o.id))
`
const dailySQL=`SELECT (o.created_at AT TIME ZONE 'Asia/Dubai')::date::text,
 count(*),
 COALESCE(sum(o.total_minor) FILTER(WHERE o.order_status='placed'),0),
 COALESCE(sum(o.total_minor) FILTER(WHERE o.payment_status='paid'),0),
 COALESCE(sum(o.total_minor) FILTER(WHERE o.order_status='placed'
  AND o.payment_method='cod' AND o.payment_status='unpaid'),0)
 FROM orders o WHERE o.restaurant_id=$1::uuid AND o.created_at>=$2 AND o.created_at<$3
 GROUP BY 1 ORDER BY 1`

func (s Service) loadReport(ctx context.Context,restaurant string,start,end time.Time,from,to string)(Report,error){
	out:=Report{RestaurantID:restaurant,Currency:"AED",From:from,To:to,Timezone:"Asia/Dubai",Days:[]Day{}}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.RepeatableRead,AccessMode:pgx.ReadOnly})
	if err!=nil{return out,err}
	defer func(){_=tx.Rollback(ctx)}()
	m:=&out.Metrics
	err=tx.QueryRow(ctx,metricSQL,restaurant,start,end).Scan(
		&m.OrdersTotal,&m.OrdersPlaced,&m.OrdersPending,&m.OrdersFailed,
		&m.BookedMinor,&m.PaidMinor,&m.CODUncollectedMinor,
		&m.DeliveryAssigned,&m.DeliveryInProgress,&m.DeliveryDelivered,&m.DeliveryFailed,
		&m.SubscriptionsActive,&m.SubscriptionsPaused,&m.SubscriptionFailures,&m.UnassignedOrders)
	if err!=nil{return out,err}
	rows,err:=tx.Query(ctx,dailySQL,restaurant,start,end)
	if err!=nil{return out,err}
	for rows.Next(){
		var d Day
		if err:=rows.Scan(&d.Date,&d.Orders,&d.BookedMinor,&d.PaidMinor,&d.CODUncollectedMinor);err!=nil{rows.Close();return out,err}
		out.Days=append(out.Days,d)
	}
	err=rows.Err();rows.Close()
	if err!=nil{return out,err}
	if err:=tx.Commit(ctx);err!=nil{return out,err}
	return out,nil
}
func (s Service) reportArgs(w http.ResponseWriter,r *http.Request)(Report,bool){
	id,ok:=s.restaurant(w,r);if !ok{return Report{},false}
	start,end,from,to,err:=parseRange(r)
	if err!=nil{fail(w,400,err.Error());return Report{},false}
	result,err:=s.loadReport(r.Context(),id,start,end,from,to)
	if err!=nil{fail(w,503,"report unavailable");return Report{},false}
	return result,true
}
func (s Service) report(w http.ResponseWriter,r *http.Request){
	v,ok:=s.reportArgs(w,r);if ok{respond(w,200,v)}
}
func (s Service) reportCSV(w http.ResponseWriter,r *http.Request){
	v,ok:=s.reportArgs(w,r);if !ok{return}
	w.Header().Set("Cache-Control","no-store")
	w.Header().Set("Content-Type","text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",fmt.Sprintf(`attachment; filename="restaurant-operations-%s-%s.csv"`,v.From,v.To))
	writer:=csv.NewWriter(w)
	_=writer.Write([]string{"date_uae","order_count","booked_aed_fils","paid_aed_fils","cod_uncollected_aed_fils"})
	for _,d:=range v.Days{
		_=writer.Write([]string{d.Date,strconv.FormatInt(d.Orders,10),strconv.FormatInt(d.BookedMinor,10),
			strconv.FormatInt(d.PaidMinor,10),strconv.FormatInt(d.CODUncollectedMinor,10)})
	}
	writer.Flush()
}
