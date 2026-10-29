"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";

type Profile = { full_name: string; phone: string; email: string };
type Address = {
 id: string; label: string; recipient_name: string; phone: string; country_code: string; city: string;
 area: string; street: string; building: string; apartment: string; instructions: string; is_default: boolean;
};
const emptyAddress: Omit<Address,"id"> = {
 label:"Home", recipient_name:"", phone:"", country_code:"AE", city:"Abu Dhabi",
 area:"", street:"", building:"", apartment:"", instructions:"", is_default:false,
};

export function AccountClient({ apiURL }: { apiURL: string }) {
  const [profile, setProfile] = useState<Profile>({ full_name:"", phone:"", email:"" });
  const [addresses, setAddresses] = useState<Address[]>([]);
  const [draft, setDraft] = useState<Omit<Address,"id">>(emptyAddress);
  const [editing, setEditing] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const request = useCallback(async (path: string, method = "GET", body?: unknown): Promise<unknown> => {
    const response = await fetch(apiURL + "/api/v1" + path, {
      method, credentials:"include", cache:"no-store",
      ...(body === undefined ? {} : { headers:{ "Content-Type":"application/json" }, body:JSON.stringify(body) }),
    });
    if (!response.ok) {
      const payload = (await response.json().catch(() => ({}))) as { error?: string };
      throw new Error(response.status === 401 ? "Please sign in on the home page first." : (payload.error || "Request failed."));
    }
    return response.status === 204 ? null : response.json();
  }, [apiURL]);

  const reload = useCallback(async () => {
    const [p, a] = await Promise.all([request("/me/profile"), request("/me/addresses")]);
    setProfile(p as Profile); setAddresses(a as Address[]);
  }, [request]);

  useEffect(() => {
    let active = true;
    Promise.all([request("/me/profile"), request("/me/addresses")])
      .then(([p,a]) => { if (active) { setProfile(p as Profile);setAddresses(a as Address[]); } })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "Could not load account."); });
    return () => { active = false; };
  }, [request]);

  async function perform(action: () => Promise<void>) {
    setBusy(true); setError(""); setMessage("");
    try { await action(); await reload(); setMessage("Saved successfully."); }
    catch (e) { setError(e instanceof Error ? e.message : "Request failed."); }
    finally { setBusy(false); }
  }

  async function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await perform(async () => { await request("/me/profile", "PUT", { full_name:profile.full_name, phone:profile.phone }); });
  }
  async function saveAddress(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await perform(async () => {
      await request("/me/addresses" + (editing ? "/" + editing : ""), editing ? "PUT" : "POST", draft);
      setEditing(""); setDraft(emptyAddress);
    });
  }
  function edit(a: Address) {
    setEditing(a.id);
    setDraft({ label:a.label, recipient_name:a.recipient_name, phone:a.phone, country_code:a.country_code,
      city:a.city, area:a.area, street:a.street, building:a.building, apartment:a.apartment,
      instructions:a.instructions, is_default:a.is_default });
  }
  function field<K extends keyof Omit<Address,"id">>(key: K, value: Omit<Address,"id">[K]) {
    setDraft((current) => ({ ...current, [key]:value }));
  }

  return <div className="account-sections">
    {message && <p className="notice" role="status">{message}</p>}
    {error && <p className="notice" role="alert">{error}</p>}
    <section className="panel">
      <h2>Profile</h2>
      <p className="muted">Signed-in email: {profile.email || "Not provided by identity provider"}</p>
      <form className="field-stack" onSubmit={saveProfile}>
        <label className="form-field">Full name
          <input maxLength={140} value={profile.full_name} onChange={(e) => setProfile({ ...profile, full_name:e.target.value })} />
        </label>
        <label className="form-field">Phone
          <input type="tel" maxLength={24} placeholder="+971 50 123 4567" value={profile.phone}
            onChange={(e) => setProfile({ ...profile, phone:e.target.value })} />
        </label>
        <button className="action" disabled={busy}>Save profile</button>
      </form>
    </section>
    <section className="panel">
      <h2>Delivery addresses</h2>
      {addresses.map((a) => <div className="account-address" key={a.id}>
        <div><strong>{a.label}{a.is_default ? " · Default" : ""}</strong>
          <p>{a.recipient_name} · {a.phone}</p>
          <p>{[a.apartment,a.building,a.street,a.area,a.city,a.country_code].filter(Boolean).join(", ")}</p>
        </div>
        <div className="record-actions">
          <button className="action secondary" onClick={() => edit(a)} disabled={busy}>Edit</button>
          <button className="action secondary" onClick={() => void perform(async () => {
            await request("/me/addresses/" + a.id, "DELETE");
            if (editing === a.id) { setEditing("");setDraft(emptyAddress); }
          })} disabled={busy}>Delete</button>
        </div>
      </div>)}
      <form className="field-stack editor-form" onSubmit={saveAddress}>
        <h3>{editing ? "Edit delivery address" : "Add delivery address"}</h3>
        {([
          ["label","Label (e.g. Home)"],["recipient_name","Recipient name"],["phone","Phone"],
          ["country_code","Country code"],["city","City"],["area","Area"],["street","Street"],
          ["building","Building"],["apartment","Apartment / Unit"],["instructions","Delivery instructions"],
        ] as const).map(([key,title]) => <label className="form-field" key={key}>{title}
          <input value={draft[key]} required={["label","recipient_name","phone","country_code","city","street"].includes(key)}
            maxLength={key==="instructions"?500:200} onChange={(e) => field(key,e.target.value)} />
        </label>)}
        <label className="check-field">
          <input type="checkbox" checked={draft.is_default} onChange={(e) => field("is_default",e.target.checked)} />
          Make default delivery address
        </label>
        <div className="record-actions"><button className="action" disabled={busy}>{editing ? "Update address" : "Save address"}</button>
          {editing && <button className="action secondary" type="button" onClick={() => { setEditing("");setDraft(emptyAddress); }}>Cancel</button>}
        </div>
      </form>
    </section>
  </div>;
}
