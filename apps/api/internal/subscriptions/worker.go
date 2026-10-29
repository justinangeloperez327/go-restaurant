package subscriptions

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
)

type snapshotLine struct {
 MealID string
 Name string
 Quantity int
 Base int64
 Unit int64
 Options []string
 Modifiers []customer.OptionSummary
}

func loadLines(ctx context.Context,tx pgx.Tx,sourceID,restaurantID string,subscription bool)([]snapshotLine,int64,error){
 query:=`SELECT ci.meal_id::text,m.name,m.price_minor,ci.quantity,ci.id::text
 FROM cart_items ci JOIN meals m ON m.id=ci.meal_id
 WHERE ci.user_id=$1::uuid ORDER BY ci.created_at,ci.id`
 if subscription{
  query=`SELECT si.meal_id::text,m.name,m.price_minor,si.quantity,si.id::text
  FROM meal_subscription_items si JOIN meals m ON m.id=si.meal_id
  WHERE si.subscription_id=$1::uuid ORDER BY si.id`
 }
 rows,err:=tx.Query(ctx,query,sourceID)
 if err!=nil{return nil,0,err}
 type raw struct {MealID,Name,ID string;Base int64;Qty int}
 var data []raw
 for rows.Next(){
  var v raw
  if err:=rows.Scan(&v.MealID,&v.Name,&v.Base,&v.Qty,&v.ID);err!=nil{rows.Close();return nil,0,err}
  data=append(data,v)
 }
 err=rows.Err();rows.Close()
 if err!=nil{return nil,0,err}
 if len(data)==0||len(data)>40{return nil,0,errUnavailable}
 out:=make([]snapshotLine,0,len(data))
 var subtotal int64
 quantities:=0
 for _,v:=range data{
  quantities+=v.Qty
  if v.Qty<1||v.Qty>50||quantities>100{return nil,0,errUnavailable}
  optionSQL:=`SELECT option_id::text FROM cart_item_options
   WHERE cart_item_id=$1::uuid ORDER BY option_id`
  if subscription{
   optionSQL=`SELECT option_id::text FROM meal_subscription_item_options
    WHERE subscription_item_id=$1::uuid ORDER BY option_id`
  }
  optRows,err:=tx.Query(ctx,optionSQL,v.ID)
  if err!=nil{return nil,0,err}
  ids:=[]string{}
  for optRows.Next(){
   var id string
   if err:=optRows.Scan(&id);err!=nil{optRows.Close();return nil,0,err}
   ids=append(ids,id)
  }
  err=optRows.Err();optRows.Close()
  if err!=nil{return nil,0,err}
  if len(ids)>30{return nil,0,errUnavailable}
  unit,mods,err:=customer.PriceSelection(ctx,tx,restaurantID,v.MealID,ids)
  if customer.IsUnavailable(err){return nil,0,errUnavailable}
  if err!=nil{return nil,0,err}
  if unit<0||unit>1000000000{return nil,0,errUnavailable}
  line:=snapshotLine{MealID:v.MealID,Name:v.Name,Quantity:v.Qty,Base:v.Base,
   Unit:unit,Options:ids,Modifiers:mods}
  out=append(out,line)
  subtotal+=unit*int64(v.Qty)
 }
 if subtotal<1||subtotal>1000000000{return nil,0,errUnavailable}
 return out,subtotal,nil
}
func cartSnapshot(ctx context.Context,tx pgx.Tx,userID,restaurantID string)([]snapshotLine,int64,error){
 return loadLines(ctx,tx,userID,restaurantID,false)
}
func (s Service) RunDue(ctx context.Context,now time.Time,max int)(int,error){
 if !s.AllowCOD {return 0,nil}
 if max<1||max>100{max=20}
 count:=0
 for count<max{
  processed,err:=s.processOne(ctx,now)
  if err!=nil{return count,err}
  if !processed{break}
  count++
 }
 return count,nil
}
func (s Service) processOne(ctx context.Context,now time.Time)(bool,error){
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.RepeatableRead})
 if err!=nil{return false,err}
 defer func(){_=tx.Rollback(ctx)}()
 var subID,userID,restaurantID,addressID,clock string
 var day,period time.Time
 var mask int
 var renew bool
 var maxTotal int64
 var notes string
 err=tx.QueryRow(ctx,`SELECT id::text,user_id::text,restaurant_id::text,address_id::text,
 delivery_time,next_delivery_date,weekday_mask,current_period_end,auto_renew,
 max_total_minor,notes
 FROM meal_subscriptions WHERE status='active' AND next_run_at<=$1
 ORDER BY next_run_at,id FOR UPDATE SKIP LOCKED LIMIT 1`,now).
 Scan(&subID,&userID,&restaurantID,&addressID,&clock,&day,&mask,&period,&renew,&maxTotal,&notes)
 if errors.Is(err,pgx.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 scheduled:=deliveryAt(day,clock)
 dateStr:=day.Format("2006-01-02")
 reason:=""
 var orderID *string
 var skip bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM meal_subscription_skips
 WHERE subscription_id=$1::uuid AND delivery_date=$2::date)`,subID,dateStr).Scan(&skip)
 if err!=nil{return false,err}
 switch{
 case skip:
  reason="requested"
 case !now.Before(scheduled):
  // Never generate surprise, backdated deliveries following downtime.
  reason="missed_delivery_window"
 default:
  var address json.RawMessage
  err=tx.QueryRow(ctx,`SELECT to_jsonb(a)-'id'-'user_id'-'is_default'-'created_at'-'updated_at'
   FROM delivery_addresses a WHERE a.id=$1::uuid AND a.user_id=$2::uuid
   AND a.country_code='AE'`,addressID,userID).Scan(&address)
  if errors.Is(err,pgx.ErrNoRows){reason="address_unavailable"}else if err!=nil{return false,err}
  if reason==""{
   lines,subtotal,err:=loadLines(ctx,tx,subID,restaurantID,true)
   if errors.Is(err,errUnavailable){reason="meal_or_modifier_unavailable"}else if err!=nil{return false,err}
   if reason==""{
    total:=s.total(subtotal)
    if total>maxTotal||total<1{reason="price_cap_exceeded"}else{
     var oid string
     idempotencyKey:="sub-"+subID+"-"+day.Format("20060102")
     tax:=total-subtotal-s.DeliveryFeeMinor
     err=tx.QueryRow(ctx,`INSERT INTO orders(user_id,restaurant_id,address_id,delivery_address,notes,
       idempotency_key,payment_method,order_status,payment_status,currency,subtotal_minor,
       delivery_fee_minor,tax_minor,total_minor,subscription_id,delivery_scheduled_at)
       VALUES($1::uuid,$2::uuid,$3::uuid,$4::jsonb,$5,$6,'cod','placed','unpaid','AED',
       $7,$8,$9,$10,$11::uuid,$12) RETURNING id::text`,
       userID,restaurantID,addressID,address,notes,idempotencyKey,subtotal,s.DeliveryFeeMinor,
       tax,total,subID,scheduled).Scan(&oid)
     if err!=nil{return false,err}
     for _,line:=range lines{
      raw,err:=json.Marshal(line.Modifiers)
      if err!=nil{return false,err}
      _,err=tx.Exec(ctx,`INSERT INTO order_items(order_id,meal_id,meal_name,quantity,
        base_price_minor,unit_price_minor,line_total_minor,selected_modifiers)
        VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8::jsonb)`,
        oid,line.MealID,line.Name,line.Quantity,line.Base,line.Unit,line.Unit*int64(line.Quantity),raw)
      if err!=nil{return false,err}
     }
     orderID=&oid
    }
   }
  }
 }
 status:="created"
 if reason!=""{
  status="failed"
  if skip||reason=="missed_delivery_window"{status="skipped"}
 }
 _,err=tx.Exec(ctx,`INSERT INTO meal_subscription_occurrences(subscription_id,delivery_date,status,order_id,reason)
 VALUES($1::uuid,$2::date,$3,$4::uuid,$5)`,subID,dateStr,status,orderID,reason)
 if err!=nil{return false,err}
 next:=nextDate(dateOnly(day).AddDate(0,0,1),mask)
 nextStatus:="active"
 if !next.Before(period){
  if !renew{nextStatus="ended"}else{
   for !next.Before(period){period=period.AddDate(0,0,7)}
  }
 }
 _,err=tx.Exec(ctx,`UPDATE meal_subscriptions SET next_delivery_date=$2::date,
 next_run_at=$3,current_period_end=$4::date,status=$5,updated_at=now()
 WHERE id=$1::uuid`,subID,next.Format("2006-01-02"),deliveryAt(next,clock).Add(-cutoff),
 period.Format("2006-01-02"),nextStatus)
 if err!=nil{return false,err}
 if err=tx.Commit(ctx);err!=nil{return false,fmt.Errorf("commit subscription delivery: %w",err)}
 return true,nil
}
