# Technical Debt and Follow-up Epics

Last updated: 2026-09-22

## Current baseline

The web/backend separation establishes these boundaries:

- `web/` and `backend/` install, test, build, and ship independently;
- browsers call a runtime-configured API origin directly;
- Next.js App Router URLs replace UI-only screen switching;
- the backend serves its OpenAPI contract and Swagger UI;
- Feed API/data remain in the backend while Feed UI is removed from web;
- the one-host test deployment runs separate web and backend containers.

Authentication intentionally remains the existing PostgreSQL-backed,
HttpOnly session cookie. Existing session-token format, database rows, and
cookie name remain supported. None of the epics below is implemented by the
separation refactor, and none should be represented as completed until its own
security, migration, and rollout acceptance has passed.

## Access/refresh token authentication with rotation and replay detection

Desired outcome: introduce short-lived access tokens and rotating refresh-token
families suitable for independently deployed web and native clients. Define
token audience/issuer, signing-key rotation, revocation, logout-all-devices,
replay detection, expiry, and incident response before choosing browser storage.

Acceptance boundary:

- a reviewed threat model covers XSS, CSRF, token theft, replay, and key loss;
- refresh reuse revokes the affected token family and produces an auditable
  security event;
- server-side revocation and signing-key rotation are integration-tested;
- migration supports current cookie sessions during an explicit compatibility
  window and defines forced sign-out behavior;
- logs and error payloads never expose access or refresh tokens.

This is not a rename of the current session cookie. Until this epic ships, the
cookie flow and exact credentialed CORS configuration remain the supported web
authentication contract.

## Native mobile authentication and secure token storage

Desired outcome: let iOS/Android clients authenticate directly with the API
without embedding browser assumptions or storing long-lived credentials in
plain application storage. Choose OAuth/PKCE or another reviewed native flow
only after the token-auth contract is defined.

Acceptance boundary:

- tokens are stored in Keychain/Keystore-class protected storage;
- refresh, logout, revocation, device loss, clock skew, and offline recovery are
  tested on supported platforms;
- deep-link/callback validation prevents scheme and redirect hijacking;
- the mobile client uses the published API contract without importing web code;
- privacy, telemetry, and app-store requirements are documented.

## `/api/v1` versioning and deprecation policy

Desired outcome: publish an explicitly versioned API contract that web and
mobile releases can consume on independent schedules.

Acceptance boundary:

- versioning rules cover URLs, schemas, errors, pagination, and media paths;
- compatibility and deprecation windows have owners and measurable dates;
- CI compares the OpenAPI contract and blocks unintended breaking changes;
- at least one compatibility strategy exists for clients unable to upgrade
  immediately;
- the migration from current `/api/*` routes has a tested rollback plan.

## Rate limiting, security headers, metrics, tracing, and alerting

Desired outcome: establish production abuse controls and observability for the
independent API and web services.

Acceptance boundary:

- per-route/user/IP rate limits have documented limits, trusted-proxy behavior,
  `429` responses, and distributed-state ownership;
- CSP, HSTS, frame, content-type, and referrer policies are tested at the
  correct web/API boundaries;
- structured metrics and traces correlate via request IDs without recording
  cookies, credentials, recordings, or transcript content;
- dashboards and actionable SLO alerts cover availability, latency, errors,
  queue/background work, database health, and external AI/TTS dependencies;
- load and failure-injection results justify the selected thresholds.

## Feed product decision: redesign and restore or remove with a data migration

Desired outcome: make an explicit product decision about the retained Feed
backend instead of leaving an indefinitely hidden surface.

Acceptance boundary for restoration:

- product/privacy/moderation requirements are approved;
- a new web or mobile experience is tested for publication consent, deletion,
  reactions, replies, abuse handling, and accessibility;
- authorization and OpenAPI coverage remain complete.

Acceptance boundary for removal:

- retention/export obligations are resolved;
- a reviewed, reversible migration removes or archives Feed posts, replies,
  reactions, media, handlers, schemas, and OpenAPI operations;
- recording deletion behavior and rollback are verified against migrated data.

Until then, do not delete Feed tables, migrations, handlers, data, or API docs;
the current web client simply has no Feed entry points.

## Feature-oriented split of the large Go HTTP package and Redux slice

Desired outcome: reduce change coupling by moving auth, practice generation,
recording lifecycle, profile/subscription, Feed, and shared transport concerns
behind feature-owned packages/modules.

Acceptance boundary:

- dependency direction and ownership are documented before files move;
- HTTP/OpenAPI behavior, persisted data, routes, and Redux-visible behavior do
  not change unintentionally;
- focused unit/contract tests protect each extracted feature boundary;
- request orchestration, media cleanup, retries, and background workers retain
  integration coverage;
- no compatibility shims become a second permanent architecture.

This epic includes decomposing `backend/internal/httpapi` and the remaining
large Redux application slice; it is not required for web/backend deployment
independence.

## Separate-resource production deployment definitions

Desired outcome: deploy immutable web and backend images on independently
scalable production resources while preserving direct client-to-API traffic.

Acceptance boundary:

- target platform, domains, DNS, TLS, ingress, secrets, and least-privilege
  identities are declared as reviewed infrastructure/configuration;
- managed PostgreSQL, backup/restore, uploads/object storage, and Whisper model
  persistence have tested disaster-recovery procedures;
- web receives only the public API origin; backend receives explicit permitted
  origins and no web build/runtime dependency;
- readiness, rollout, rollback, schema compatibility, capacity, cost, and
  observability are rehearsed in a production-like environment;
- mobile and web clients can call the same published API origin concurrently.

The current Compose/Caddy deployment is a one-host test topology, not this
future production definition.
