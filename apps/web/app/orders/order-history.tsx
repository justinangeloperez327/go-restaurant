"use client";

import Link from "next/link";
import { useEffect,useState } from "react";
type Order={id:string;restaurant_name:string;order_status:string;payment_status:string;total_minor:number;currency:string;created_at:string};
export function OrderHistory({apiURL}:{apiURL:string}){
 const [items,setItems]=useState<Order[]>([]);
 const [error,setError]=useState("");
 const [loading,setLoading]=useState(true);
 useEffect(()=>{
  let live=true;
  fetch(apiURL+"/api/v1/orders",{credentials:"include",cache:"no-store"})
   .then(async r=>{if(!r.ok)throw new Error(r.status===401?"Sign in to view your orders.":"Could not load orders.");return (await r.json()) as {orders:Order[]};})
   .then(data=>{if(live)setItems(data.orders);})
   .catch((e:unknown)=>{if(live)setError(e instanceof Error?e.message:"Unable to load orders.");})
   .finally(()=>{if(live)setLoading(false);});
  return ()=>{live=false};
 },[apiURL]);
 return <section className="catalog-section">
  {loading&&<p className="muted">Loading your orders…</p>}
  {error&&<p className="notice" role="alert">{error}</p>}
  {!loading&&!error&&items.length===0&&<p>No orders yet. <Link href="/">Browse meals</Link></p>}
  {items.map(item=><Link href={"/orders/"+item.id} className="panel order-link" key={item.id}>
   <div><strong>{item.restaurant_name}</strong><p className="muted">{new Date(item.created_at).toLocaleString()}</p></div>
   <div><strong>{new Intl.NumberFormat("en-AE",{style:"currency",currency:item.currency}).format(item.total_minor/100)}</strong>
    <p className="muted">{item.order_status.replaceAll("_"," ")} · {item.payment_status}</p></div>
  </Link>)}
 </section>;
}
