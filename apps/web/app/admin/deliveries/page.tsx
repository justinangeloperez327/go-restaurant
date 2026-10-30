export const dynamic = "force-dynamic";

import Link from "next/link";
import { DispatchClient } from "./dispatch-client";
export default function DispatchPage(){
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar"><Link href="/" className="brand"><span className="brand-mark">G</span> Go Restaurant</Link>
   <div className="record-actions"><Link href="/admin/catalog" className="action secondary">Catalog</Link>
    <Link href="/rider/deliveries" className="action secondary">Rider portal</Link></div></header>
  <section className="catalog-header"><span className="eyebrow">GROUP 7 · DISPATCH</span>
   <h1>Delivery operations</h1><p>Assign payment-eligible orders to active riders and monitor each delivery.</p></section>
  <DispatchClient apiURL={apiURL}/><footer>Go Restaurant · Group 7</footer>
 </main>;
}
