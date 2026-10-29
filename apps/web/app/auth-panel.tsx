"use client";

import { useCallback, useEffect, useState } from "react";

type User = { id: string; email: string; display_name: string; roles: string[] };
type Providers = { providers: string[] };

export function AuthPanel({ apiURL }: { apiURL: string }) {
  const [providers, setProviders] = useState<string[]>([]);
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    try {
      const res = await fetch(apiURL + "/api/v1/me", {
        cache: "no-store",
        credentials: "include",
      });
      if (res.ok) {
        setUser((await res.json()) as User);
      } else {
        setUser(null);
      }
    } catch {
      setNotice("The API is not reachable.");
    }
  }, [apiURL]);

  useEffect(() => {
    let active = true;
    Promise.all([
      fetch(apiURL + "/api/v1/auth/providers", { cache: "no-store" }).then(async (r) => {
        if (!r.ok) throw new Error("Providers unavailable");
        return (await r.json()) as Providers;
      }),
      fetch(apiURL + "/api/v1/me", { credentials: "include", cache: "no-store" }),
    ])
      .then(async ([config, me]) => {
        if (!active) return;
        setProviders(config.providers);
        if (me.ok) setUser((await me.json()) as User);
      })
      .catch(() => {
        if (active) setNotice("Unable to reach the Go authentication API.");
      })
      .finally(() => {
        if (active) setReady(true);
      });
    return () => { active = false; };
  }, [apiURL]);

  async function logout() {
    setNotice("");
    try {
      const result = await fetch(apiURL + "/api/v1/auth/logout", {
        method: "POST",
        credentials: "include",
      });
      if (!result.ok) throw new Error("Logout failed");
      await refresh();
      setNotice("Signed out.");
    } catch {
      setNotice("Could not sign out. Try again.");
    }
  }

  return (
    <section className="auth-panel" aria-label="Account">
      <h2>Your account</h2>
      {!ready ? (
        <p>Checking sign-in options…</p>
      ) : user ? (
        <>
          <p>Signed in as <strong>{user.display_name || user.email || "Customer"}</strong></p>
          <p className="muted">Roles: {user.roles.join(", ")}</p>
          <button className="action secondary" onClick={logout}>Sign out</button>
          {user.roles.includes("admin") && <AdminUsers apiURL={apiURL} />}
        </>
      ) : providers.length ? (
        <div className="auth-actions">
          {providers.includes("google") && (
            <a className="action" href={apiURL + "/api/v1/auth/google/start"}>Continue with Google</a>
          )}
          {providers.includes("apple") && (
            <a className="action secondary" href={apiURL + "/api/v1/auth/apple/start"}>Continue with Apple</a>
          )}
        </div>
      ) : (
        <p className="muted">Configure Google or Apple developer credentials to enable sign-in.</p>
      )}
      {notice && <p className="notice" role="status">{notice}</p>}
    </section>
  );
}

function AdminUsers({ apiURL }: { apiURL: string }) {
  const [users, setUsers] = useState<User[]>([]);
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);

  async function loadUsers() {
    setLoading(true);
    try {
      const result = await fetch(apiURL + "/api/v1/admin/users", {
        credentials: "include", cache: "no-store",
      });
      if (!result.ok) throw new Error("Unable to load users");
      const body = (await result.json()) as { users: User[] };
      setUsers(body.users);
      setMessage("");
    } catch {
      setMessage("Unable to load users. Check your permissions.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="admin-users">
      <h3>Admin · Users</h3>
      <button className="action secondary" type="button" onClick={loadUsers} disabled={loading}>
        {loading ? "Loading…" : "Load users"}
      </button>
      {users.length > 0 && (
        <ul className="user-list">
          {users.map((u) => (
            <li key={u.id}>
              <span>{u.display_name || u.email || "Unnamed user"}</span>
              <small>{u.roles.join(", ")}</small>
            </li>
          ))}
        </ul>
      )}
      {message && <p role="status" className="notice">{message}</p>}
    </div>
  );
}
