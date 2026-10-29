"use client";

import { useCallback,useEffect,useRef,useState } from "react";
import Link from "next/link";

type Job={
 id:string;order_id:string;restaurant_name:string;status:string;
 delivery_address:Record<string,string>;failure_reason:string;
};
export function RiderClient({apiURL}:{apiURL:string}){
 const [jobs,setJobs]=useState<Job[]>([]);
 const [error,setError]=useState("");
 const [notice,setNotice]=useState("");
 const [busy,setBusy]=useState(false);
 const [failureReason,setFailureReason]=useState("");
 const [sharingID,setSharingID]=useState("");
 const watchRef=useRef<number|null>(null);
 const lastSent=useRef(0);

 const refresh=useCallback(async()=>{
  const res=await fetch(apiURL+"/api/v1/rider/deliveries",{credentials:"include",cache:"no-store"});
  if(!res.ok)throw new Error(res.status===401?"Sign in with an active rider account.":"Could not load deliveries.");
  const result=(await res.json()) as {deliveries:Job[]};
  setJobs(result.deliveries);
 },[apiURL]);
 useEffect(()=>{
  let alive=true;
  const load=async()=>{try{await refresh();if(alive)setError("");}
   catch(e){if(alive)setError(e instanceof Error?e.message:"Unable to load deliveries.");}};
  void load();
  const timer=window.setInterval(()=>{if(alive)void load();},15000);
  return()=>{alive=false;window.clearInterval(timer);};
 },[refresh]);
 const stopSharing=useCallback(()=>{
  if(watchRef.current!==null&&typeof navigator!=="undefined"&&navigator.geolocation){
   navigator.geolocation.clearWatch(watchRef.current);
  }
  watchRef.current=null;setSharingID("");
 },[]);
 useEffect(()=>()=>{if(watchRef.current!==null&&navigator.geolocation)navigator.geolocation.clearWatch(watchRef.current);},[]);
 useEffect(()=>{
  const current=jobs.find(j=>j.id===sharingID);
  if(sharingID&&(!current||!["assigned","picked_up","en_route"].includes(current.status))){
   const handle=window.setTimeout(()=>stopSharing(),0);
   return ()=>window.clearTimeout(handle);
  }
 },[jobs,sharingID,stopSharing]);
 async function changeStatus(job:Job,status:string){
  if(status==="failed"&&failureReason.trim().length<4){setError("Enter a failure reason (at least four characters).");return;}
  setBusy(true);setError("");setNotice("");
  try{
   const res=await fetch(apiURL+"/api/v1/rider/deliveries/"+job.id+"/status",{
    method:"PUT",credentials:"include",headers:{"Content-Type":"application/json"},
    body:JSON.stringify({status,reason:status==="failed"?failureReason:""})
   });
   if(!res.ok){const b=(await res.json().catch(()=>({}))) as {error?:string};throw new Error(b.error||"Unable to update status.");}
   if(status==="delivered"||status==="failed")stopSharing();
   setFailureReason("");await refresh();setNotice("Delivery status updated.");
  }catch(e){setError(e instanceof Error?e.message:"Update failed.");}
  finally{setBusy(false);}
 }
 function startSharing(job:Job){
  setError("");setNotice("");
  if(!navigator.geolocation){setError("Browser geolocation is unavailable. Use HTTPS or localhost.");return;}
  stopSharing();
  lastSent.current=0;
  setSharingID(job.id);
  watchRef.current=navigator.geolocation.watchPosition(
   async position=>{
    if(Date.now()-lastSent.current<7000)return;
    lastSent.current=Date.now();
    try{
     const result=await fetch(apiURL+"/api/v1/rider/deliveries/"+job.id+"/location",{
      method:"PUT",credentials:"include",headers:{"Content-Type":"application/json"},
      body:JSON.stringify({latitude:position.coords.latitude,longitude:position.coords.longitude,
       accuracy_m:Math.min(1000,position.coords.accuracy)})
     });
     if(!result.ok){
      const b=(await result.json().catch(()=>({}))) as {error?:string};
      if(result.status===409||result.status===403||result.status===404){stopSharing();}
      throw new Error(b.error||"GPS update failed.");
     }
     setNotice("GPS location shared with this delivery.");
    }catch(e){setError(e instanceof Error?e.message:"GPS update failed.");}
   },
   e=>{setError("GPS permission or positioning error: "+e.message);stopSharing();},
   {enableHighAccuracy:true,maximumAge:1000,timeout:15000}
  );
 }
 return <section className="catalog-section">
  <p className="muted">Location sharing is optional and starts only when you select “Share live GPS”. It stops when you select “Stop sharing”, when an assignment changes, or when delivery completes.</p>
  {error&&<p className="notice" role="alert">{error}</p>}
  {notice&&<p className="notice" role="status">{notice}</p>}
  {jobs.length===0&&<div className="panel"><h2>No deliveries assigned</h2>
   <p className="muted">Active riders will see their jobs once a restaurant manager assigns them.</p></div>}
  {jobs.map(job=>{
   const active=["assigned","picked_up","en_route"].includes(job.status);
   return <article key={job.id} className="panel">
    <div className="delivery-heading"><div><h2>{job.restaurant_name}</h2>
     <p className="muted">Order {job.order_id.slice(0,8)} · <strong>{job.status.replaceAll("_"," ")}</strong></p>
    </div><span className="tag">{active?"Active delivery":"Completed job"}</span></div>
    <p>{[job.delivery_address.apartment,job.delivery_address.building,job.delivery_address.street,
     job.delivery_address.area,job.delivery_address.city].filter(Boolean).join(", ")}</p>
    <p className="muted">Recipient: {job.delivery_address.recipient_name} · {job.delivery_address.phone}</p>
    <div className="record-actions">
     {job.status==="assigned"&&<button className="action" disabled={busy}
      onClick={()=>void changeStatus(job,"picked_up")}>Mark picked up</button>}
     {job.status==="picked_up"&&<button className="action" disabled={busy}
      onClick={()=>void changeStatus(job,"en_route")}>Start delivery</button>}
     {job.status==="en_route"&&<button className="action" disabled={busy}
      onClick={()=>void changeStatus(job,"delivered")}>Mark delivered</button>}
     {active&&(sharingID===job.id?
      <button className="action secondary" onClick={stopSharing}>Stop GPS sharing</button>:
      <button className="action secondary" onClick={()=>startSharing(job)}>Share live GPS</button>)}
     <Link href={"/orders/"+job.order_id}>Order details</Link>
    </div>
    {active&&<div className="field-stack delivery-failure">
     <label className="form-field">Delivery failure reason (if applicable)
      <input maxLength={250} value={failureReason} onChange={e=>setFailureReason(e.target.value)}
       placeholder="Address inaccessible, customer not available…" />
     </label>
     <button className="action secondary" disabled={busy||failureReason.trim().length<4}
      onClick={()=>void changeStatus(job,"failed")}>Report delivery failed</button>
    </div>}
   </article>;
  })}
 </section>;
}
