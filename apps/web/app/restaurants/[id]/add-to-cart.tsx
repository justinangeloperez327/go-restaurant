"use client";

import { useState } from "react";
import Link from "next/link";

type Group = { id: string; name: string; min_select: number; max_select: number };
type Option = { id: string; group_id: string; name: string; price_delta_minor: number; is_available: boolean };

export function AddToCart({ mealID, groups, options, currency, apiURL, available }: {
  mealID: string; groups: Group[]; options: Option[];
  currency: string; apiURL: string; available: boolean;
}) {
  const [quantity, setQuantity] = useState(1);
  const [selected, setSelected] = useState<string[]>([]);
  const [message, setMessage] = useState("");
  const [added, setAdded] = useState(false);
  const [busy, setBusy] = useState(false);
  const amount = (minor: number) => new Intl.NumberFormat("en-AE", { style: "currency", currency }).format(minor / 100);

  function toggle(id: string, group: Group) {
    setMessage(""); setAdded(false);
    setSelected((old) => {
      if (old.includes(id)) return old.filter((x) => x !== id);
      const inGroup = old.filter((x) => options.some((opt) => opt.id === x && opt.group_id === group.id));
      if (inGroup.length >= group.max_select) return old;
      return [...old, id];
    });
  }

  async function add() {
    setMessage(""); setAdded(false);
    for (const g of groups) {
      const count = selected.filter((id) => options.some((x) => x.id === id && x.group_id === g.id)).length;
      if (count < g.min_select || count > g.max_select) {
        setMessage("Choose " + g.min_select + " to " + g.max_select + " options in " + g.name + "."); return;
      }
    }
    setBusy(true);
    try {
      const res = await fetch(apiURL + "/api/v1/cart/items", {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ meal_id: mealID, quantity, option_ids: selected }),
      });
      if (!res.ok) {
        const payload = (await res.json().catch(() => ({}))) as { error?: string };
        if (res.status === 401) throw new Error("Sign in on the home page before adding meals.");
        throw new Error(payload.error || "Unable to add meal");
      }
      setAdded(true); setMessage("Added to your cart.");
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Unable to add meal.");
    } finally { setBusy(false); }
  }

  return <div className="cart-actions">
    {groups.map((group) => <fieldset key={group.id} className="modifier-options">
      <legend>{group.name} <small>({group.min_select}–{group.max_select})</small></legend>
      {options.filter((opt) => opt.group_id === group.id && opt.is_available).map((opt) =>
        <label key={opt.id} className="check-field">
          <input type="checkbox" checked={selected.includes(opt.id)} onChange={() => toggle(opt.id, group)} />
          <span>{opt.name}{opt.price_delta_minor > 0 ? " · +" + amount(opt.price_delta_minor) : ""}</span>
        </label>)}
    </fieldset>)}
    <div className="cart-add-row">
      <label className="form-field">Qty
        <input aria-label="Meal quantity" type="number" min={1} max={50} value={quantity}
          onChange={(e) => setQuantity(Math.max(1, Math.min(50, Number(e.target.value) || 1)))} />
      </label>
      <button className="action" type="button" onClick={() => void add()} disabled={!available || busy}>
        {busy ? "Adding…" : available ? "Add to cart" : "Sold out"}
      </button>
      {added && <Link className="action secondary" href="/cart">View cart</Link>}
    </div>
    {message && <p role="status" className="notice">{message}</p>}
  </div>;
}
