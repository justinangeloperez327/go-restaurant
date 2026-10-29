"use client";

import { useCallback, useEffect, useState } from "react";

type Delivery = {
 id:string; order_id:string; status:string; rider_name:string;
 latitude:number|null; longitude:number|null; accuracy_m:number|null; location_at:string|null;
 assigned_at:string; picked_up_at:string|null; en_route_at:string|null;
 completed_at:string|null; failure_reason:string;
};

const stages = ["assigned","picked_up","en_route","delivered"];
const stageLabel:Record<string,string> = {
 assigned:"Rider assigned", picked_up:"Picked up from restaurant",
 en_route:"On the way", delivered:"Delivered", failed:"Delivery issue",
};
export function DeliveryTracking({apiURL,orderID}:{apiURL:string;orderID:string}){
 const [delivery,setDelivery]=useState<Delivery|null>(null);
 const [error,setError]=useState("");
 const [available,setAvailable]=useState(false);
 const refresh=useCallback(async()=>{
  const r=await fetch(apiURL+"/api/v1/orders/"+encodeURIComponent(orderID)+"/delivery",{
   credentials:"include",cache:"no-store"
  });
  if(r.status===404){setAvailable(false);setDelivery(null);return;}
  if(!r.ok)throw new Error(r.status===401?"Sign in to track your order.":"Delivery tracking unavailable.");
  setDelivery((await r.json()) as Delivery);
  setAvailable(true);
 },[apiURL,orderID]);
 useEffect(()=>{
  let live=true;
  const check=async()=>{
   try{await refresh();if(live)setError("");}
   catch(e){if(live)setError(e instanceof Error?e.message:"Tracking unavailable.");}
  };
  void check();
  const timer=window.setInterval(()=>{if(live)void check();},15000);
  return()=>{live=false;window.clearInterval(timer);};
 },[refresh]);
 useEffect(()=>{
  if(!delivery?.id)return;
  const events=new EventSource(apiURL+"/api/v1/deliveries/"+encodeURIComponent(delivery.id)+"/events",{withCredentials:true});
  const update=()=>{void refresh().catch(()=>setError("Live tracking disconnected; checking periodically."));};
  events.addEventListener("ready",update);
  events.addEventListener("delivery-update",update);
  events.onerror=()=>setError("Live connection interrupted. Automatic refresh remains active.");
  return()=>{events.close();};
 },[apiURL,delivery?.id,refresh]);
 const step=delivery?stages.indexOf(delivery.status):-1;
 const hasGPS=delivery?.latitude!=null&&delivery.longitude!=null;
 const externalMap=hasGPS?
  "https://www.openstreetmap.org/?mlat="+delivery.latitude+"&mlon="+delivery.longitude+
  "#map=15/"+delivery.latitude+"/"+delivery.longitude:"";
 return <section className="panel delivery-tracking" aria-live="polite">
  <h2>Delivery tracking</h2>
  {error&&<p className="notice" role="status">{error}</p>}
  {!available&&<p className="muted">Rider assignment has not been made yet. This page checks automatically for updates.</p>}
  {delivery&&<>
   <div className="delivery-heading"><strong>{stageLabel[delivery.status]||delivery.status}</strong>
    <span className="tag">Assigned to {delivery.rider_name}</span></div>
   {delivery.status==="failed"&&<p className="notice">Delivery issue: {delivery.failure_reason}</p>}
   <div className="delivery-steps">
    {stages.map((stage,index)=><div className={"delivery-step "+(index<=step?"done":"")} key={stage}>
     <span className="delivery-step-number">{index+1}</span><span>{stageLabel[stage]}</span>
    </div>)}
   </div>
   {hasGPS&&delivery.status!=="delivered"&&delivery.status!=="failed"&&<div className="delivery-location">
    <p><strong>Live rider position:</strong> {delivery.latitude?.toFixed(5)}, {delivery.longitude?.toFixed(5)}
     {delivery.accuracy_m!=null&&<span className="muted"> · accuracy ±{Math.round(delivery.accuracy_m)} m</span>}</p>
    {delivery.location_at&&<p className="muted">Last GPS update: {new Date(delivery.location_at).toLocaleString()}</p>}
    <a href={externalMap} target="_blank" rel="noopener noreferrer" className="action secondary">Open location in OpenStreetMap</a>
    <p className="muted">The external map receives the location only when you open it.</p>
   </div>}
   {!hasGPS&&delivery.status!=="delivered"&&delivery.status!=="failed"&&<p className="muted">The rider has not enabled GPS sharing for this delivery.</p>}
   {(delivery.status==="delivered"||delivery.status==="failed")&&<p className="muted">Live GPS sharing has ended. Exact rider coordinates are no longer retained on this delivery.</p>}
  </>}
 </section>;
}
