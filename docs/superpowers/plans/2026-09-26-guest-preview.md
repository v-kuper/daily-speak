# Bounded guest preview

## Goal

Let a new mobile installation record one short sample before registration while
keeping the expensive product pipeline behind authentication. A guest receives
only a transcript and at most two high-confidence corrections. Full multipass
analysis, rewrite, and shadowing start only after registration or login.

The legacy web cookie API remains unchanged. The current Windows deployment
continues to use local media storage, and the same API contract also works with
the existing S3 storage adapter.

## Contract

- A guest access token may create one `guest_preview_audio` upload. Photos,
  Feed media, shadowing, and ordinary recordings remain account-only.
- Guest audio is capped at 10 MiB and the declared recording duration is capped
  at 60 seconds. The preview endpoint requires an `Idempotency-Key`.
- `POST /api/v1/guest/previews` atomically attaches one ready audio asset and
  enqueues one durable `guest.preview` job.
- `GET /api/v1/guest/previews/{previewId}` returns only the state, transcript,
  zero to two corrections, safe failure information, and expiry. It never
  exposes a corrected transcript, full detector output, rewrite, or shadowing.
- Foreign preview and media identifiers return `404` rather than revealing
  ownership.

## Processing and isolation

Guest preview has its own worker pool and concurrency control. The worker
materializes the authorized media asset, transcribes it, and makes one bounded
AI request for the preview corrections. It never calls the full multipass
detectors, reviewer, natural rewrite, or Cartesia.

Every publish is fenced by the active processing job and preview state. A
retried or stale worker cannot overwrite a promoted preview, and a completed
preview is not billed again by a redelivered job.

## Promotion

Registration and login retain the existing principal merge transaction. In
that same transaction the backend:

1. transfers only the media attached to the eligible preview to the account;
2. creates the normal recording using the preview ID as the stable recording
   ID;
3. reuses a ready transcript by starting at the suggestions stage, otherwise
   starts at transcription;
4. enqueues one idempotent `recording.process` job;
5. removes temporary media retention and fences/cancels the guest job;
6. revokes the guest device session.

Preview corrections are deliberately not copied into the full recording. The
authenticated pipeline remains the only source of full analysis.

Promotion is also an account-level, one-time onboarding entitlement. It locks
the same user row as ordinary recording creation and reserves weekly free quota
before it creates the full-processing job. Authentication still succeeds when
the entitlement was already used or quota is exhausted; the token response
reports `guestPreviewPromotion.status=not_promoted` with a stable reason. Only
the asset attached to an eligible preview changes ownership. A guest upload
that never became a preview stays guest-owned and expires normally.

## Retention and capacity

Unpromoted guest data keeps a fixed expiry. Maintenance marks expired preview
media for durable deletion through the normal media cleanup queue. Promotion
removes that retention deadline.

The database enforces one live preview audio asset and one preview per guest,
including concurrent requests. The API rejects new preview work with a stable,
retryable capacity error when the configured queue bound is exhausted. Broader
distributed source-rate limiting, production metrics, tracing, and load/fault
testing belong to the following scale-and-operations epic.

## Verification

- migration and schema tests cover the additive tables, constraints, and job
  kind;
- auth integration tests cover atomic, idempotent promotion and transcript
  reuse;
- API tests cover guest-only access, one-preview bounds, ownership, expiry,
  idempotency, and response redaction;
- worker tests cover the two-correction cap, one-call preview path, fencing,
  retry, and terminal failure;
- OpenAPI tests inventory the new stable mobile routes and error codes;
- normal Go, race, OpenAPI, boundary, and workflow checks run without starting
  Docker or application services in this environment.
