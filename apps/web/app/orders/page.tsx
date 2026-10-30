export const dynamic = "force-dynamic";

import Link from "next/link";import {OrderHistory} from "./order-history";
export default function OrdersPage(){
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
   <Link href="/cart" className="action secondary">My cart</Link></header>
  <section className="catalog-header"><span className="eyebrow">ORDERS</span><h1>Your orders</h1></section>
  <OrderHistory apiURL={apiURL}/><footer>Go Restaurant · Group 5</footer>
 </main>;
}
