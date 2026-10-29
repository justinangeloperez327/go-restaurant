import Link from "next/link";
import { OperationsClient } from "./operations-client";

export default function OperationsPage(){
 const apiURL=process.env.NEXT_PUBLIC_API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar">
   <Link href="/" className="brand"><span className="brand-mark">G</span> Go Restaurant</Link>
   <Link href="/admin/deliveries" className="action secondary">Dispatch</Link>
  </header>
  <section className="catalog-header">
   <span className="eyebrow">GROUP 8 · RESTAURANT OPERATIONS</span>
   <h1>Performance & operations</h1>
   <p>Track orders, deliveries, subscriptions, and alerts for restaurants you manage.</p>
  </section>
  <OperationsClient apiURL={apiURL}/>
  <footer>Go Restaurant · Operations dashboard</footer>
 </main>;
}
