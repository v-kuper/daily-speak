# Architecture

Last reviewed: 2026-09-27

## System boundary

Daily Speaking is a monorepo with two independently buildable applications and
two backend process roles:

```text
web or mobile client
        |
        | HTTPS / JSON API and signed media requests
        v
Go API -------- PostgreSQL
  |                 |
  | queues work     | durable jobs, identity, metadata
  v                 |
Go worker ----------+
  |
  +-- local persistent media (test) or private S3-compatible storage
  +-- Whisper, Ollama, and Cartesia
```

- `web/` is a Next.js sandbox client. It owns browser routes and UI state, but
  no backend business logic or server credentials.
- `backend/internal/app` is the composition root for both process roles. It
  selects providers and storage adapters and wires repositories and services.
- `backend/cmd/api` parses process configuration, opens shared infrastructure,
  and starts the already-composed HTTP API.
- `backend/cmd/worker` owns paid or slow transcription, analysis, TTS, and
  cleanup work. It uses the same backend image but is a separate process.
- PostgreSQL is the source of truth for identities, sessions, recordings,
  media metadata, durable jobs, quotas, and distributed rate limits.
- Media bytes are owned by the backend storage interface. Local disk is the
  current test deployment; S3-compatible storage is the multi-host path.

The backend does not serve or proxy Next.js. The web client knows the backend
only through `PUBLIC_API_BASE_URL` and the published HTTP contract. The backend
accepts configured browser origins through CORS but has no web build/runtime
dependency. A native client can therefore call the same API directly.

## Public contracts

- `/api/v1/*` is the stable client contract shared by mobile and web identity,
  media, guest preview, and new recording development.
- `/api/*` is the remaining legacy web resource surface for profile, practice,
  recording detail/actions, and the retained Feed. Recording creation uses the
  same v1 media-backed contract in web and mobile.
- `/openapi.json` and `/docs` expose the backend-owned OpenAPI contract and
  Swagger UI.
- `/healthz` is process liveness; `/readyz` checks PostgreSQL and queue
  admission; `/metrics` is a protected operations endpoint.
- `/uploads/*` is the legacy backend media path. New mobile media flows use
  authorized media resources and short-lived signed requests.

Compatibility and deprecation rules live in
[`api-compatibility.md`](api-compatibility.md).

## Identity flow

Web and mobile use one backend-issued identity model:

1. A new installation calls `POST /api/v1/auth/anonymous` and receives a guest
   principal, device session, short-lived access token, and rotating opaque
   refresh token. Native clients receive both tokens in JSON. Browser clients
   receive the access token in JSON and the refresh token only in a scoped,
   Secure HttpOnly cookie.
2. Access tokens are HS256 JWTs scoped by issuer, audience, principal, device
   session, identity kind, and expiry. The signing secret remains server-only.
3. Refresh tokens are random, single-use, and stored only as SHA-256 hashes.
   Reuse revokes the affected device session.
4. Registration or login can atomically merge the guest principal and its
   preview into the user account. Logout, logout-all, and device revocation are
   server-side operations.

All clients keep access tokens in memory. Native apps keep refresh tokens in
OS-protected secure storage; browser JavaScript cannot read its refresh cookie.
Clients never receive signing secrets.

## Recording and guest flow

State transitions that schedule work persist the resource and its job in the
same PostgreSQL transaction. Workers claim jobs with leases, send heartbeats,
retry with bounded exponential backoff, and publish terminal failure. Stable
idempotency keys prevent client retries and worker redelivery from duplicating
user-visible or billable work.

A guest may submit one bounded preview. The guest path performs transcription
and at most two high-confidence corrections; it does not run full multipass
analysis or TTS. Account promotion queues full processing while reusing work
that already succeeded.

Recording, guest-preview, shadowing, and cleanup jobs have independent worker
concurrency controls. Increasing API replicas never implicitly increases paid
processing concurrency.

## Media ownership

PostgreSQL stores media identity, ownership, state, object location, size, and
checksum; it does not store media bytes. The storage interface supports local
disk and private S3-compatible storage. Upload creation, completion, download,
and deletion all authorize the owning principal inside the media application
service. Clients exchange the stable, owner-protected media download path for a
short-lived signed request; UI media elements never receive a permanent object
URL. Guest purpose restrictions and account-only downloads are application
policy rather than HTTP rules. Storage responses are mapped to media-owned
application types before they reach HTTP. Multipart uploads and signed requests
are bounded and expire.

The current Windows test host mounts one persistent local directory into API
and worker. A multi-host deployment must switch to shared object storage before
API and workers are placed on different machines.

## Scale and failure model

API processes are stateless apart from PostgreSQL and media storage. They can
scale horizontally behind a trusted proxy. PostgreSQL owns rate-limit counters
and durable jobs so replicas share admission and work state. Worker pools scale
independently by job kind and provider capacity.

The current Compose/Caddy topology is a single-host test environment, not a
high-availability production deployment. Production still requires managed or
operated PostgreSQL, shared object storage, independent ingress/TLS, monitoring,
backup/restore rehearsals, and measured capacity. Operational thresholds and
procedures live in [`BACKEND_OPERATIONS.md`](BACKEND_OPERATIONS.md).

## Code ownership

```text
web/                         standalone Next.js application
backend/cmd/api              API process entrypoint and lifecycle
backend/cmd/worker           durable worker entrypoint and lifecycle
backend/internal/app         API and worker dependency composition root
backend/internal/aiparse     provider-neutral model-output normalization
backend/internal/auth        unified web/mobile identity and token lifecycle
backend/internal/db          PostgreSQL connection and migrations
backend/internal/httpapi     HTTP transport, authorization gates, response mapping
backend/internal/feed        retained Feed service, persistence, and reply-media adapter
backend/internal/learner     shared learner level and interest vocabulary
backend/internal/media       authorized media lifecycle
backend/internal/practice    speaking-practice generation application service
backend/internal/practice/ollamaadapter  Ollama adapter for the practice port
backend/internal/quota       recording quota policy and formatting
backend/internal/recording   recording creation, deletion, processing, analysis
backend/internal/guestpreview     bounded anonymous preview lifecycle
backend/internal/shadowing        pronunciation generation lifecycle
backend/internal/profile          profile application service and repository
backend/internal/subscription     subscription application service and repository
backend/internal/storage     local and S3 storage adapters
backend/internal/worker      worker configuration and pool lifecycle
backend/internal/workqueue   durable PostgreSQL queue
backend/internal/operations  rate limits, proxy trust, and metrics
backend/migrations           immutable ordered schema migrations
backend/docs                 generated OpenAPI and Swagger assets
```

Production SQL and transaction management live in feature repositories rather
than `backend/internal/httpapi`. Cross-feature vocabulary has an explicit
owner: learner data belongs to `learner`, quota rules to `quota`, media formats
to `media`, practice normalization to `practice`, recording text to
`recording`, and local shadowing paths to `shadowing`. There is no catch-all
domain or utilities package. New business rules must not be added to the
transport or composition packages.

## Retained Feed backend

The web Feed UI and publication controls are intentionally absent. Feed API,
data, migrations, and OpenAPI operations remain until product, privacy,
moderation, retention, and migration requirements support either restoration
or removal. New clients must not expose Feed merely because routes still exist.
