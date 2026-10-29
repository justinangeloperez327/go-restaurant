"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";

type Option = { id: string; name: string; price_delta_minor: number };
type CartItem = { id: string; meal_id: string; name: string; quantity: number; unit_price_minor: number; line_total_minor: number; options: Option[]; is_valid: boolean; issue?: string };
type CartData = { restaurant: { id: string; name: string; currency: string } | null; items: CartItem[]; subtotal_minor: number; can_checkout: boolean };

export function CartClient({ apiURL }: { apiURL: string }) {
  const [cart, setCart] = useState<CartData | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const refresh = useCallback(async () => {
    const response = await fetch(apiURL + "/api/v1/cart", { credentials: "include", cache: "no-store" });
    if (response.status === 401) throw new Error("Please sign in to view your cart.");
    if (!response.ok) throw new Error("Could not load cart.");
    setCart((await response.json()) as CartData);
  }, [apiURL]);

  useEffect(() => {
    let active = true;
    fetch(apiURL + "/api/v1/cart", { credentials: "include", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error(response.status === 401 ? "Please sign in to view your cart." : "Unable to load cart.");
        const content = (await response.json()) as CartData;
        if (active) setCart(content);
      })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "Unable to load cart."); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [apiURL]);

  async function mutate(path: string, method: string, data?: unknown) {
    setBusy(true); setError("");
    try {
      const res = await fetch(apiURL + "/api/v1" + path, {
        method, credentials: "include",
        ...(data === undefined ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(data) }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: string };
        throw new Error(body.error || "Could not update cart.");
      }
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not update cart.");
    } finally { setBusy(false); }
  }

  function money(minor: number) {
    const currency = cart?.restaurant?.currency || "AED";
    return new Intl.NumberFormat("en-AE", { style: "currency", currency }).format(minor / 100);
  }

  return <section className="catalog-section">
    {loading && <p className="muted">Loading your cart…</p>}
    {error && <p role="status" className="notice">{error}</p>}
    {cart && <>
      {cart.restaurant ? <div className="panel">
        <div className="cart-title"><h2>{cart.restaurant.name}</h2>
          <button className="action secondary" disabled={busy} onClick={() => void mutate("/cart", "DELETE")}>Clear cart</button>
        </div>
        {cart.items.map((item) => <div className="cart-line" key={item.id}>
          <div className="cart-line-main">
            <strong>{item.name}</strong>
            {item.options.map((opt) => <small className="muted" key={opt.id}>{opt.name} {opt.price_delta_minor ? "+" + money(opt.price_delta_minor) : ""}</small>)}
            {!item.is_valid && <p role="status" className="notice">{item.issue || "Please remove and re-add this item."}</p>}
          </div>
          <label className="form-field">Quantity
            <input type="number" min={1} max={50} value={item.quantity} disabled={busy}
              onChange={(e) => {
                const next = Number(e.target.value);
                if (next >= 1 && next <= 50 && Number.isInteger(next)) void mutate("/cart/items/" + item.id, "PUT", { quantity: next });
              }} />
          </label>
          <strong>{item.is_valid ? money(item.line_total_minor) : "Review item"}</strong>
          <button className="action secondary" disabled={busy} onClick={() => void mutate("/cart/items/" + item.id, "DELETE")}>Remove</button>
        </div>)}
        <div className="cart-total"><span>Estimated subtotal</span><strong>{money(cart.subtotal_minor)}</strong></div>
        <p className="muted">Prices are recalculated from the live menu. Tax, delivery fees, and payment will be finalized during Group 5 checkout.</p>
        <p className="muted">{cart.can_checkout ? "Your cart is ready for the upcoming checkout workflow." : "Some items need attention, or your cart is empty."}</p>
        <Link className="action secondary" href={"/restaurants/" + cart.restaurant.id}>Continue shopping</Link>
      </div> : <div className="panel"><h2>Your cart is empty</h2><p>Browse available meals and build your order.</p><Link className="action" href="/">Browse restaurants</Link></div>}
    </>}
  </section>;
}
