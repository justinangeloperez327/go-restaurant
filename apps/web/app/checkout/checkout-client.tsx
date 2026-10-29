"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";

type Address = { id: string; label: string; street: string; city: string; is_default: boolean };
type Quote = { restaurant_id: string; currency: string; subtotal_minor: number; tax_minor: number; delivery_fee_minor: number; total_minor: number };
type Methods = { cod: boolean; stripe: boolean };
type CheckoutResponse = { order_id: string; payment_method: string; payment_status: string; redirect_url: string };

export function CheckoutClient({ apiURL }: { apiURL: string }) {
 const [addresses,setAddresses] = useState<Address[]>([]);
 const [address,setAddress] = useState("");
 const [methods,setMethods] = useState<Methods>({cod:false,stripe:false});
 const [method,setMethod] = useState<"cod"|"stripe">("cod");
 const [notes,setNotes] = useState("");
 const [quote,setQuote] = useState<Quote|null>(null);
 const [error,setError] = useState("");
 const [busy,setBusy] = useState(false);
 const [loading,setLoading] = useState(true);
 const currentKey=useRef<string|null>(null);
 const init=useCallback(async()=>{
  const [a,m]=await Promise.all([
   fetch(apiURL+"/api/v1/me/addresses",{credentials:"include",cache:"no-store"}),
   fetch(apiURL+"/api/v1/checkout/config",{credentials:"include",cache:"no-store"}),
  ]);
  if(!a.ok||!m.ok)throw new Error(a.status===401?"Please sign in before checkout.":"Unable to load checkout settings.");
  const listed=(await a.json()) as Address[];
  const config=(await m.json()) as Methods;
  setAddresses(listed);setMethods(config);
  setAddress(listed.find(x=>x.is_default)?.id||listed[0]?.id||"");
  setMethod(config.cod?"cod":"stripe");
 },[apiURL]);
 useEffect(()=>{
  let live=true;
  async function start(){
   try{
    // Requests are always credentialed; no sensitive data is stored in the browser.
    await init();
   }catch(e){if(live)setError(e instanceof Error?e.message:"Unable to load checkout.");}
   finally{if(live)setLoading(false);}
  }
  void start();
  return ()=>{live=false;};
 },[init]);
 useEffect(()=>{
  if(!address){return;}
  let live=true;
  async function loadQuote(){
   try{
    const r=await fetch(apiURL+"/api/v1/checkout/quote?address_id="+encodeURIComponent(address),{credentials:"include",cache:"no-store"});
    if(!r.ok){
     const e=(await r.json().catch(()=>({}))) as {error?:string};
     throw new Error(e.error||"Unable to price this order.");
    }
    const q=(await r.json()) as Quote;
    if(live){setQuote(q);setError("");}
   }catch(e){if(live){setQuote(null);setError(e instanceof Error?e.message:"Unable to load quote.");}}
  }
  void loadQuote();
  return ()=>{live=false;};
 },[address,apiURL]);
 function changeAddress(id:string){currentKey.current=null;setAddress(id);}
 function changeMethod(next:"cod"|"stripe"){currentKey.current=null;setMethod(next);}
 function changeNotes(next:string){currentKey.current=null;setNotes(next);}
 const money=(minor:number)=>new Intl.NumberFormat("en-AE",{style:"currency",currency:"AED"}).format(minor/100);
 async function submit(){
  if(!address||!quote||busy)return;
  setBusy(true);setError("");
  const key=currentKey.current??("order_"+crypto.randomUUID());
  currentKey.current=key;
  try{
   const res=await fetch(apiURL+"/api/v1/checkout",{
    method:"POST",credentials:"include",headers:{"Content-Type":"application/json","Idempotency-Key":key},
    body:JSON.stringify({address_id:address,payment_method:method,notes}),
   });
   if(!res.ok){
    const body=(await res.json().catch(()=>({}))) as {error?:string};
    if(res.status===409)currentKey.current=null;
    throw new Error(body.error||"Checkout could not be completed.");
   }
   const data=(await res.json()) as CheckoutResponse;
   if(method==="stripe"&&data.redirect_url){
    window.location.assign(data.redirect_url);
   }else{
    window.location.assign("/orders/"+data.order_id);
   }
  }catch(e){setError(e instanceof Error?e.message:"Checkout failed.");setBusy(false);}
 }
 return <section className="catalog-section account-sections">
  {loading&&<p className="muted">Preparing checkout…</p>}
  {error&&<p className="notice" role="alert">{error}</p>}
  {!loading&&<>
   <div className="panel">
    <h2>Delivery address</h2>
    {addresses.length===0?<p><Link href="/account" className="action secondary">Add a delivery address</Link></p>:<>
     <label className="form-field">Deliver to
      <select value={address} onChange={e=>changeAddress(e.target.value)}>
       {addresses.map(a=><option key={a.id} value={a.id}>{a.label} · {a.street}, {a.city}</option>)}
      </select>
     </label>
     <p><Link href="/account">Manage addresses</Link></p>
    </>}
   </div>
   <div className="panel">
    <h2>Payment method</h2>
    {methods.cod&&<label className="check-field"><input type="radio" checked={method==="cod"} onChange={()=>changeMethod("cod")} />Cash on delivery (unpaid until collected)</label>}
    {methods.stripe&&<label className="check-field"><input type="radio" checked={method==="stripe"} onChange={()=>changeMethod("stripe")} />Card or wallet via Stripe Checkout</label>}
    {!methods.cod&&!methods.stripe&&<p className="notice">No payment methods are configured.</p>}
    <label className="form-field">Delivery instructions
     <textarea maxLength={500} value={notes} onChange={e=>changeNotes(e.target.value)} placeholder="Optional notes for the restaurant or rider"/>
    </label>
   </div>
   {quote&&<div className="panel">
    <h2>Order summary</h2>
    <div className="cart-total"><span>Meals</span><strong>{money(quote.subtotal_minor)}</strong></div>
    <div className="cart-total"><span>Delivery</span><strong>{money(quote.delivery_fee_minor)}</strong></div>
    <div className="cart-total"><span>Configured tax</span><strong>{money(quote.tax_minor)}</strong></div>
    <div className="cart-total"><strong>Total</strong><strong>{money(quote.total_minor)}</strong></div>
    <p className="muted">The server recalculates prices when you submit. Your order is confirmed only after the required payment step.</p>
    <button type="button" className="action" onClick={()=>void submit()} disabled={busy||!address||(!methods.cod&&!methods.stripe)}>
     {busy?"Processing…":method==="cod"?"Place cash-on-delivery order":"Continue to secure payment"}
    </button>
   </div>}
  </>}
 </section>;
}
