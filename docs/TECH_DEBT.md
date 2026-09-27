# Technical debt and platform roadmap

Last reviewed: 2026-09-27

This file tracks work that is not implemented on `main`. Current architecture
belongs in [`ARCHITECTURE.md`](ARCHITECTURE.md); operational procedures belong
in [`BACKEND_OPERATIONS.md`](BACKEND_OPERATIONS.md).

## Completed foundation

The repository already has independently buildable web and backend projects,
App Router navigation, a versioned `/api/v1` contract, OpenAPI/Swagger,
anonymous-to-account identity, short access tokens, rotating single-use refresh
tokens with replay revocation, one Bearer contract for web and mobile, device
sessions, durable PostgreSQL jobs,
independent workers, local/S3 media adapters, guest preview, distributed rate
limits, readiness, protected metrics, trace correlation, and a Windows
single-host deployment.

Mobile client development can use this foundation now. The remaining items
below are required before claiming broad production readiness or measured high
availability.

## Active architecture refactor plan

This refactor is intentionally separate from production hardening. Its goal is
to leave a small, explicit backend architecture in which transport, application
policy, persistence, and infrastructure adapters can change independently.

Complete the following stages in order, keeping every stage deployable and the
OpenAPI contract and architecture documentation current:

1. **Completed:** Move the web sandbox to the versioned media/recording flow and remove the
   duplicate legacy recording-create and recording-upload-session paths.
2. **Completed:** Remove direct SQL and transaction management from `internal/httpapi`; HTTP
   handlers may validate transport data, call an application service, and map
   its result only.
3. **Completed:** Move dependency construction and environment-driven adapter selection out
   of `internal/httpapi` into an application composition root shared by the API
   and worker entry points where appropriate.
4. **Completed:** Finish the media application boundary so ownership, guest restrictions, and
   lifecycle transitions are application policy rather than HTTP policy.
5. **Completed:** Isolate the retained Feed backend behind its own repository and service
   boundary while keeping it absent from the web and mobile clients.
6. **Completed:** Replace the catch-all `internal/domain` helpers with feature-owned helpers,
   then split large files only where the split follows a real responsibility.

Architecture completion criteria:

- `internal/httpapi` contains no SQL and constructs no database, provider,
  storage, queue, worker, or feature service.
- recording creation has one media-backed application flow for web and mobile.
- each retained feature owns its persistence and business rules.
- dependency direction is transport -> application port -> adapter, with
  environment parsing confined to configuration/composition code.
- `ARCHITECTURE.md`, OpenAPI/Swagger, contract tests, and CI boundary checks
  describe and enforce the resulting structure.

## P0: authentication and security hardening

- Add an access-token key ring with `kid`, an active signing key, overlapping
  verification keys, and a rehearsed rotation/revocation procedure.
- Require a distinct `MEDIA_URL_SIGNING_SECRET`; do not use the access-token
  signing key as a media-signing fallback in production.
- Persist bounded security audit events for login, refresh replay, device
  revocation, logout-all, recovery, and administrative actions.
- Add verified email ownership and password-reset/recovery flows without
  leaking whether an address is registered.
- Apply account-aware login throttling in addition to source-IP limits.
- Complete a threat model for token theft, XSS/CSRF at the browser refresh boundary,
  device loss, key compromise, provider compromise, and sensitive log data.
- Add complete HTTP server timeouts/header limits and uniform request-body
  bounds to legacy as well as v1 handlers.

The initial native client must keep access tokens in memory and refresh tokens
in Keychain/Keystore-class storage. Apple/Google OAuth with PKCE is a separate
product choice, not a prerequisite for email/password mobile development.

## P0: production evidence and recovery

- Run the existing load probe against a production-like remote environment and
  record the first measured capacity, p95/p99, database utilization, queue age,
  provider saturation, and cost per recording.
- Rehearse PostgreSQL loss, stopped workers, expired leases, AI/TTS failure,
  object-storage failure, and disk exhaustion as described in the operations
  runbook.
- Restore a matched PostgreSQL/media backup into an isolated environment and
  record the demonstrated RPO and RTO.
- Define retention, account deletion/export, and privacy behavior for audio,
  transcripts, AI outputs, guest identities, and security events.

Configured thresholds are hypotheses until this evidence exists.

## P1: provider integration boundary

- Move Ollama, Whisper, and Cartesia behind feature-owned ports/adapters rather
  than HTTP-package orchestration.
- Standardize deadlines, retry budgets, non-retryable errors, circuit breaking,
  and provider-specific concurrency bulkheads.
- Export bounded outcome, latency, retry, saturation, and cost metrics per
  provider; add actionable alerts and dashboards.
- Make provider replacement/fallback a configuration and adapter change, not an
  API or recording-domain rewrite.

Do not add a general message broker solely for abstraction. The PostgreSQL
queue remains the default until measured workload demonstrates a limitation.

## P1: feature-oriented backend split

The retained vertical slices now have explicit application boundaries:
practice generation, recording creation/deletion/processing and analysis,
guest preview, shadowing, unified identity, profile, subscription, and Feed.
Worker composition lives outside HTTP; local/S3 and legacy
session files are injected adapters. Their HTTP handlers validate transport
data, call a service, and map its result.

Production HTTP transport no longer owns SQL, transactions, dependency
construction, or cross-feature utility code. Keep those boundaries enforced as
new features are added.

Each extraction must preserve routes, OpenAPI, persisted data, authorization,
idempotency, retry behavior, and integration coverage. Avoid a single large
rewrite or permanent compatibility layer.

## P1: production data and deployment topology

- Move production media to a private S3-compatible store with encryption,
  versioning/lifecycle policy, verified migration, and disaster recovery.
- Set explicit PostgreSQL pool budgets per API/worker replica and introduce a
  pooler only when connection measurements justify it.
- Run migrations as a controlled deployment step and use expand/contract
  schema changes compatible with rolling API/worker releases.
- Define independent immutable web, API, and worker deployment resources with
  DNS, TLS, least-privilege identities, readiness, rollout, and rollback.
- Deploy metrics scraping, dashboards, trace export, centralized logs, and SLO
  alerts. The current W3C trace correlation alone is not distributed tracing.

## P1: CI/CD and supply-chain security

- Add Go vulnerability/static analysis and JavaScript dependency review.
- Scan final container images, produce an SBOM, and automate dependency updates.
- Pin third-party GitHub Actions to reviewed commit SHAs.
- Add a secret scanner and ensure diagnostic commands cannot print deployment
  credentials.
- Compare OpenAPI revisions in CI and reject unapproved breaking `/api/v1`
  changes.

## Product decision: retained Feed backend

Choose either a privacy/moderation-ready Feed product or a reviewed reversible
removal migration. Until that decision, keep Feed tables, media ownership,
handlers, migrations, and OpenAPI operations, but expose no web or mobile Feed
entry point.

Restoration requires explicit publication consent, deletion, moderation, abuse
handling, authorization, and accessibility. Removal requires retention/export
approval, media cleanup, rollback, and migrated-data verification.
