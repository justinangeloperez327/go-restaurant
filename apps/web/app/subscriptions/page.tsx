import Link from "next/link";
import { SubscriptionClient } from "./subscription-client";

export default function SubscriptionsPage(){
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
   <div className="record-actions"><Link href="/cart" className="action secondary">My cart</Link>
   <Link href="/orders" className="action secondary">My orders</Link></div></header>
  <section className="catalog-header"><span className="eyebrow">GROUP 6 · MEAL SUBSCRIPTIONS</span><h1>Meals on your schedule.</h1>
   <p>Choose weekly delivery days, set a maximum per-delivery price and manage renewals. All recurring deliveries are cash on delivery.</p>
  </section>
  <SubscriptionClient apiURL={apiURL}/><footer>Go Restaurant · Group 6</footer>
 </main>;
}
