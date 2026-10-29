package operations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Alert struct{
	ID string `json:"id"`
	Kind string `json:"kind"`
	Title string `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	Read bool `json:"read"`
}
type AlertList struct {
	Unread int64 `json:"unread"`
	Alerts []Alert `json:"alerts"`
}
// SyncAlerts is idempotent and safe across API replicas. Backfill is restricted
// to recent operational data and each source has a database unique key.
func (s Service) SyncAlerts(ctx context.Context)(int64,error){
	queries:=[]string{
		`INSERT INTO operation_alerts(restaurant_id,kind,source_id,title,created_at)
		SELECT o.restaurant_id,'order_placed',o.id,'New order placed',o.created_at
		FROM orders o WHERE o.order_status='placed' AND o.created_at>=now()-interval '30 days'
		ORDER BY o.created_at DESC LIMIT 1000
		ON CONFLICT(restaurant_id,kind,source_id) DO NOTHING`,
		`INSERT INTO operation_alerts(restaurant_id,kind,source_id,title,created_at)
		SELECT o.restaurant_id,'payment_received',o.id,'Online payment received',o.updated_at
		FROM orders o WHERE o.payment_method='stripe' AND o.payment_status='paid'
		AND o.updated_at>=now()-interval '30 days'
		ORDER BY o.updated_at DESC LIMIT 1000
		ON CONFLICT(restaurant_id,kind,source_id) DO NOTHING`,
		`INSERT INTO operation_alerts(restaurant_id,kind,source_id,title,created_at)
		SELECT o.restaurant_id,'payment_failed',o.id,'Payment failed',o.updated_at
		FROM orders o WHERE o.payment_status='failed'
		AND o.updated_at>=now()-interval '30 days'
		ORDER BY o.updated_at DESC LIMIT 1000
		ON CONFLICT(restaurant_id,kind,source_id) DO NOTHING`,
		`INSERT INTO operation_alerts(restaurant_id,kind,source_id,title,created_at)
		SELECT d.restaurant_id,'delivery_failed',d.id,'Delivery requires attention',d.updated_at
		FROM delivery_jobs d WHERE d.status='failed' AND d.updated_at>=now()-interval '30 days'
		ORDER BY d.updated_at DESC LIMIT 1000
		ON CONFLICT(restaurant_id,kind,source_id) DO NOTHING`,
		`INSERT INTO operation_alerts(restaurant_id,kind,source_id,title,created_at)
		SELECT s.restaurant_id,'subscription_failed',o.id,'Scheduled meal delivery could not be created',o.created_at
		FROM meal_subscription_occurrences o
		JOIN meal_subscriptions s ON s.id=o.subscription_id
		WHERE o.status='failed' AND o.created_at>=now()-interval '30 days'
		ORDER BY o.created_at DESC LIMIT 1000
		ON CONFLICT(restaurant_id,kind,source_id) DO NOTHING`,
	}
	var inserted int64
	for _,query:=range queries{
		tag,err:=s.DB.Exec(ctx,query)
		if err!=nil{return inserted,err}
		inserted+=tag.RowsAffected()
	}
	return inserted,nil
}
func (s Service) alerts(w http.ResponseWriter,r *http.Request){
	id,ok:=s.restaurant(w,r);if !ok{return}
	result:=AlertList{Alerts:[]Alert{}}
	err:=s.DB.QueryRow(r.Context(),`SELECT count(*) FROM operation_alerts a
	LEFT JOIN operation_alert_reads read ON read.alert_id=a.id AND read.user_id=$2::uuid
	WHERE a.restaurant_id=$1::uuid AND read.alert_id IS NULL`,id,actor(r)).Scan(&result.Unread)
	if err!=nil{fail(w,503,"notifications unavailable");return}
	rows,err:=s.DB.Query(r.Context(),`SELECT a.id::text,a.kind,a.title,a.created_at,
	read.alert_id IS NOT NULL FROM operation_alerts a
	LEFT JOIN operation_alert_reads read ON read.alert_id=a.id AND read.user_id=$2::uuid
	WHERE a.restaurant_id=$1::uuid ORDER BY a.created_at DESC,a.id DESC LIMIT 100`,id,actor(r))
	if err!=nil{fail(w,503,"notifications unavailable");return}
	for rows.Next(){
		var a Alert
		if err:=rows.Scan(&a.ID,&a.Kind,&a.Title,&a.CreatedAt,&a.Read);err!=nil{
			rows.Close();fail(w,503,"notifications unavailable");return
		}
		result.Alerts=append(result.Alerts,a)
	}
	err=rows.Err();rows.Close()
	if err!=nil{fail(w,503,"notifications unavailable");return}
	respond(w,200,result)
}
func (s Service) markRead(w http.ResponseWriter,r *http.Request){
	if s.WebOrigin==""||r.Header.Get("Origin")!=s.WebOrigin{
		fail(w,403,"invalid origin");return
	}
	id,ok:=s.restaurant(w,r);if !ok{return}
	alertID:=chi.URLParam(r,"alertID")
	if !uuidRE.MatchString(alertID){fail(w,400,"invalid alert ID");return}
	tx,err:=s.DB.Begin(r.Context())
	if err!=nil{fail(w,503,"notifications unavailable");return}
	defer func(){_=tx.Rollback(r.Context())}()
	// Recheck access within the write transaction to avoid stale manager grants.
	var allowed bool
	err=tx.QueryRow(r.Context(),scopeSQL,id,actor(r)).Scan(&allowed)
	if err!=nil{fail(w,503,"authorization unavailable");return}
	if !allowed{fail(w,403,"restaurant access denied");return}
	var source string
	err=tx.QueryRow(r.Context(),`SELECT id::text FROM operation_alerts
	WHERE id=$1::uuid AND restaurant_id=$2::uuid`,alertID,id).Scan(&source)
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"notification not found");return}
	if err!=nil{fail(w,503,"notifications unavailable");return}
	_,err=tx.Exec(r.Context(),`INSERT INTO operation_alert_reads(alert_id,user_id)
	VALUES($1::uuid,$2::uuid) ON CONFLICT(alert_id,user_id) DO NOTHING`,alertID,actor(r))
	if err!=nil{fail(w,503,"notifications unavailable");return}
	if err=tx.Commit(r.Context());err!=nil{fail(w,503,"notifications unavailable");return}
	w.WriteHeader(http.StatusNoContent)
}
