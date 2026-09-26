# Media storage and direct mobile uploads

## Goal

Move backend-owned media behind a storage abstraction so the API and workers no
longer require the same local filesystem in production. Keep the current
Windows Compose deployment working with local storage while adding a private,
S3-compatible production backend and a stable mobile upload/download contract.

## Compatibility boundary

- `MEDIA_STORAGE_DRIVER=local` is the default and remains explicit in the
  current Windows deployment.
- Existing cookie routes, `/uploads/...` URLs, the uploads bind mount, and
  existing database columns remain available during migration.
- New mobile responses persist media IDs and stable API paths, never expiring
  provider URLs.
- The bucket is private. Bucket names, object keys, credentials, and provider
  URLs are server-owned details.
- Guest principals are represented in the schema and ownership transfer, but
  direct guest uploads stay disabled until the guest-preview epic adds quotas,
  abuse controls, and cleanup policy.

## Data model

Add `media_assets` with principal ownership, purpose, provider/key, expected and
verified size/checksum, state, retention, attachment, and deletion timestamps.
Add `media_uploads` for resumable multipart state, provider upload ID,
idempotency, expiry, and the creating device session. Add nullable asset
references to recording and Feed media while retaining legacy URL columns.

Backfill existing `/uploads/...` values as `local` assets with deterministic
IDs and object keys. The migration does not read or delete files, so it remains
transactional and safe on both existing and new databases.

## Storage contract

`internal/media` owns:

- strict environment configuration for `local` and `s3`;
- local and S3-compatible object stores;
- multipart create, part signing/receiving, list, complete, and abort;
- bounded materialization to a temporary local file for Whisper;
- object metadata verification and idempotent deletion;
- short-lived authorized download requests;
- SHA-256 and exact-size validation.

Local multipart URLs are API-relative and HMAC signed. S3 multipart URLs are
presigned with the official AWS SDK for Go v2. The client treats every URL and
required header as opaque.

## Mobile API

- `POST /api/v1/media/uploads` creates an idempotent upload intent.
- `GET /api/v1/media/uploads/{uploadId}` resumes an upload.
- `POST /api/v1/media/uploads/{uploadId}/parts` signs a bounded batch of parts.
- `PUT /api/v1/media/uploads/{uploadId}/parts/{partNumber}` receives a signed
  local-storage part without proxying S3 traffic through the API.
- `POST /api/v1/media/uploads/{uploadId}/complete` verifies and publishes the
  asset idempotently.
- `DELETE /api/v1/media/uploads/{uploadId}` aborts idempotently.
- `GET /api/v1/media/{assetId}/download` authorizes ownership and returns a
  short-lived opaque download request.

All authenticated management routes use the v1 error envelope. Foreign media
returns `404`, not `403`. Upload purpose, MIME type, extension, size, part count,
and checksum are server validated. `Idempotency-Key` is required when creating
an upload intent.

## Legacy migration

Add an explicit `cmd/media-migrate` command. It supports dry-run and resumable
copy from legacy local paths into the configured target store, computes SHA-256
and size, verifies the stored object, then updates the asset provider/key. It
never runs automatically during deploy and never deletes the source file.

Workers resolve asset IDs first and legacy URLs second. Cleanup accepts both
asset IDs and old `/uploads/...` resources until the compatibility window ends.

## Delivery tasks

1. Add the additive schema and migration/backfill tests.
2. Add the storage abstraction, local adapter, S3 adapter, config validation,
   signing, and contract tests.
3. Add authenticated v1 upload/download handlers, ownership checks,
   idempotency, resume, complete/abort, and OpenAPI coverage.
4. Integrate assets into recording/shadowing workers and durable cleanup with
   legacy fallback.
5. Add the dry-run/resumable legacy migration command.
6. Wire server-only environment configuration into API/worker/Compose/CI while
   pinning the existing Windows topology to local storage.
7. Run formatting, unit, race, API-doc, migration, boundary, and workflow tests.
   Do not start Docker or local services in this environment.

## Rollout

Deploy the additive schema and local adapter first. Enable the v1 media routes
while still using local storage, then test the mobile contract. Configure a
private S3-compatible bucket in a production-like environment, run migration in
dry-run mode, copy and audit legacy objects, and only then switch the driver.
Public legacy routes are removed only after all clients and Feed policy have a
separate reviewed migration.
