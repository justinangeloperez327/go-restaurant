package delivery

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Assignment struct{ RiderID string `json:"rider_id"` }
type StatusInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type LocationInput struct {
	Latitude *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	AccuracyM *float64 `json:"accuracy_m"`
}

func (s Service) assign(w http.ResponseWriter,r *http.Request){
	if !s.mutation(w,r){return}
	orderID:=chi.URLParam(r,"orderID")
	if !uuidRE.MatchString(orderID){fail(w,400,"invalid order ID");return}
	var input Assignment
	if decode(w,r,&input)!=nil||!uuidRE.MatchString(input.RiderID){fail(w,400,"invalid rider assignment");return}
	ctx:=r.Context()
	tx,err:=s.DB.Begin(ctx);if err!=nil{databaseError(w,err);return}
	defer func(){_=tx.Rollback(ctx)}()
	// Lock the order so two managers cannot create two delivery jobs for it.
	var restaurantID,orderStatus,paymentMethod,paymentStatus string
	err=tx.QueryRow(ctx,`SELECT restaurant_id::text,order_status,payment_method,payment_status
	FROM orders WHERE id=$1::uuid FOR UPDATE`,orderID).
		Scan(&restaurantID,&orderStatus,&paymentMethod,&paymentStatus)
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"order not found");return}
	if err!=nil{databaseError(w,err);return}
	var allowed,riderAllowed bool
	if err=tx.QueryRow(ctx,"SELECT "+managerScope,restaurantID,actor(r)).Scan(&allowed);err!=nil{databaseError(w,err);return}
	if !allowed{fail(w,403,"restaurant assignment access denied");return}
	if orderStatus!="placed"||!((paymentMethod=="cod"&&paymentStatus=="unpaid")||
		(paymentMethod=="stripe"&&paymentStatus=="paid")){
		fail(w,409,"only placed, payment-eligible orders may be dispatched");return
	}
	if err=tx.QueryRow(ctx,"SELECT "+riderScope,restaurantID,input.RiderID).Scan(&riderAllowed);err!=nil{databaseError(w,err);return}
	if !riderAllowed{fail(w,409,"selected user is not an active rider");return}
	var id,oldRider,status string
	err=tx.QueryRow(ctx,`SELECT id::text,rider_id::text,status FROM delivery_jobs
	WHERE order_id=$1::uuid FOR UPDATE`,orderID).Scan(&id,&oldRider,&status)
	event:="assigned"
	switch {
	case errors.Is(err,pgx.ErrNoRows):
		err=tx.QueryRow(ctx,`INSERT INTO delivery_jobs(order_id,restaurant_id,rider_id)
		VALUES($1::uuid,$2::uuid,$3::uuid) RETURNING id::text`,
			orderID,restaurantID,input.RiderID).Scan(&id)
		if err!=nil{databaseError(w,err);return}
	case err!=nil:
		databaseError(w,err);return
	case status!="assigned":
		fail(w,409,"delivery already started; reassignment is unavailable");return
	case oldRider==input.RiderID:
		if err=tx.Commit(ctx);err!=nil{databaseError(w,err);return}
		j,err:=s.readOne(ctx,id,actor(r));if err!=nil{databaseError(w,err);return}
		write(w,200,j);return
	default:
		event="reassigned"
		_,err=tx.Exec(ctx,`UPDATE delivery_jobs SET rider_id=$2::uuid,assigned_at=now(),
		latitude=NULL,longitude=NULL,location_accuracy_m=NULL,location_at=NULL,
		updated_at=now() WHERE id=$1::uuid AND status='assigned'`,id,input.RiderID)
		if err!=nil{databaseError(w,err);return}
	}
	_,err=tx.Exec(ctx,`INSERT INTO delivery_events(delivery_id,event_type,actor_id)
	VALUES($1::uuid,$2,$3::uuid)`,id,event,actor(r))
	if err!=nil{databaseError(w,err);return}
	if err=tx.Commit(ctx);err!=nil{databaseError(w,err);return}
	j,err:=s.readOne(ctx,id,actor(r));if err!=nil{databaseError(w,err);return}
	write(w,200,j)
}

func (s Service) updateStatus(w http.ResponseWriter,r *http.Request){
	if !s.mutation(w,r){return}
	id:=chi.URLParam(r,"deliveryID")
	if !uuidRE.MatchString(id){fail(w,400,"invalid delivery ID");return}
	var input StatusInput
	if decode(w,r,&input)!=nil || len([]rune(input.Reason))>250 ||
		(input.Status!="picked_up"&&input.Status!="en_route"&&input.Status!="delivered"&&input.Status!="failed"){
		fail(w,400,"invalid delivery status");return
	}
	if input.Status=="failed" && len([]rune(input.Reason))<4 {fail(w,400,"failure reason required");return}
	if input.Status!="failed" && input.Reason!="" {fail(w,400,"reason only allowed when failing");return}
	ctx:=r.Context()
	tx,err:=s.DB.Begin(ctx);if err!=nil{databaseError(w,err);return}
	defer func(){_=tx.Rollback(ctx)}()
	var current,restaurantID string
	err=tx.QueryRow(ctx,`SELECT status,restaurant_id::text FROM delivery_jobs
	WHERE id=$1::uuid AND rider_id=$2::uuid FOR UPDATE`,id,actor(r)).Scan(&current,&restaurantID)
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"delivery not assigned to this rider");return}
	if err!=nil{databaseError(w,err);return}
	var allowed bool
	if err=tx.QueryRow(ctx,"SELECT "+riderScope,restaurantID,actor(r)).Scan(&allowed);err!=nil{databaseError(w,err);return}
	if !allowed{fail(w,403,"rider permission required");return}
	eligible:=input.Status=="failed"&&(current=="assigned"||current=="picked_up"||current=="en_route")||
		(current=="assigned"&&input.Status=="picked_up")||
		(current=="picked_up"&&input.Status=="en_route")||
		(current=="en_route"&&input.Status=="delivered")
	if !eligible{fail(w,409,"invalid delivery state transition");return}
	var timestamp string
	switch input.Status {
	case "picked_up": timestamp="picked_up_at=now(),"
	case "en_route":timestamp="en_route_at=now(),"
	default:timestamp="completed_at=now(),"
	}
	// This switch is internal and cannot be influenced by untrusted SQL input.
	query:=`UPDATE delivery_jobs SET status=$2, failure_reason=$3,`+timestamp+`
	updated_at=now() WHERE id=$1::uuid`
	if input.Status=="failed"||input.Status=="delivered" {
		query=`UPDATE delivery_jobs SET status=$2,failure_reason=$3,`+timestamp+`
		latitude=NULL,longitude=NULL,location_accuracy_m=NULL,location_at=NULL,
		updated_at=now() WHERE id=$1::uuid`
	}
	if _,err=tx.Exec(ctx,query,id,input.Status,input.Reason);err!=nil{databaseError(w,err);return}
	if _,err=tx.Exec(ctx,`INSERT INTO delivery_events(delivery_id,event_type,actor_id)
	VALUES($1::uuid,$2,$3::uuid)`,id,input.Status,actor(r));err!=nil{databaseError(w,err);return}
	if err=tx.Commit(ctx);err!=nil{databaseError(w,err);return}
	j,err:=s.readOne(ctx,id,actor(r));if err!=nil{databaseError(w,err);return}
	write(w,200,j)
}
func validCoordinate(v *float64,minimum,maximum float64)bool{
	return v!=nil&&!math.IsNaN(*v)&&!math.IsInf(*v,0)&&*v>=minimum&&*v<=maximum
}
func (s Service) updateLocation(w http.ResponseWriter,r *http.Request){
	if !s.mutation(w,r){return}
	id:=chi.URLParam(r,"deliveryID")
	if !uuidRE.MatchString(id){fail(w,400,"invalid delivery ID");return}
	var x LocationInput
	if decode(w,r,&x)!=nil || !validCoordinate(x.Latitude,-90,90) ||
		!validCoordinate(x.Longitude,-180,180)||!validCoordinate(x.AccuracyM,0,1000){
		fail(w,400,"invalid GPS coordinates or accuracy");return
	}
	ctx:=r.Context()
	tx,err:=s.DB.Begin(ctx);if err!=nil{databaseError(w,err);return}
	defer func(){_=tx.Rollback(ctx)}()
	var status,restaurantID string
	var last *time.Time
	err=tx.QueryRow(ctx,`SELECT status,restaurant_id::text,location_at FROM delivery_jobs
	WHERE id=$1::uuid AND rider_id=$2::uuid FOR UPDATE`,id,actor(r)).
		Scan(&status,&restaurantID,&last)
	if errors.Is(err,pgx.ErrNoRows){fail(w,404,"delivery not assigned to this rider");return}
	if err!=nil{databaseError(w,err);return}
	var allowed bool
	if err=tx.QueryRow(ctx,"SELECT "+riderScope,restaurantID,actor(r)).Scan(&allowed);err!=nil{databaseError(w,err);return}
	if !allowed{fail(w,403,"rider permission required");return}
	if status!="assigned"&&status!="picked_up"&&status!="en_route"{
		fail(w,409,"location sharing stopped after delivery completion");return
	}
	if last!=nil && time.Since(*last)<5*time.Second {
		fail(w,429,"GPS updates limited to once every five seconds");return
	}
	_,err=tx.Exec(ctx,`UPDATE delivery_jobs SET latitude=$2,longitude=$3,
	location_accuracy_m=$4,location_at=now(),updated_at=now() WHERE id=$1::uuid`,
	id,*x.Latitude,*x.Longitude,*x.AccuracyM)
	if err!=nil{databaseError(w,err);return}
	_,err=tx.Exec(ctx,`INSERT INTO delivery_events(delivery_id,event_type,actor_id)
	VALUES($1::uuid,'location',$2::uuid)`,id,actor(r))
	if err!=nil{databaseError(w,err);return}
	if err=tx.Commit(ctx);err!=nil{databaseError(w,err);return}
	w.WriteHeader(http.StatusNoContent)
}
