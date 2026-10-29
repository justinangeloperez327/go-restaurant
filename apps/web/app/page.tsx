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

  return (
    <main className="container">
      <header className="topbar">
        <div className="brand"><span className="brand-mark">G</span> Go Restaurant</div>
        <span className="tag">Group 1 · Foundation</span>
      </header>
      <section className="hero">
        <span className="eyebrow">RESTAURANT PLATFORM</span>
        <h1>A clean foundation for ordering, meal plans, and delivery.</h1>
        <p>
          Go API, PostgreSQL, and Next.js are connected. Customer ordering,
          subscriptions, rider tracking, and administration will be built in the next groups.
        </p>
        <div className="status">
          <span className={api === "online" ? "dot online" : "dot offline"} aria-hidden="true" />
          <span>Go API: <strong>{api}</strong></span>
        </div>
      </section>
      <footer>Go Restaurant · Development foundation</footer>
    </main>
  );
}
