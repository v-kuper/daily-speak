# Backend as a mobile platform

## Goal and current contract

The Go backend is the product API for mobile clients. The Next.js web app is a
sandbox client. Mobile and web call the backend directly, and the backend owns
identity, recording state, media metadata, processing, and the API contract.
The currently deployed cookie API and data remain usable during migration.

The first release can use one API and one worker. Every durable state change
must be stored outside their process so replicas and separate worker pools can
be added without changing client contracts.

## User journey

1. A new installation receives a short-lived anonymous identity from the API.
2. The guest uploads at most one bounded recording. The backend produces a
   transcript and one or two high-confidence preview corrections within a
   separately metered guest budget. The guest response contains no hidden
   analysis data.
3. Registration upgrades that identity to an account. Login to an existing
   account merges the guest recording into that account in one transaction.
   Retries cannot duplicate the merge or trigger duplicate paid processing.
4. The backend schedules full analysis, corrected text, and shadowing. The
   client polls a stable recording resource until it is ready or failed.

The guest preview never starts the full multipass analysis or TTS. Guest
identity has a short expiry, rate and size limits, and a cleanup deadline.
Guests may read only their own preview. The account receives the full result.

## Identity and compatibility

Add a backend-owned principal for guest and registered ownership. Existing
users and their recordings are backfilled without changing their IDs or the
current cookie sessions. New mobile endpoints live under `/api/v1` and use
short-lived access tokens and rotating opaque refresh tokens. Refresh tokens
are stored only as hashes and scoped to a device family. Reuse revokes that
family. Logout and logout-all revoke stored grants. The existing `/api/auth/*`
cookie flow remains until web migration is tested.

## Processing and media

API requests create durable jobs in PostgreSQL in the same transaction as the
recording state change. Worker processes claim jobs with a lease, retry with
backoff, and publish terminal failure. Preview, transcription, full analysis,
TTS, and cleanup have separate concurrency and cost controls. A repeat delivery
must not repeat billable work after a successful result.

An object-store interface owns media. Local storage supports development;
production uses S3-compatible storage. Mobile clients upload and download via
short-lived signed URLs after backend authorization. PostgreSQL stores object
keys and metadata, not media bytes. CDN delivery and distinct worker pools can
be added when measured traffic warrants them.

## Operational bounds

API replicas are stateless. Migrations are versioned and reversible by a
tested forward repair, with an explicit deploy step before multiple replicas.
Production PostgreSQL TLS verifies the certificate and hostname. Connection
pools, queue depth, oldest job age, p95/p99 latency, error rate, storage errors,
and provider spend are measured. Admission control limits guest work and
returns a retryable response when capacity is exhausted. Backups and restore
drills cover PostgreSQL and media.

## Delivery sequence

Each epic keeps the deployed API working and has its own tests and rollout.

1. **Database foundation:** versioned, serialized migrations; verified
   PostgreSQL TLS. This epic changes no public endpoint or table shape.
2. **Mobile API contract:** `/api/v1`, stable error codes, pagination,
   request IDs, OpenAPI checks, and a compatibility policy.
3. **Identity:** principal backfill, anonymous grants, access/refresh rotation,
   device sessions, logout, and account upgrade/merge. Keep cookie endpoints.
4. **Durable work:** transactional job creation, leases, recovery, idempotency,
   separate API/worker entrypoints, bounded concurrency.
5. **Media:** storage interface, S3 implementation, signed multipart uploads,
   authorized download, checksums, retention, and legacy file migration.
6. **Guest preview:** bounded transcript and one or two preview corrections,
   caps and abuse controls, atomic promotion to full processing after auth.
7. **Scale and operations:** rate limits, readiness, metrics, tracing, load and
   fault tests, backups, and scaling policy for API, workers, and PostgreSQL.

Do not claim mobile readiness until the guest-to-account journey, token
rotation/replay, crash recovery, media authorization, and load admission have
passed end-to-end tests. The web sandbox is not the source of truth for any of
those flows.

## First epic: database foundation

The embedded `0001_init.sql` becomes an immutable baseline in a sorted
migration catalog. On existing databases the idempotent baseline executes once
under an advisory transaction lock, then its checksum is recorded. Each later
migration is applied once in version order, transactionally, and a checksum
mismatch aborts startup before serving traffic. Concurrent startup cannot
apply the same migration twice.

`DATABASE_SSL=true` accepts only a parsed PostgreSQL connection configuration
whose primary and fallback connections verify the server certificate and host.
The current Docker-local deployment keeps its non-TLS setting. Production
connection URLs use `sslmode=verify-full` and a trusted CA when needed. The
application never replaces pgx TLS settings with an insecure override.
