export const dynamic = "force-dynamic";

import Link from "next/link";
import { AddToCart } from "./add-to-cart";
import { notFound } from "next/navigation";

type Restaurant = { id: string; name: string; description: string; currency: string; image_url: string };
type Category = { id: string; name: string };
type Meal = { id: string; category_id: string; name: string; description: string; price_minor: number; image_url: string; is_available: boolean };
type Group = { id: string; meal_id: string; name: string; min_select: number; max_select: number };
type Option = { id: string; group_id: string; name: string; price_delta_minor: number; is_available: boolean };
type Catalog = { restaurant: Restaurant; categories: Category[]; meals: Meal[]; modifier_groups: Group[]; modifier_options: Option[] };

async function getMenu(id: string): Promise<Catalog | null> {
  try {
    const res = await fetch(new URL("/api/v1/restaurants/" + encodeURIComponent(id) + "/menu",
      process.env.API_INTERNAL_URL ?? "http://localhost:8080"),
      { cache: "no-store", signal: AbortSignal.timeout(3000) });
    return res.ok ? (await res.json()) as Catalog : null;
  } catch { return null; }
}
function amount(value: number, currency: string) {
  return new Intl.NumberFormat("en-AE", { style: "currency", currency }).format(value / 100);
}
export default async function MenuPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const menu = await getMenu(id);
  if (!menu) notFound();
  const r = menu.restaurant;
  return <main className="container">
    <header className="topbar"><Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link><div className="record-actions"><Link href="/cart" className="action secondary">My cart</Link>
      <Link href="/account" className="action secondary">My account</Link>
      <Link href="/" className="action secondary">All restaurants</Link></div></header>
    <section className="catalog-header"><span className="eyebrow">RESTAURANT MENU</span><h1>{r.name}</h1><p>{r.description}</p></section>
    {menu.categories.map((category) => {
      const meals = menu.meals.filter((m) => m.category_id === category.id);
      if (!meals.length) return null;
      return <section key={category.id} className="catalog-section"><h2>{category.name}</h2>
        <div className="restaurant-grid">{meals.map((meal) => <article className="meal-card" key={meal.id}>
          {meal.image_url && <div className="restaurant-cover" style={{ backgroundImage: "url(" + JSON.stringify(meal.image_url) + ")" }} />}
          <div className="restaurant-content"><div className="meal-heading"><strong>{meal.name}</strong><strong>{amount(meal.price_minor, r.currency)}</strong></div>
          <p>{meal.description}</p>
          {!meal.is_available && <span className="notice">Currently unavailable</span>}
          <AddToCart mealID={meal.id} available={meal.is_available} currency={r.currency}
            apiURL={process.env.API_PUBLIC_URL ?? "http://localhost:8080"}
            groups={menu.modifier_groups.filter((g) => g.meal_id === meal.id)}
            options={menu.modifier_options} />
          </div>
        </article>)}</div>
      </section>;
    })}
    <footer>Your cart is persistent. Checkout becomes available in Group 5.</footer>
  </main>;
}
