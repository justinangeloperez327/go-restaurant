import Link from "next/link";
import { AccountClient } from "./account-client";

export default function AccountPage() {
  const apiURL = process.env.API_PUBLIC_URL ?? "http://localhost:8080";
  return <main className="container">
    <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
      <Link className="action secondary" href="/cart">My cart</Link></header>
    <section className="catalog-header"><span className="eyebrow">GROUP 4 · CUSTOMER ACCOUNT</span>
      <h1>Your account</h1><p>Manage your contact details and delivery addresses.</p></section>
    <AccountClient apiURL={apiURL} /><footer>Go Restaurant · Group 4</footer>
  </main>;
}
