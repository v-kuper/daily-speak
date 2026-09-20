# Cartesia Shadowing Audio Design

## Goal

Turn each recording's AI-corrected natural transcript into a reusable reference pronunciation. The recording details screen will present the corrected text and a dedicated audio player together so the learner can listen, repeat, and practice shadowing.

## Scope

- Generate reference audio automatically after the natural transcript is saved.
- Use Cartesia text-to-speech from the Go backend.
- Use one configured natural female American English voice for the first version.
- Persist the generated audio as a separate upload and return its URL with the recording.
- Keep shadowing generation independent from transcription, suggestions, and rewriting.
- Let the recording owner retry a failed shadowing generation.
- Remove the shadowing file when its recording is deleted.
- Extend the existing details screen rather than add a separate screen.

## Non-goals

- Do not expose a voice picker or per-user voice preference.
- Do not generate audio in the browser or expose the Cartesia API key to the client.
- Do not stream partial TTS output or add word-level timing in this version.
- Do not score the learner's shadowing attempt or record a second take from this block.
- Do not share the shadowing audio in Feed posts.
- Do not call the real Cartesia API from automated tests.

## User experience

The existing `A natural way to say it` section becomes a `Shadowing practice` card. This avoids duplicating the corrected transcript while keeping it separate from the original transcript, suggestions, and original-audio player.

The card contains:

- the guidance `Listen, then repeat with the same rhythm and pronunciation`;
- the complete corrected transcript;
- a dedicated audio player when reference audio is ready;
- `Creating pronunciation audio...` while generation is pending or active;
- a concise error and `Retry` button when generation fails.

The existing original-recording player remains unchanged and independent. Native accessible audio controls are sufficient for the first version and retain play, pause, seeking, volume, keyboard, and mobile-platform behavior.

## Recording data model

The `recordings` table gains four nullable or defaulted fields:

- `shadowing_status TEXT NOT NULL DEFAULT 'pending'` with application-level values `pending`, `processing`, `ready`, and `failed`;
- `shadowing_audio_url TEXT`;
- `shadowing_error TEXT`;
- `shadowing_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()` for guarded retry and stale-job recovery.

Old recordings migrate safely with `pending`. They are not synthesized in bulk. When the owner opens an old recording that already has a corrected transcript, the details screen schedules it once through the same endpoint used by Retry. This produces audio only for old recordings that the user actually revisits and avoids an unexpected one-time Cartesia bill during deployment.

The recording API model gains `shadowingStatus`, `shadowingAudioUrl`, `shadowingError`, and `shadowingUpdatedAt`. Backend and frontend normalization reject unknown statuses, malformed timestamps, and invalid upload URLs. The shadowing URL must be under the generic `/uploads/` namespace and may not be an arbitrary remote URL.

## Cartesia integration

A focused backend client owns the Cartesia request and is injected into the recording service so it can be replaced by a fake in tests. It calls `POST https://api.cartesia.ai/tts/bytes` with:

- the AI-corrected transcript;
- `sonic-3.6` by default;
- the configured voice ID;
- English language selection;
- normal speed and volume;
- MP3 output suitable for browser and mobile playback.

Configuration is server-only:

- `CARTESIA_API_KEY` is required to generate audio;
- `CARTESIA_VOICE_ID` selects the approved female American voice and is required;
- `CARTESIA_MODEL` defaults to `sonic-3.6`;
- `CARTESIA_API_URL` defaults to the Cartesia production endpoint and exists primarily for tests;
- `CARTESIA_API_VERSION` defaults to `2026-08-14`.

Missing credentials do not prevent the application from starting or the main recording analysis from completing. Shadowing moves to `failed` with a user-safe configuration message. Secrets, authorization headers, provider response bodies, and transcript contents are not written to logs.

## Processing flow

1. The existing background analysis transcribes the recording, creates suggestions, and generates the corrected transcript.
2. The server persists the corrected transcript and marks the main recording `ready` before starting TTS.
3. A separate shadowing worker atomically changes the shadowing status from `pending` or `failed` to `processing`. If another worker already owns it, the new request exits without creating a duplicate Cartesia call.
4. The worker sends the corrected transcript to Cartesia with a 90-second timeout.
5. The response is accepted only for a successful status, an audio content type, and a non-empty body no larger than 25 MB.
6. Audio is written to a temporary file under the uploads root and atomically renamed to `/uploads/shadowing/<sanitized-user-id>/<recording-id>.mp3`.
7. The database is updated to `ready` with the public URL and no error.
8. Any provider, validation, timeout, storage, or database failure removes temporary output and updates only the shadowing state to `failed` with a safe error.

The main recording remains `ready` even when shadowing fails. When a ready recording has a corrected transcript and shadowing is `pending`, the details screen sends one scheduling request. It continues polling while either the main recording or shadowing is processing, then stops when both reach a terminal state.

## Retry API and concurrency

`POST /api/recordings/{recordingID}/shadowing` is authenticated and owner-only. It returns the current recording immediately after scheduling generation.

The endpoint:

- returns `404` when the recording does not belong to the authenticated user;
- returns `409` if no corrected transcript is available yet;
- returns the existing recording without starting another job when shadowing is `ready` or was recently changed to `processing`;
- changes `failed` or `pending` to `processing` exactly once and starts background generation;
- reclaims `processing` only when `shadowing_updated_at` is more than five minutes old;
- never accepts transcript, voice, file path, or provider credentials from the client.

Automatic generation uses the same conditional database update as Retry. This keeps behavior idempotent across simultaneous requests and prevents repeated button presses or multiple app instances from spending duplicate Cartesia requests.

If the process exits during generation, a recording may retain `processing`. Once `shadowingUpdatedAt` is more than five minutes old, the details screen replaces the normal progress message with a delayed-job message and Retry; the endpoint may then reclaim the job. This first version does not introduce a durable job queue.

## Storage and deletion

Shadowing audio uses the existing uploads root and static uploads handler. File paths are derived only from sanitized server-owned identifiers. The client and Cartesia response cannot choose a destination path.

Recording deletion reads both `audio_data_url` and `shadowing_audio_url` in the existing transaction, adds both valid paths to `pending_file_deletions`, deletes database records, cancels active processing, and wakes the deletion worker. The same safe path validation used for other stored uploads applies to shadowing files.

Retry after an existing failed generation replaces only that recording's deterministic shadowing file. A successful ready result is reused and does not regenerate automatically.

## Error handling and observability

Provider errors are classified for logs without exposing provider payloads or credentials. Logs include recording ID, outcome, duration, HTTP status when available, and generated byte count. User-facing messages distinguish missing server configuration from a temporary generation failure but do not reveal secrets or internal filesystem details.

The Cartesia client applies a request timeout and response-size limit. A failure never clears the corrected transcript, suggestions, original audio, or main ready status.

## Testing

Go tests use an `httptest` Cartesia server and temporary upload directory to cover:

- request method, API version, bearer authorization, content type, model, voice, transcript, language, speed, and MP3 format;
- successful audio validation and atomic persistence;
- timeouts, non-2xx responses, non-audio responses, empty bodies, and oversized bodies;
- shadowing status transitions and preservation of the main ready status;
- authenticated owner-only Retry, idempotent repeated requests, and missing corrected transcript behavior;
- inclusion and normalization of shadowing fields in recording responses;
- deletion scheduling for both original and shadowing files.

Frontend and Node tests cover status parsing, polling decisions, progress/error labels, Retry request behavior, and rejection of invalid shadowing URLs. Typecheck, lint, backend tests, existing recording-flow tests, and API contract generation remain part of the quality gate.

After the key and voice ID are configured, one manual smoke test creates a recording, waits for the corrected transcript and MP3, plays both audio sources, reloads the details page to confirm persistence, and exercises Retry by temporarily using invalid credentials. No automated test spends Cartesia credits.

## Operational setup

The Docker service passes the five Cartesia variables into the Go backend without committing their values. A tracked `.env.example` contains empty placeholders; the real values go in the repository-root `.env`, which is already ignored and automatically read by Docker Compose. Documentation explains that the user obtains an API key and chooses the natural female American voice in Cartesia, then sets `CARTESIA_API_KEY` and `CARTESIA_VOICE_ID` before rebuilding or restarting the app.

The final handoff will provide the exact `.env` entries and restart commands. The API key is never requested in chat, committed to Git, or embedded in an APK/browser bundle.
