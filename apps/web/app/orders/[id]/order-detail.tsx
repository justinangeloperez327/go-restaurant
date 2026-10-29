"use client";

import Link from "next/link";
import { DeliveryTracking } from "./delivery-tracking";
import { useEffect,useState } from "react";
type Modifier={id:string;name:string;price_delta_minor:number};
type Line={meal_name:string;quantity:number;unit_price_minor:number;line_total_minor:number;selected_modifiers:Modifier[]};
type Order={id:string;restaurant_name:string;order_status:string;payment_status:string;payment_method:string;currency:string;subtotal_minor:number;delivery_fee_minor:number;tax_minor:number;total_minor:number;delivery_address:Record<string,string>;notes:string;items:Line[];created_at:string};
export function OrderDetail({apiURL,id}:{apiURL:string;id:string}){
 const [data,setData]=useState<Order|null>(null);
 const [error,setError]=useState("");
 const [loading,setLoading]=useState(true);
 useEffect(()=>{
  let live=true;
  fetch(apiURL+"/api/v1/orders/"+encodeURIComponent(id),{credentials:"include",cache:"no-store"})
   .then(async r=>{if(!r.ok)throw new Error(r.status===401?"Sign in to view this order.":"Order unavailable.");return (await r.json()) as Order;})
   .then(d=>{if(live)setData(d);})
   .catch((e:unknown)=>{if(live)setError(e instanceof Error?e.message:"Order unavailable.");})
   .finally(()=>{if(live)setLoading(false);});
  return ()=>{live=false;}
 },[apiURL,id]);
 function money(n:number){return new Intl.NumberFormat("en-AE",{style:"currency",currency:data?.currency||"AED"}).format(n/100);}
 return <section className="catalog-section">
  {loading&&<p className="muted">Loading order…</p>}
  {error&&<p role="alert" className="notice">{error}</p>}
  {data&&<><div className="panel">
   <h2>{data.restaurant_name}</h2>
   <p>Order ID: <code>{data.id}</code></p>
   <p><strong>Order:</strong> {data.order_status.replaceAll("_"," ")} · <strong>Payment:</strong> {data.payment_status}</p>
   {data.payment_status==="pending"&&<p className="notice">Payment pending. A browser return does not prove payment; status updates only after the verified provider webhook.</p>}
   {data.payment_method==="cod"&&<p className="muted">Cash on delivery — payment has not yet been collected.</p>}
   {data.items.map((line,i)=><div key={i} className="cart-line">
    <div className="cart-line-main"><strong>{line.meal_name}</strong>
     {line.selected_modifiers.map(m=><small className="muted" key={m.id}>{m.name}</small>)}</div>
    <span>× {line.quantity}</span><strong>{money(line.line_total_minor)}</strong>
   </div>)}
   <div className="cart-total"><span>Subtotal</span><strong>{money(data.subtotal_minor)}</strong></div>
   <div className="cart-total"><span>Delivery</span><strong>{money(data.delivery_fee_minor)}</strong></div>
   <div className="cart-total"><span>Tax</span><strong>{money(data.tax_minor)}</strong></div>
   <div className="cart-total"><strong>Total</strong><strong>{money(data.total_minor)}</strong></div>
   <h3>Delivery address</h3><p>{[data.delivery_address.street,data.delivery_address.area,data.delivery_address.city].filter(Boolean).join(", ")}</p>
   {data.notes&&<p>Instructions: {data.notes}</p>}
   <Link href="/orders" className="action secondary">All orders</Link>
  </div>
  <DeliveryTracking apiURL={apiURL} orderID={id}/>
  </>}
 </section>;
}
