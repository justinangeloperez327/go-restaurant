"use client";

import { useCallback,useEffect,useState } from "react";
import Link from "next/link";

type Restaurant={id:string;name:string};
type Rider={id:string;name:string};
type Order={id:string;created_at:string;payment_method:string;payment_status:string;address:Record<string,string>};
type Job={id:string;order_id:string;rider_id:string;rider_name:string;status:string;restaurant_name:string;created_at:string};
export function DispatchClient({apiURL}:{apiURL:string}){
 const [restaurants,setRestaurants]=useState<Restaurant[]>([]);
 const [restaurantID,setRestaurantID]=useState("");
 const [riders,setRiders]=useState<Rider[]>([]);
 const [riderID,setRiderID]=useState("");
 const [eligible,setEligible]=useState<Order[]>([]);
 const [jobs,setJobs]=useState<Job[]>([]);
 const [error,setError]=useState("");
 const [message,setMessage]=useState("");
 const [busy,setBusy]=useState(false);
 const request=useCallback(async(path:string,method="GET",body?:unknown)=>{
  const r=await fetch(apiURL+"/api/v1"+path,{
   credentials:"include",method,cache:"no-store",
   ...(body===undefined?{}:{headers:{"Content-Type":"application/json"},body:JSON.stringify(body)})
  });
  const output=(await r.json().catch(()=>({}))) as Record<string,unknown>;
  if(!r.ok)throw new Error(r.status===401?"Sign in with a restaurant manager or administrator account.":String(output.error||"Request failed."));
  return output;
 },[apiURL]);
 useEffect(()=>{
  let alive=true;
  request("/admin/restaurants/").then(items=>{
   if(!alive)return;
   const list=items as unknown as Restaurant[];
   setRestaurants(list);
   if(list.length)setRestaurantID(list[0].id);
  }).catch(e=>{if(alive)setError(e instanceof Error?e.message:"Restaurant access unavailable.");});
  return()=>{alive=false;};
 },[request]);
 const refresh=useCallback(async(id:string)=>{
  if(!id)return;
  const query="?restaurant_id="+encodeURIComponent(id);
  const [jobsData,orderData,riderData]=await Promise.all([
   request("/admin/deliveries"+query),request("/admin/deliveries/eligible-orders"+query),
   request("/admin/deliveries/riders"+query),
  ]);
  setJobs((jobsData as {deliveries:Job[]}).deliveries);
  setEligible((orderData as {orders:Order[]}).orders);
  const roster=(riderData as {riders:Rider[]}).riders;
  setRiders(roster);
  setRiderID((current)=>roster.some(x=>x.id===current)?current:roster[0]?.id||"");
 },[request]);
 useEffect(()=>{
  if(!restaurantID)return;
  let mounted=true;
  const check=async()=>{try{await refresh(restaurantID);if(mounted)setError("");}
   catch(e){if(mounted)setError(e instanceof Error?e.message:"Unable to load dispatch.");}};
  void check();
  const timer=window.setInterval(()=>{if(mounted)void check();},15000);
  return()=>{mounted=false;window.clearInterval(timer);};
 },[restaurantID,refresh]);
 async function assign(orderID:string,chosenRider:string,reassign=false){
  if(!chosenRider)return;
  if(reassign&&!window.confirm("Reassign this delivery before pickup? Previous rider GPS will be cleared."))return;
  setBusy(true);setMessage("");setError("");
  try{
   await request("/admin/deliveries/orders/"+orderID+"/assignment","PUT",{rider_id:chosenRider});
   await refresh(restaurantID);setMessage("Delivery assigned successfully.");
  }catch(e){setError(e instanceof Error?e.message:"Assignment failed.");}
  finally{setBusy(false);}
 }
 return <section className="catalog-section">
  <div className="panel">
   <label className="form-field">Restaurant
    <select value={restaurantID} onChange={e=>setRestaurantID(e.target.value)}>
     <option value="">Choose a restaurant</option>
     {restaurants.map(r=><option value={r.id} key={r.id}>{r.name}</option>)}
    </select>
   </label>
  </div>
  {error&&<p className="notice" role="alert">{error}</p>}
  {message&&<p className="notice" role="status">{message}</p>}
  {!restaurantID&&<p className="muted">No restaurants are assigned to this account.</p>}
  {restaurantID&&<>
   <section className="panel">
    <h2>Dispatch rider</h2>
    <label className="form-field">Active rider
     <select value={riderID} onChange={e=>setRiderID(e.target.value)}>
      <option value="">Select a rider</option>
      {riders.map(r=><option key={r.id} value={r.id}>{r.name} ({r.id.slice(0,8)})</option>)}
     </select>
    </label>
    {riders.length===0&&<p className="muted">No active riders. An administrator must grant users the rider role first.</p>}
    <h3>Orders ready for dispatch</h3>
    {eligible.length===0&&<p className="muted">No unassigned eligible orders.</p>}
    <div className="dispatch-orders">
     {eligible.map(o=><div className="dispatch-order" key={o.id}>
      <div>
       <Link href={"/orders/"+o.id}><strong>Order {o.id.slice(0,8)}</strong></Link>
       <p className="muted">{o.payment_method.toUpperCase()} · {o.payment_status} · {new Date(o.created_at).toLocaleString()}</p>
       <p className="muted">{[o.address.street,o.address.area,o.address.city].filter(Boolean).join(", ")}</p>
      </div>
      <button className="action" disabled={!riderID||busy} onClick={()=>void assign(o.id,riderID)}>Assign rider</button>
     </div>)}
    </div>
   </section>
   <section className="panel">
    <h2>Assigned delivery jobs</h2>
    {jobs.length===0&&<p className="muted">No delivery jobs in this restaurant.</p>}
    {jobs.map(job=><div className="dispatch-order" key={job.id}>
     <div>
      <strong>Order {job.order_id.slice(0,8)}</strong>
      <p className="muted">Rider: {job.rider_name} · {job.status.replaceAll("_"," ")}</p>
      <Link href={"/orders/"+job.order_id}>View order</Link>
     </div>
     {job.status==="assigned"&&<button className="action secondary"
      disabled={!riderID||busy||riderID===job.rider_id}
      onClick={()=>void assign(job.order_id,riderID,true)}>Reassign to selected rider</button>}
    </div>)}
   </section>
  </>}
 </section>;
}
