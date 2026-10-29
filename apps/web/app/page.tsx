import Link from "next/link";
import { AuthPanel } from "./auth-panel";

type Restaurant = { id: string; name: string; slug: string; description: string; image_url: string; currency: string };
async function apiState(): Promise<"online" | "offline"> {
  try {
    const res = await fetch(new URL("/healthz", process.env.API_INTERNAL_URL ?? "http://localhost:8080"), {
      cache: "no-store", signal: AbortSignal.timeout(3000),
    });
    return res.ok ? "online" : "offline";
  } catch { return "offline"; }
}
async function restaurants(): Promise<Restaurant[]> {
  try {
    const res = await fetch(new URL("/api/v1/restaurants", process.env.API_INTERNAL_URL ?? "http://localhost:8080"), {
      cache: "no-store", signal: AbortSignal.timeout(3000),
    });
    return res.ok ? (await res.json()) as Restaurant[] : [];
  } catch { return []; }
}
export default async function Home() {
  const [api, locations] = await Promise.all([apiState(), restaurants()]);
  return <main className="container">
    <header className="topbar">
      <div className="brand"><span className="brand-mark">G</span> Go Restaurant</div>
      <div className="record-actions">
        <Link href="/account" className="action secondary">My account</Link>
        <Link href="/cart" className="action secondary">My cart</Link>
        <Link href="/admin/catalog" className="action secondary">Restaurant admin</Link>
      </div>
    </header>
    <section className="hero">
      <span className="eyebrow">MEALS & SUBSCRIPTIONS</span>
      <h1>Fresh meals, simple ordering.</h1>
      <p>Explore published restaurant menus. Create your cart with customizable meals. Checkout and subscriptions are coming next.</p>
      <div className="status"><span className={api === "online" ? "dot online" : "dot offline"} aria-hidden="true" />
        <span>Go API: <strong>{api}</strong></span></div>
      <AuthPanel apiURL={process.env.API_PUBLIC_URL ?? "http://localhost:8080"} />
    </section>
    <section className="catalog-section">
      <h2>Restaurants</h2>
      {locations.length === 0 && <p className="muted">No restaurants are currently published.</p>}
      <div className="restaurant-grid">{locations.map((r) => <Link href={"/restaurants/" + r.id} key={r.id} className="restaurant-card">
        {r.image_url && <div className="restaurant-cover" style={{ backgroundImage: "url(" + JSON.stringify(r.image_url) + ")" }} />}
        <div className="restaurant-content"><strong>{r.name}</strong><p>{r.description}</p><span className="muted">View menu · {r.currency}</span></div>
      </Link>)}</div>
    </section>
    <footer>Go Restaurant · Group 4 · Profiles, addresses and cart</footer>
  </main>;
}
