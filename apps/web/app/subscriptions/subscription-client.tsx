"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";

type Address = { id: string; label: string; street: string; city: string };
type Cart = { items: unknown[]; subtotal_minor: number; restaurant: { name: string } | null; can_checkout: boolean };
type Subscription = {
 id: string; restaurant_name: string; status: string; address_id: string;
 weekdays: number[]; delivery_time: string; next_delivery_date: string; current_period_end: string;
 auto_renew: boolean; max_total_minor: number; notes: string;
};
type Occurrence = { date: string; status: string; order_id: string | null; reason: string };
const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const money = (v:number) => new Intl.NumberFormat("en-AE",{style:"currency",currency:"AED"}).format(v/100);
const today = () => new Date().toLocaleDateString("sv-SE",{timeZone:"Asia/Dubai"});

export function SubscriptionClient({apiURL}:{apiURL:string}) {
 const [list,setList] = useState<Subscription[]>([]);
 const [addresses,setAddresses] = useState<Address[]>([]);
 const [cart,setCart] = useState<Cart|null>(null);
 const [addressID,setAddressID] = useState("");
 const [startDate,setStartDate] = useState(today);
 const [deliveryTime,setDeliveryTime] = useState("12:00");
 const [weekdays,setWeekdays] = useState<number[]>([1,2,3,4,5]);
 const [maxTotalAED,setMaxTotalAED] = useState("60.00");
 const [autoRenew,setAutoRenew] = useState(true);
 const [notes,setNotes] = useState("");
 const [error,setError] = useState("");
 const [message,setMessage] = useState("");
 const [busy,setBusy] = useState(false);
 const [loading,setLoading] = useState(true);
 const [selectedID,setSelectedID] = useState("");
 const [history,setHistory] = useState<Occurrence[]>([]);
 const [skipDate,setSkipDate] = useState(today);
 const [editCap,setEditCap] = useState("");
 const [editRenew,setEditRenew] = useState(true);

 const api = useCallback(async(path:string,method="GET",body?:unknown)=>{
  const r=await fetch(apiURL+"/api/v1"+path,{method,credentials:"include",cache:"no-store",
   ...(body===undefined?{}:{headers:{"Content-Type":"application/json"},body:JSON.stringify(body)})});
  const result=await r.json().catch(()=>({})) as Record<string,unknown>;
  if(!r.ok)throw new Error(r.status===401?"Sign in before managing subscriptions.":String(result.error||"Request failed."));
  return result;
 },[apiURL]);
 const refresh=useCallback(async()=>{
  const data=await api("/subscriptions") as {subscriptions:Subscription[]};
  setList(data.subscriptions||[]);
 },[api]);
 useEffect(()=>{
  let alive=true;
  Promise.all([api("/subscriptions"),api("/me/addresses"),api("/cart")])
   .then(([s,a,c])=>{
    if(!alive)return;
    const rows=(s as {subscriptions:Subscription[]}).subscriptions||[];
    const as=a as unknown as Address[];
    const current=c as unknown as Cart;
    setList(rows);setAddresses(as);setCart(current);
    if(as.length)setAddressID(as[0].id);
    if(current.subtotal_minor>0)setMaxTotalAED((Math.ceil((current.subtotal_minor+500)/100)).toFixed(2));
   })
   .catch((e:unknown)=>{if(alive)setError(e instanceof Error?e.message:"Could not load subscriptions.");})
   .finally(()=>{if(alive)setLoading(false);});
  return ()=>{alive=false;};
 },[api]);
 async function apply(op:()=>Promise<void>){
  setBusy(true);setError("");setMessage("");
  try{await op();await refresh();setMessage("Subscription updated.");}
  catch(e){setError(e instanceof Error?e.message:"Request failed.");}
  finally{setBusy(false);}
 }
 async function create(e:FormEvent<HTMLFormElement>){
  e.preventDefault();
  await apply(async()=>{
   const cap=Number(maxTotalAED);
   if(!Number.isFinite(cap)||cap<0.01)throw new Error("Enter a positive maximum AED total.");
   const result=await api("/subscriptions","POST",{
    address_id:addressID,start_date:startDate,delivery_time:deliveryTime,
    weekdays,max_total_minor:Math.round(cap*100),auto_renew:autoRenew,notes,
   });
   setSelectedID(String(result.id));
   setMessage("Subscription created.");
  });
 }
 function selectSubscription(item:Subscription){
  setSelectedID(item.id);setEditCap((item.max_total_minor/100).toFixed(2));setEditRenew(item.auto_renew);
  setHistory([]);
  void api("/subscriptions/"+item.id+"/occurrences")
   .then(x=>setHistory((x as {occurrences:Occurrence[]}).occurrences))
   .catch((e:unknown)=>setError(e instanceof Error?e.message:"Could not load history."));
 }
 async function changeStatus(id:string,action:"pause"|"resume"|"cancel"){
  if(action==="cancel"&&!window.confirm("Cancel all future recurring deliveries? Orders already created remain in your order history."))return;
  await apply(async()=>{await api("/subscriptions/"+id+"/"+action,"POST");});
  if(selectedID===id){
   const data=await api("/subscriptions/"+id+"/occurrences") as {occurrences:Occurrence[]};
   setHistory(data.occurrences);
  }
 }
 async function settings(id:string){
  await apply(async()=>{await api("/subscriptions/"+id,"PUT",{
   auto_renew:editRenew,max_total_minor:Math.round(Number(editCap)*100),
  });});
 }
 async function skip(id:string){
  await apply(async()=>{await api("/subscriptions/"+id+"/skips","POST",{date:skipDate});});
 }

 return <section className="catalog-section subscription-layout">
  {loading&&<p className="muted">Loading subscriptions…</p>}
  {error&&<p role="alert" className="notice">{error}</p>}
  {message&&<p role="status" className="notice">{message}</p>}
  {!loading&&<div className="admin-grid">
   <section className="panel">
    <h2>New weekly meal plan</h2>
    <p className="muted">Your current cart becomes the recurring meal template. Meals are priced again before every delivery. Cash is collected on delivery; no card is charged automatically.</p>
    <p>{cart?.restaurant?.name||"No restaurant selected"} · {cart?money(cart.subtotal_minor):"Empty cart"}</p>
    <form className="field-stack" onSubmit={e=>void create(e)}>
     <label className="form-field">Delivery address
      <select value={addressID} required onChange={e=>setAddressID(e.target.value)}>
       <option value="">Choose address</option>
       {addresses.map(a=><option key={a.id} value={a.id}>{a.label} · {a.street}, {a.city}</option>)}
      </select>
     </label>
     <div className="record-actions"><Link href="/account">Manage addresses</Link><Link href="/cart">Edit meal selection</Link></div>
     <label className="form-field">Start date (UAE local)
      <input type="date" min={today()} value={startDate} required onChange={e=>setStartDate(e.target.value)} />
     </label>
     <label className="form-field">Delivery time (Asia/Dubai)
      <input type="time" value={deliveryTime} required onChange={e=>setDeliveryTime(e.target.value)} />
     </label>
     <fieldset className="modifier-options"><legend>Delivery weekdays</legend>
      <div className="weekday-selector">{days.map((d,i)=><label key={d} className="check-field">
       <input type="checkbox" checked={weekdays.includes(i)}
        onChange={e=>setWeekdays(old=>e.target.checked?[...old,i].sort():old.filter(v=>v!==i))}/>
       {d}
      </label>)}</div>
     </fieldset>
     <label className="form-field">Maximum total per delivery (AED)
      <input type="number" step="0.01" min="0.01" max="10000000" required
       value={maxTotalAED} onChange={e=>setMaxTotalAED(e.target.value)}/>
     </label>
     <p className="muted">The cap includes current delivery fees and configured tax. Future deliveries are skipped if the total exceeds this limit.</p>
     <label className="check-field"><input type="checkbox" checked={autoRenew}
      onChange={e=>setAutoRenew(e.target.checked)} />Auto-renew each 7-day period</label>
     <label className="form-field">Instructions (optional)
      <textarea maxLength={500} value={notes} onChange={e=>setNotes(e.target.value)}/>
     </label>
     <button className="action" disabled={busy||!cart?.can_checkout||!addressID||weekdays.length===0}>Create COD subscription</button>
    </form>
   </section>
   <div>
    <section className="panel"><h2>My subscriptions</h2>
     {list.length===0&&<p className="muted">No subscriptions yet.</p>}
     {list.map(s=><div className="subscription-item" key={s.id}>
      <strong>{s.restaurant_name}</strong> <span className="tag">{s.status}</span>
      <p className="muted">{s.weekdays.map(d=>days[d]).join(", ")} at {s.delivery_time} · next {s.next_delivery_date}</p>
      <p className="muted">Cap {money(s.max_total_minor)} · {s.auto_renew?"Auto-renews":"Ends after current period"}</p>
      <div className="record-actions">
       <button className="action secondary" type="button" onClick={()=>selectSubscription(s)}>Manage</button>
       {s.status==="active"&&<button className="action secondary" disabled={busy} onClick={()=>void changeStatus(s.id,"pause")}>Pause</button>}
       {s.status==="paused"&&<button className="action secondary" disabled={busy} onClick={()=>void changeStatus(s.id,"resume")}>Resume</button>}
       {(s.status==="active"||s.status==="paused")&&<button className="action secondary" disabled={busy} onClick={()=>void changeStatus(s.id,"cancel")}>Cancel</button>}
      </div>
     </div>)}
    </section>
    {selectedID&&(()=>{
     const sub=list.find(s=>s.id===selectedID);
     return sub&&<section className="panel">
      <h2>Manage {sub.restaurant_name}</h2>
      <p className="muted">Next scheduled date: {sub.next_delivery_date}. Weekly period ends {sub.current_period_end} (exclusive).</p>
      {(sub.status==="active"||sub.status==="paused")&&<div className="field-stack">
       <label className="form-field">Maximum total per delivery (AED)
        <input type="number" min="0.01" step="0.01" value={editCap} onChange={e=>setEditCap(e.target.value)}/>
       </label>
       <label className="check-field"><input type="checkbox" checked={editRenew} onChange={e=>setEditRenew(e.target.checked)}/>Auto-renew after period end</label>
       <button className="action secondary" type="button" disabled={busy} onClick={()=>void settings(sub.id)}>Save settings</button>
       <label className="form-field">Skip delivery date (at least 24 hours before delivery)
        <input type="date" value={skipDate} min={today()} onChange={e=>setSkipDate(e.target.value)}/>
       </label>
       <button className="action secondary" type="button" disabled={busy} onClick={()=>void skip(sub.id)}>Skip selected date</button>
      </div>}
      <h3>Delivery history</h3>
      {history.length===0&&<p className="muted">No deliveries processed yet.</p>}
      {history.map(o=><p key={o.date} className="subscription-history">
       {o.date} · <strong>{o.status}</strong>{o.reason?" · "+o.reason:""}
       {o.order_id&&<> · <Link href={"/orders/"+o.order_id}>View order</Link></>}
      </p>)}
     </section>;
    })()}
   </div>
  </div>}
 </section>;
}
