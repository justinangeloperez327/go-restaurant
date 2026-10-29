import Link from "next/link";
import { CheckoutClient } from "./checkout-client";
export default function CheckoutPage(){
 const apiURL=process.env.API_PUBLIC_URL??"http://localhost:8080";
 return <main className="container">
  <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
   <Link href="/cart" className="action secondary">Back to cart</Link></header>
  <section className="catalog-header"><span className="eyebrow">GROUP 5 · CHECKOUT</span><h1>Checkout</h1>
   <p>Confirm your delivery address, review the latest prices and choose how to pay.</p></section>
  <CheckoutClient apiURL={apiURL}/>
  <footer>Checkout and payments are processed by the Go API.</footer>
 </main>;
}
