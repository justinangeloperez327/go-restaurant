"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";

type Restaurant = { id: string; name: string; currency: string };
type Metrics = {
 orders_total:number; orders_placed:number; orders_pending:number; orders_failed:number;
 booked_minor:number; paid_minor:number; cod_uncollected_minor:number;
 delivery_assigned:number; delivery_in_progress:number; delivery_delivered:number; delivery_failed:number;
 subscriptions_active:number; subscriptions_paused:number; subscription_failures:number; unassigned_orders:number;
};
type Day = { date:string; orders:number; booked_minor:number; paid_minor:number; cod_uncollected_minor:number };
type Report = { restaurant_id:string;currency:string;from:string;to:string;timezone:string;metrics:Metrics;days:Day[] };
type Alert = { id:string;kind:string;title:string;created_at:string;read:boolean };
type Inbox = { unread:number; alerts:Alert[] };
const today = ()=>new Date().toLocaleDateString("sv-SE",{timeZone:"Asia/Dubai"});
const daysAgo=(days:number)=>{const d=new Date();d.setUTCDate(d.getUTCDate()-days);return d.toLocaleDateString("sv-SE",{timeZone:"Asia/Dubai"});};
const currency=(minor:number)=>new Intl.NumberFormat("en-AE",{style:"currency",currency:"AED"}).format(minor/100);
const count=(v:number)=>new Intl.NumberFormat("en-AE").format(v);
const typeName:Record<string,string>={order_placed:"New order",payment_received:"Payment received",
 payment_failed:"Payment failed",delivery_failed:"Delivery issue",subscription_failed:"Subscription issue"};

export function OperationsClient({apiURL}:{apiURL:string}){
 const [restaurants,setRestaurants]=useState<Restaurant[]>([]);
 const [restaurantID,setRestaurantID]=useState("");
 const [from,setFrom]=useState(()=>daysAgo(29));
 const [to,setTo]=useState(today);
 const [report,setReport]=useState<Report|null>(null);
 const [inbox,setInbox]=useState<Inbox|null>(null);
 const [error,setError]=useState("");
 const [busy,setBusy]=useState(false);
 const [loading,setLoading]=useState(true);

 const request=useCallback(async(path:string,method="GET")=>{
  const response=await fetch(apiURL+"/api/v1"+path,{method,credentials:"include",cache:"no-store"});
  if(!response.ok){
   const data=(await response.json().catch(()=>({}))) as {error?:string};
   throw new Error(response.status===401?"Sign in as a restaurant manager or administrator.":data.error||"Unable to load operations.");
  }
  return response.status===204?null:response.json();
 },[apiURL]);

 useEffect(()=>{
  let live=true;
  request("/admin/restaurants/")
   .then(data=>{
    if(!live)return;
    const rows=data as Restaurant[];
    setRestaurants(rows);
    setRestaurantID(rows[0]?.id||"");
   })
   .catch(e=>{if(live)setError(e instanceof Error?e.message:"Access denied.");})
   .finally(()=>{if(live)setLoading(false);});
  return()=>{live=false;};
 },[request]);
 const params=useMemo(()=>new URLSearchParams({restaurant_id:restaurantID,from,to}).toString(),[restaurantID,from,to]);
 const load=useCallback(async()=>{
  if(!restaurantID)return;
  const query="?"+params;
  const [a,b]=await Promise.all([
   request("/admin/operations/report"+query),
   request("/admin/operations/alerts?restaurant_id="+encodeURIComponent(restaurantID))
  ]);
  setReport(a as Report);
  setInbox(b as Inbox);
 },[request,params,restaurantID]);
 useEffect(()=>{
  if(!restaurantID)return;
  let active=true;
  const refresh=async()=>{
   try{await load();if(active)setError("");}
   catch(e){if(active)setError(e instanceof Error?e.message:"Could not load report.");}
  };
  void refresh();
  const interval=window.setInterval(()=>{if(active)void refresh();},30000);
  return()=>{active=false;window.clearInterval(interval);};
 },[load,restaurantID]);
 async function readAlert(id:string){
  setBusy(true);setError("");
  try{
   await request("/admin/operations/alerts/"+id+"/read?restaurant_id="+encodeURIComponent(restaurantID),"PUT");
   const result=await request("/admin/operations/alerts?restaurant_id="+encodeURIComponent(restaurantID)) as Inbox;
   setInbox(result);
  }catch(e){setError(e instanceof Error?e.message:"Could not mark alert read.");}
  finally{setBusy(false);}
 }
 const max=Math.max(1,...(report?.days??[]).map(d=>d.booked_minor));
 const csvURL=apiURL+"/api/v1/admin/operations/report.csv?"+params;
 const metric=report?.metrics;
 return <section className="catalog-section operations-layout">
  <div className="panel operations-filters">
   <label className="form-field">Restaurant
    <select value={restaurantID} onChange={e=>setRestaurantID(e.target.value)}>
     <option value="">Choose a restaurant</option>
     {restaurants.map(r=><option key={r.id} value={r.id}>{r.name}</option>)}
    </select>
   </label>
   <label className="form-field">From (UAE local)
    <input type="date" value={from} max={today()} onChange={e=>setFrom(e.target.value)} />
   </label>
   <label className="form-field">To (UAE local)
    <input type="date" value={to} max={today()} onChange={e=>setTo(e.target.value)} />
   </label>
   {restaurantID&&<a className="action secondary" href={csvURL}>Export daily CSV</a>}
  </div>
  {loading&&<p className="muted">Loading restaurants…</p>}
  {error&&<p role="alert" className="notice">{error}</p>}
  {!loading&&restaurants.length===0&&<p className="muted">No restaurants are assigned to this account.</p>}
  {report&&metric&&<>
   <div className="operations-headline">
    <div><span className="eyebrow">PERIOD: {report.from} – {report.to}</span>
     <h2>Overview</h2><p className="muted">AED · {report.timezone} · booked value is not collected revenue</p></div>
   </div>
   <div className="operations-metrics">
    {[
     ["Placed orders",count(metric.orders_placed),"Orders in placed state"],
     ["Booked order value",currency(metric.booked_minor),"Placed order totals — not receipts"],
     ["Verified paid",currency(metric.paid_minor),"Orders with verified payments"],
     ["Uncollected COD",currency(metric.cod_uncollected_minor),"Placed cash orders still unpaid"],
     ["Pending payment",count(metric.orders_pending),"Orders awaiting payment"],
     ["Unsuccessful orders",count(metric.orders_failed),"Failed or cancelled"],
     ["Unassigned backlog",count(metric.unassigned_orders),"Current eligible order backlog"],
     ["Active deliveries",count(metric.delivery_assigned+metric.delivery_in_progress),"Assigned / picked up / en route"],
     ["Delivered",count(metric.delivery_delivered),"Current delivery status"],
     ["Delivery failures",count(metric.delivery_failed),"Current failed deliveries"],
     ["Active subscriptions",count(metric.subscriptions_active),"Current recurring meal plans"],
     ["Subscription failures",count(metric.subscription_failures),"Failed delivery occurrences in period"],
    ].map(([label,value,description])=><div className="operations-metric" key={label}>
     <span className="muted">{label}</span><strong>{value}</strong><small className="muted">{description}</small>
    </div>)}
   </div>
   <section className="panel">
    <div className="operations-headline"><div><h2>Daily order value</h2>
     <p className="muted">Booked value by order creation date (UAE). For reconciliation, use the table below.</p></div>
    </div>
    {report.days.length===0?<p className="muted">No orders were created in the selected period.</p>:
    <div className="operations-bars" role="img" aria-label="Booked order value by day">
     {report.days.map(d=><div className="operations-bar-row" key={d.date}>
      <span>{d.date.slice(5)}</span>
      <div className="operations-bar-track"><div style={{width:Math.max(0,(d.booked_minor/max)*100)+"%"}}/></div>
      <strong>{currency(d.booked_minor)}</strong>
     </div>)}
    </div>}
    <div className="operations-table-wrap"><table className="operations-table">
     <thead><tr><th>Date</th><th>Orders</th><th>Booked</th><th>Paid</th><th>COD unpaid</th></tr></thead>
     <tbody>{report.days.map(d=><tr key={d.date}>
      <td>{d.date}</td><td>{count(d.orders)}</td>
      <td>{currency(d.booked_minor)}</td><td>{currency(d.paid_minor)}</td>
      <td>{currency(d.cod_uncollected_minor)}</td>
     </tr>)}</tbody>
    </table></div>
   </section>
  </>}
  <section className="panel">
   <div className="operations-headline">
    <div><h2>Operational notifications</h2>
     <p className="muted">In-app alerts from recent orders, payments, deliveries and subscriptions.</p></div>
    <span className="tag">{count(inbox?.unread??0)} unread</span>
   </div>
   {!inbox||inbox.alerts.length===0?<p className="muted">No operational alerts yet.</p>:
    inbox.alerts.map(alert=><div key={alert.id} className={"operations-alert"+(alert.read?" is-read":"")}>
     <div><span className="eyebrow">{typeName[alert.kind]||alert.kind}</span>
      <strong>{alert.title}</strong><span className="muted">{new Date(alert.created_at).toLocaleString()}</span></div>
     {alert.read?<span className="muted">Read</span>:
      <button className="action secondary" disabled={busy} onClick={()=>void readAlert(alert.id)}>Mark read</button>}
    </div>)}
  </section>
  <div className="record-actions"><Link href="/admin/deliveries" className="action secondary">Delivery dispatch</Link>
   <Link href="/admin/catalog" className="action secondary">Menu management</Link></div>
 </section>;
}
