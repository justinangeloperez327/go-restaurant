export const dynamic = "force-dynamic";

import Link from "next/link";import {OrderDetail} from "./order-detail";
export default async function OrderPage({params}:{params:Promise<{id:string}>}){
 const {id}=await params;
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
 <header className="topbar"><Link href="/" className="brand"><span className="brand-mark">G</span> Go Restaurant</Link><Link href="/orders" className="action secondary">All orders</Link></header>
 <section className="catalog-header"><span className="eyebrow">ORDER DETAILS</span><h1>Order status</h1></section>
 <OrderDetail apiURL={apiURL} id={id}/><footer>Go Restaurant · Group 7</footer>
 </main>;
}
