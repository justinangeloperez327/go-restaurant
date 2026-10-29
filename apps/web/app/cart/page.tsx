import Link from "next/link";
import { CartClient } from "./cart-client";

export default function CartPage() {
  const apiURL = process.env.API_PUBLIC_URL ?? "http://localhost:8080";
  return <main className="container">
    <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
      <Link className="action secondary" href="/account">My account</Link></header>
    <section className="catalog-header"><span className="eyebrow">GROUP 4 · SHOPPING CART</span>
      <h1>Your cart</h1><p>Items and prices come directly from the current menu.</p></section>
    <CartClient apiURL={apiURL} /><footer>Go Restaurant · Group 4</footer>
  </main>;
}
