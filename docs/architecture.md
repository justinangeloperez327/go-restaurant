# Architecture

Go + Chi API, Next.js web client, PostgreSQL database. Modular monolith; no microservices, Redis or separate authentication service.

Group 2 adds Google/Apple authorization-code sign-in, provider ID-token verification, signed Apple client-secret generation, opaque sessions, independent provider identities, roles and permissions, transactional SQL migrations, explicit CORS origin and Origin checks for cookie-authenticated mutation endpoints.

Group 3 will implement menu and restaurant management with role enforcement and ownership checks. Order and delivery ownership rules will follow in their own groups.
