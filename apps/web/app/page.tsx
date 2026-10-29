import { AuthPanel } from "./auth-panel";

type ApiState = "online" | "offline";

async function getApiState(): Promise<ApiState> {
  const baseUrl = process.env.API_INTERNAL_URL ?? "http://localhost:8080";
  try {
    const response = await fetch(new URL("/healthz", baseUrl), {
      cache: "no-store",
      signal: AbortSignal.timeout(3000),
    });
    return response.ok ? "online" : "offline";
  } catch {
    return "offline";
  }
}

export default async function HomePage() {
  const api = await getApiState();
  const apiURL = process.env.API_PUBLIC_URL ?? "http://localhost:8080";

  return (
    <main className="container">
      <header className="topbar">
        <div className="brand"><span className="brand-mark">G</span> Go Restaurant</div>
        <span className="tag">Group 2 · Authentication and RBAC</span>
      </header>
      <section className="hero">
        <span className="eyebrow">RESTAURANT PLATFORM</span>
        <h1>Your meals, your schedule, one account.</h1>
        <p>
          Go, Next.js, and PostgreSQL power the platform. Google and Apple
          sign-in, account sessions, and server-enforced roles are now available
          when provider credentials are configured.
        </p>
        <div className="status">
          <span className={api === "online" ? "dot online" : "dot offline"} aria-hidden="true" />
          <span>Go API: <strong>{api}</strong></span>
        </div>
        <AuthPanel apiURL={apiURL} />
      </section>
      <footer>Go Restaurant · Authentication and RBAC foundation</footer>
    </main>
  );
}
