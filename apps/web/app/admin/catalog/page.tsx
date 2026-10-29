"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";

type Item = { id: string; [key: string]: string | number | boolean };
type Restaurant = Item & { id: string; name: string; slug: string; description: string; currency: string; image_url: string; is_active: boolean };
type Catalog = {
  restaurant: Restaurant;
  categories: Item[];
  meals: Item[];
  modifier_groups: Item[];
  modifier_options: Item[];
};
type Field = { key: string; title: string; type?: "number" | "checkbox" | "select" | "url"; options?: { value: string; text: string }[]; defaultValue?: string | number | boolean };
const restaurantEmpty = { name: "", slug: "", description: "", currency: "AED", image_url: "", is_active: false };

export default function CatalogAdminPage() {
  const apiURL = process.env.NEXT_PUBLIC_API_PUBLIC_URL ?? "http://localhost:8080";
  const [restaurants, setRestaurants] = useState<Restaurant[]>([]);
  const [restaurantID, setRestaurantID] = useState("");
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [restaurant, setRestaurant] = useState(restaurantEmpty);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectedMeal, setSelectedMeal] = useState("");
  const [selectedGroup, setSelectedGroup] = useState("");
  const [managerID, setManagerID] = useState("");

  const request = useCallback(async (path: string, method = "GET", body?: unknown) => {
    const res = await fetch(apiURL + "/api/v1" + path, {
      method, credentials: "include", cache: "no-store",
      ...(body === undefined ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
    });
    if (!res.ok) {
      const payload = (await res.json().catch(() => ({}))) as { error?: string };
      throw new Error(payload.error || "Request failed (" + res.status + ")");
    }
    return res.status === 204 ? null : (await res.json()) as unknown;
  }, [apiURL]);

  const refreshRestaurants = useCallback(async () => {
    const results = await request("/admin/restaurants/") as Restaurant[];
    setRestaurants(results);
    setRestaurantID((id) => (id && results.some((r) => r.id === id) ? id : (results[0]?.id ?? "")));
  }, [request]);

  useEffect(() => {
    async function load() {
      try { await refreshRestaurants(); }
      catch (e) { setMessage(e instanceof Error ? e.message : "Sign in as a restaurant manager or administrator."); }
    }
    void load();
  }, [refreshRestaurants]);

  const refreshCatalog = useCallback(async (id: string) => {
    if (!id) { setCatalog(null); return; }
    const result = await request("/admin/restaurants/" + id + "/catalog") as Catalog;
    setCatalog(result);
  }, [request]);

  useEffect(() => {
    async function load() {
      try { await refreshCatalog(restaurantID); }
      catch (e) { setMessage(e instanceof Error ? e.message : "Unable to load catalog."); }
    }
    void load();
  }, [restaurantID, refreshCatalog]);

  async function mutation(operation: () => Promise<void>) {
    setBusy(true);
    setMessage("");
    try {
      await operation();
      await refreshRestaurants();
      if (restaurantID) await refreshCatalog(restaurantID);
      setMessage("Saved successfully.");
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Could not save.");
    } finally { setBusy(false); }
  }

  async function createRestaurant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await mutation(async () => {
      const result = await request("/admin/restaurants/", "POST", restaurant) as { id: string };
      setRestaurantID(result.id);
      setRestaurant(restaurantEmpty);
    });
  }
  async function updateRestaurant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!catalog) return;
    await mutation(async () => {
      await request("/admin/restaurants/" + restaurantID, "PUT", {
        name: catalog.restaurant.name, slug: catalog.restaurant.slug,
        description: catalog.restaurant.description, currency: catalog.restaurant.currency,
        image_url: catalog.restaurant.image_url, is_active: catalog.restaurant.is_active,
      });
    });
  }

  const root = "/admin/restaurants/" + restaurantID;
  const categoryChoices = (catalog?.categories ?? []).map((c) => ({ value: String(c.id), text: String(c.name) }));
  const mealChoices = (catalog?.meals ?? []).map((m) => ({ value: String(m.id), text: String(m.name) }));
  const groupChoices = (catalog?.modifier_groups ?? []).filter((g) => g.meal_id === selectedMeal)
    .map((g) => ({ value: String(g.id), text: String(g.name) }));

  return (
    <main className="container admin-page">
      <header className="topbar">
        <Link className="brand" href="/"><span className="brand-mark">G</span> Go Restaurant</Link>
        <span className="tag">Catalog management</span>
      </header>
      <section className="catalog-header">
        <span className="eyebrow">GROUP 3 · RESTAURANT ADMIN</span>
        <h1>Menus & meals</h1>
        <p>Manage your restaurant catalog. All changes are verified by the Go API against your current permissions and assigned restaurant.</p>
      </section>
      {message && <p className="notice" role="status">{message}</p>}
      <section className="admin-grid">
        <div className="panel">
          <h2>Restaurants</h2>
          <label className="form-field">Select restaurant
            <select value={restaurantID} onChange={(e) => setRestaurantID(e.target.value)}>
              <option value="">Select a restaurant</option>
              {restaurants.map((r) => <option key={r.id} value={r.id}>{r.name} ({r.slug})</option>)}
            </select>
          </label>
          <form onSubmit={createRestaurant} className="field-stack">
            <h3>Create restaurant (admin)</h3>
            <label className="form-field">Name <input required maxLength={140} value={restaurant.name} onChange={(e) => setRestaurant({ ...restaurant, name: e.target.value })} /></label>
            <label className="form-field">Slug <input required pattern="[a-z0-9]+(-[a-z0-9]+)*" maxLength={100} value={restaurant.slug} onChange={(e) => setRestaurant({ ...restaurant, slug: e.target.value })} /></label>
            <label className="form-field">Currency <input required maxLength={3} value={restaurant.currency} onChange={(e) => setRestaurant({ ...restaurant, currency: e.target.value.toUpperCase() })} /></label>
            <button className="action" disabled={busy}>Create restaurant</button>
          </form>
        </div>
        {catalog && <div className="panel">
          <h2>Restaurant settings</h2>
          <form className="field-stack" onSubmit={updateRestaurant}>
            <label className="form-field">Name <input required value={catalog.restaurant.name} onChange={(e) => setCatalog({ ...catalog, restaurant: { ...catalog.restaurant, name: e.target.value } })} /></label>
            <label className="form-field">Slug <input required value={catalog.restaurant.slug} onChange={(e) => setCatalog({ ...catalog, restaurant: { ...catalog.restaurant, slug: e.target.value } })} /></label>
            <label className="form-field">Description <textarea value={catalog.restaurant.description} onChange={(e) => setCatalog({ ...catalog, restaurant: { ...catalog.restaurant, description: e.target.value } })} /></label>
            <label className="form-field">Image URL (HTTPS) <input type="url" value={catalog.restaurant.image_url} onChange={(e) => setCatalog({ ...catalog, restaurant: { ...catalog.restaurant, image_url: e.target.value } })} /></label>
            <label className="check-field"><input type="checkbox" checked={catalog.restaurant.is_active} onChange={(e) => setCatalog({ ...catalog, restaurant: { ...catalog.restaurant, is_active: e.target.checked } })} /> Published restaurant</label>
            <p className="muted">Currency {catalog.restaurant.currency} is immutable after creation.</p>
            <button className="action secondary" disabled={busy}>Save settings</button>
          </form>
          <div className="field-stack">
            <h3>Assign restaurant manager (admin)</h3>
            <label className="form-field">Manager user UUID <input value={managerID} onChange={(e) => setManagerID(e.target.value)} placeholder="User UUID" /></label>
            <button className="action secondary" disabled={busy || !managerID} onClick={() => void mutation(async () => {
              await request(root + "/managers/" + managerID, "PUT");
              setManagerID("");
            })}>Assign manager</button>
            <button className="action secondary" disabled={busy || !managerID} onClick={() => void mutation(async () => {
              await request(root + "/managers/" + managerID, "DELETE");
              setManagerID("");
            })}>Remove manager</button>
          </div>
        </div>}
      </section>
      {catalog && <>
        <Editor key={root + "categories"} title="Categories" root={root + "/categories"} records={catalog.categories}
          fields={[{ key: "name", title: "Name" }, { key: "sort_order", title: "Order", type: "number", defaultValue: 0 }, { key: "is_active", title: "Visible", type: "checkbox", defaultValue: false }]}
          request={request} changed={() => refreshCatalog(restaurantID)} />
        <Editor key={root + "meals"} title="Meals" root={root + "/meals"} records={catalog.meals}
          fields={[{ key: "category_id", title: "Category", type: "select", options: categoryChoices }, { key: "name", title: "Meal name" },
            { key: "description", title: "Description" }, { key: "price_minor", title: "Price (minor units; AED fils)", type: "number", defaultValue: 0 },
            { key: "image_url", title: "HTTPS image URL", type: "url" },
            { key: "is_active", title: "Published", type: "checkbox", defaultValue: false },
            { key: "is_available", title: "Available", type: "checkbox", defaultValue: true },
            { key: "sort_order", title: "Order", type: "number", defaultValue: 0 }]}
          request={request} changed={() => refreshCatalog(restaurantID)} />
        <div className="panel">
          <label className="form-field">Select meal to manage modifiers
            <select value={selectedMeal} onChange={(e) => { setSelectedMeal(e.target.value); setSelectedGroup(""); }}>
              <option value="">Select a meal</option>
              {mealChoices.map((x) => <option value={x.value} key={x.value}>{x.text}</option>)}
            </select>
          </label>
        </div>
        {selectedMeal && <Editor key={root + selectedMeal + "groups"} title="Modifier groups"
          root={root + "/meals/" + selectedMeal + "/modifier-groups"}
          records={catalog.modifier_groups.filter((g) => g.meal_id === selectedMeal)}
          fields={[{ key: "name", title: "Group name" }, { key: "min_select", title: "Minimum selections", type: "number", defaultValue: 0 },
            { key: "max_select", title: "Maximum selections", type: "number", defaultValue: 1 }, { key: "sort_order", title: "Order", type: "number", defaultValue: 0 }]}
          request={request} changed={() => refreshCatalog(restaurantID)} />}
        {selectedMeal && <div className="panel">
          <label className="form-field">Select modifier group for options
            <select value={selectedGroup} onChange={(e) => setSelectedGroup(e.target.value)}>
              <option value="">Select a group</option>
              {groupChoices.map((x) => <option value={x.value} key={x.value}>{x.text}</option>)}
            </select>
          </label>
        </div>}
        {selectedMeal && selectedGroup && <Editor key={selectedGroup} title="Modifier options"
          root={root + "/meals/" + selectedMeal + "/modifier-groups/" + selectedGroup + "/options"}
          records={catalog.modifier_options.filter((o) => o.group_id === selectedGroup)}
          fields={[{ key: "name", title: "Option name" }, { key: "price_delta_minor", title: "Additional price (minor units)", type: "number", defaultValue: 0 },
            { key: "is_available", title: "Available", type: "checkbox", defaultValue: true },
            { key: "sort_order", title: "Order", type: "number", defaultValue: 0 }]}
          request={request} changed={() => refreshCatalog(restaurantID)} />}
      </>}
      <footer>Go Restaurant · Go-backed catalog operations</footer>
    </main>
  );
}

function Editor({ title, root, records, fields, request, changed }: {
  title: string; root: string; records: Item[]; fields: Field[];
  request: (path: string, method?: string, body?: unknown) => Promise<unknown>;
  changed: () => Promise<void>;
}) {
  const defaults = useCallback(() => Object.fromEntries(fields.map((f) => [f.key, f.defaultValue ?? (f.type === "checkbox" ? false : f.type === "number" ? 0 : "")])),
    [fields]);
  const [data, setData] = useState<Record<string, string | number | boolean>>(() => defaults());
  const [editing, setEditing] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  function edit(item: Item) {
    setEditing(item.id);
    setData(Object.fromEntries(fields.map((f) => [f.key, item[f.key] ?? f.defaultValue ?? ""])));
  }
  function reset() { setEditing(""); setData(defaults()); }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage("");
    try {
      await request(root + (editing ? "/" + editing : "/"), editing ? "PUT" : "POST", data);
      await changed(); reset(); setMessage("Saved.");
    } catch (e) { setMessage(e instanceof Error ? e.message : "Save failed"); }
    finally { setBusy(false); }
  }
  async function remove(id: string) {
    if (!window.confirm("Delete this record? Related modifier records may also be deleted.")) return;
    setBusy(true); setMessage("");
    try { await request(root + "/" + id, "DELETE"); await changed(); if (editing === id) reset(); }
    catch (e) { setMessage(e instanceof Error ? e.message : "Delete failed"); }
    finally { setBusy(false); }
  }

  return (
    <section className="panel catalog-editor">
      <h2>{title}</h2>
      <div className="record-grid">
        {records.map((item) => <div key={item.id} className="record">
          <strong>{String(item.name)}</strong>
          <span className="muted">{item.price_minor !== undefined ? String(item.price_minor) + " minor units" : item.price_delta_minor !== undefined ? "+" + String(item.price_delta_minor) : ""}</span>
          <div className="record-actions">
            <button type="button" className="action secondary" onClick={() => edit(item)}>Edit</button>
            <button type="button" className="action secondary" disabled={busy} onClick={() => void remove(item.id)}>Delete</button>
          </div>
        </div>)}
      </div>
      {records.length === 0 && <p className="muted">No records yet.</p>}
      <form className="field-stack editor-form" onSubmit={save}>
        <h3>{editing ? "Edit" : "Add"} {title.toLowerCase()}</h3>
        {fields.map((field) => field.type === "checkbox" ?
          <label className="check-field" key={field.key}>
            <input type="checkbox" checked={Boolean(data[field.key])}
              onChange={(e) => setData({ ...data, [field.key]: e.target.checked })} />
            {field.title}
          </label> :
          <label className="form-field" key={field.key}>{field.title}
            {field.type === "select" ?
              <select value={String(data[field.key] ?? "")} required onChange={(e) => setData({ ...data, [field.key]: e.target.value })}>
                <option value="">Select</option>
                {field.options?.map((option) => <option value={option.value} key={option.value}>{option.text}</option>)}
              </select> :
              <input type={field.type === "number" ? "number" : field.type === "url" ? "url" : "text"}
                min={field.type === "number" ? 0 : undefined}
                value={String(data[field.key] ?? "")} required={field.key === "name"}
                onChange={(e) => setData({ ...data, [field.key]: field.type === "number" ? Number(e.target.value) : e.target.value })} />
            }
          </label>
        )}
        <div className="record-actions">
          <button className="action" disabled={busy}>{editing ? "Update" : "Create"}</button>
          {editing && <button type="button" className="action secondary" onClick={reset}>Cancel edit</button>}
        </div>
      </form>
      {message && <p className="notice" role="status">{message}</p>}
    </section>
  );
}
