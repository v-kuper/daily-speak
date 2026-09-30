# Architecture

Last reviewed: 2026-09-29

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
  +-- Cartesia speech services and Ollama
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

- `/api/v1/*` is the only application contract. Mobile and web share identity,
  profile, practice, subscription, media, guest preview, and recording APIs.
- `/openapi.json` and `/docs` expose the backend-owned OpenAPI contract and
  Swagger UI.
- `/healthz` is process liveness; `/readyz` checks PostgreSQL and queue
  admission; `/metrics` is a protected operations endpoint.
- Recording media is available only through owner-protected media resources
  and short-lived signed requests; the API does not expose a public file tree.

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

A guest may submit one preview bounded to 180 seconds. The guest path performs
transcription and at most two high-confidence corrections; it does not run
full multipass analysis or TTS. Account promotion queues full processing while
reusing work that already succeeded. Every authenticated account may save up
to 600 seconds per recording, independent of subscription state, and has no
weekly recording quota.

The `weeklyLimitSeconds` and `weeklyRemainingSeconds` response fields remain for
rolling compatibility with older clients. They are fixed at 600 for
non-subscribers, null for subscribers, and never participate in recording
admission or decrement after a recording. `weeklyUsedSeconds` is informational.

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

Before upload, the web client stages a captured audio `Blob` in IndexedDB and
keeps only an opaque draft key in Redux. This browser copy supports local
playback, authentication handoff, and retry; it is deleted after successful
resource creation, while draft-storage access prunes abandoned blobs older than
seven days. IndexedDB is temporary client storage and is never a source of truth.

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

Speaking practice uses the learner's selected interest themes to generate three
opening questions. The chosen question becomes the first question of an
interview. The legacy topic-guidance contract can generate ten further
questions and eight useful words or short phrases for older clients. The web
client owns the selectable interest catalog and maps saved interest IDs to
theme names before calling the practice API. The API response counts are
documented in OpenAPI.

Daily question generation reads saved opening themes from finalized interview
sessions and legacy topic recordings, plus explicit question dismissals from
PostgreSQL. Interview follow-up turns do not enter this exclusion history.
Recent exclusions guide the prompt; all exclusions are checked before returning
a generated set. The web client remembers recently presented options during the
open page and can dismiss one suggestion permanently for that account. A
single-question request then fills only the dismissed card; the other two stay
visible. The prompt expands selected interests into relatable situations and
requires the three opening questions to cover different broad subjects.
Adaptive interview generation uses the English level stored on the
session as a hard CEFR ceiling. The same generation call may simplify below
that ceiling when the latest answer is short, fragmented, or disconnected; it
does not run a second model call to classify or validate difficulty.

Adaptive topic interviews use a separate `/api/v1/interviews` lifecycle. The
selected opening question starts a durable session. Preparation generates 6 to
10 level-appropriate English words or short phrases for that question, plus one
hidden next question with its own words. Each answered turn can generate one
contextual replacement from the answer transcript. If the reserve is empty after
a skip or failed generation, a durable refill generates one standalone question
and its words. A partial unique index enforces at most one unconsumed candidate
per session. The preparation screen shows no answer words; the recording screen
scrolls only the current question's words above the question. New words may be
nouns, verbs, adjectives, adverbs, connectors, or short helper phrases. The
legacy session-wide vocabulary fields remain in v1 responses for compatibility:
`usefulWords` mirrors the opening question's words, while new sessions leave
the translated `usefulVocabulary` list empty.
During an answer, the browser sends raw
PCM to Cartesia over a realtime WebSocket using a short-lived, STT-scoped token
issued by the API. The browser persists each final answer transcript through
the interview API and records one continuous audio file for playback. Presented
question text and answer boundaries remain session data rather than learner
speech. The browser streams approximately 100 ms PCM packets continuously,
flushes the remaining samples at each question boundary, and displays the
latest interim words as they arrive so longer answers do not hide new text
behind the two-line caption limit. The manual STT connection specifies English
and finalizes only at the explicit answer boundary so late provider deltas
remain attached to the correct question.
Displayed questions are pronounced automatically through Cartesia's bytes TTS
endpoint unless the learner mutes them. The API issues a separate short-lived,
TTS-only token for an owned, active interview. The browser fetches and caches
audio for the one hidden next question before it is displayed, then plays it
when that question becomes visible. The opening question may incur an initial
fetch delay. Playback temporarily pauses microphone capture so synthesized
speech does not enter the learner's answer, and the long-lived provider key
stays server-only. Pronunciation does not add work to the durable interview
worker.
The learner may move past an unanswered question or stop on it. The interview
repository marks that turn as skipped in the same state transition that opens
the next question, or through the final-turn skip endpoint when recording
stops. Skipped turns remain internal sequence history for idempotency and
question de-duplication, but are excluded from the visible timeline, final
dialogue, analysis, and shadowing. Advancing still consumes a prefetched
candidate and adds no extra model call to the live path.
The existing topic-guidance contract remains available to older clients.

The final recording or guest preview is created from the continuous audio when
the learner saves the interview. Its canonical text is composed from the stored
per-turn transcripts in sequence, so final analysis does not transcribe the
continuous audio again. The composed question-and-answer dialogue gives the AI
the interview context while the transcript field remains learner speech only.
If a turn has no realtime transcript, its uploaded answer audio can still use
the durable worker and Cartesia's batch endpoint before finalization.

The rewrite step returns one corrected learner answer for every stored turn in
the same sequence. The repository stores those answers on the turns and builds
the corrected interview transcript from the immutable questions and corrected
answers in chronological order. Shadowing speaks that complete dialogue without
synthetic role labels. Free-talk and photo-description corrected transcripts
continue to contain learner speech only.

Full recording analysis is owned by `internal/recording`. Its application
service runs seven category detectors and a reviewer through the provider port.
It skips the language-switch call when no Cyrillic learner text is present and
skips review when no candidates exist. Successful detector passes are persisted
under the worker's active lease and reused on retry only when the analysis input
and pipeline version match. Checkpoints are internal repository data.

Every new correction and strength has a stable ID and an exact evidence span.
Spans use half-open UTF-16 offsets; for interviews they refer to one learner
answer identified by its stored sequence. Models identify an exact phrase and
one-based occurrence, and the recording service resolves and validates its
location. It rejects phrases spanning answers and ambiguous locations. Conflict
resolution compares actual ranges, prioritizes mandatory language translations
and more severe errors, and gives corrections precedence over overlapping
strengths. Server-owned learning references remain the rule source.

Saving corrections advances to rewriting and atomically queues an independent
`recording.strengths` job. It shares the bounded recording worker pool and has
its own two-minute timeout, retries, active-job fencing, and terminal status.
A failed strength job never fails the recording. The owner may retry it through
`POST /api/v1/recordings/{recordingId}/strengths` after correction analysis is
complete. Repeated calls while processing or ready are no-ops. Deletion cancels
both recording jobs; retrying transcription or correction analysis cancels
obsolete positive-feedback work. API and worker composition selects concrete
adapters; HTTP only authenticates, maps feature errors, and serializes results.

The additive v1 `id` and `span` feedback fields and `strengthsStatus` recording
field preserve older rows and clients. `unknown` represents legacy coverage;
`ready` with an empty array is a completed search with no selected examples.
The web client renders exact anchored spans and uses a conservative exact,
whole-word, unique-match fallback for old feedback. It keeps ambiguous legacy
cards without guessing locations. Request generations prevent late reads from
overwriting mutation results. The natural practice version can rephrase learner
speech and is labeled separately from the correction explanations.

Free-talk recordings and non-interview guest previews use Cartesia's batch
transcription API. Audio remains in backend-owned storage; the worker sends a
temporary materialized copy and deletes it after processing. The long-lived
Cartesia key remains server-only. Batch provider adapters may change without
changing the interview API or transcript composition rules.

Realtime interview STT deliberately crosses the browser boundary to avoid an
extra streaming hop: the API authorizes the interview and returns a short-lived
STT-scoped credential together with the provider connection parameters, while
the browser speaks the configured provider's WebSocket protocol. Realtime
connection mapping lives in `interview/cartesiaadapter`, the shared Cartesia
HTTP client lives in `transcription`, and `internal/app` only supplies
configuration and wires the adapter to the provider-neutral interview port.
The web provider protocol is isolated in `cartesiaRealtime.ts` behind the
provider-neutral `liveTranscription.ts` boundary consumed by screens. Replacing
this direct realtime provider requires coordinated backend and web adapter
changes, but it does not change screen orchestration, interview state,
persistence, or final processing.

Every account recording is duration-probed by the recording worker before
transcription. The repository verifies the measured duration against the
600-second per-recording account limit and stores the measured value.
Interview workers also replace the last question boundary with the measured
full-audio end. Guest previews are independently probed against their
180-second limit. This keeps free talk and topic recordings on the same
server-owned identity policy without relying on client-declared duration.

```text
web/                         standalone Next.js application
backend/cmd/api              API process entrypoint and lifecycle
backend/cmd/worker           durable worker entrypoint and lifecycle
backend/internal/app         API and worker dependency composition root
backend/internal/aiparse     provider-neutral model-output normalization
backend/internal/auth        unified web/mobile identity and token lifecycle
backend/internal/db          PostgreSQL connection and migrations
backend/internal/httpapi     HTTP transport, authorization gates, response mapping
backend/internal/interview  adaptive interview sessions, turns, and question policy
backend/internal/interview/cartesiaadapter  Cartesia realtime credential and connection mapping
backend/internal/learner     shared learner level and interest vocabulary
backend/internal/media       authorized media lifecycle
backend/internal/practice    speaking-practice generation application service
backend/internal/practice/ollamaadapter  Ollama adapter for the practice port
backend/internal/quota       recording duration policy, usage reporting, and formatting
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
