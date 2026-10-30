export const dynamic = "force-dynamic";

import Link from "next/link";
import { RiderClient } from "./rider-client";
export default function RiderPage(){
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar"><Link href="/" className="brand"><span className="brand-mark">G</span> Go Restaurant</Link>
   <Link href="/orders" className="action secondary">My orders</Link></header>
  <section className="catalog-header"><span className="eyebrow">GROUP 7 · RIDER PORTAL</span>
   <h1>My deliveries</h1><p>Manage pickups, deliveries, and optional GPS sharing.</p></section>
  <RiderClient apiURL={apiURL}/><footer>Go Restaurant · Group 7</footer>
 </main>;
}
