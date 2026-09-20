# Cartesia Shadowing Audio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate a persistent Cartesia MP3 from every AI-corrected recording transcript and present it with the corrected text in a retryable Shadowing practice card.

**Architecture:** A focused Go `tts` package owns the Cartesia HTTP contract. Recording analysis marks the primary result ready first, then an independently guarded shadowing job synthesizes and atomically stores MP3 audio; a protected idempotent endpoint schedules pending, failed, or stale work. Redux carries the independent shadowing state to a details-card player without coupling it to original-audio playback.

**Tech Stack:** Go 1.24, `net/http`, PostgreSQL/pgx, Next.js 15, React 19, TypeScript, Redux Toolkit, Node test runner, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-09-20-cartesia-shadowing-design.md`

## Global Constraints

- Cartesia credentials remain server-only and never enter JSON responses, browser code, logs, Git, or automated-test fixtures.
- Use one configured female American voice from `CARTESIA_VOICE_ID`; use `sonic-3.6`, language `en`, normal speed/volume, and MP3 output.
- Use Cartesia API version `2026-08-14`, a 90-second request timeout, and a 25 MB response limit.
- Main recording status becomes `ready` before TTS begins and remains usable if TTS fails.
- Shadowing states are exactly `pending`, `processing`, `ready`, and `failed`; processing older than five minutes is reclaimable.
- Old recordings synthesize only when their owner opens them; deployment must not launch a bulk backfill.
- Shadowing audio stays private to recording details and is not copied into Feed posts.
- Automated tests use local fakes and `httptest`; no test spends Cartesia credits.
- Do not add a durable queue, voice picker, pronunciation scoring, word timestamps, or second shadowing recording flow.

## Review Focus

- Missing or invalid Cartesia configuration: the shadowing job must fail safely while the main recording remains `ready` (Task 3 integration test).
- Simultaneous auto-schedule and Retry: the conditional database claim must cause exactly one provider call (Task 3 integration test).
- An old `pending` recording opened more than once: the client may request scheduling repeatedly, but the backend must synthesize only once (Tasks 3 and 4 tests).
- A server restart leaving `processing`: recent work must not duplicate, while work older than five minutes must be reclaimable (Task 3 integration test).
- Malicious provider content or stored URLs: non-audio/oversized responses and traversal paths must not produce a stored file or deletion outside uploads (Tasks 1 and 3 tests).

## File Structure

- Create `backend/internal/tts/cartesia.go`: Cartesia configuration, request/response validation, and the `Synthesizer` boundary.
- Create `backend/internal/tts/cartesia_test.go`: local HTTP tests for the provider contract and failure limits.
- Create `backend/internal/domain/domain_test.go`: strict shadowing upload URL normalization tests.
- Create `backend/internal/httpapi/shadowing.go`: database claiming, background generation, atomic file persistence, Retry handler, and cancellation.
- Create `backend/internal/httpapi/shadowing_test.go`: pure storage/status tests that do not need PostgreSQL.
- Create `backend/internal/httpapi/shadowing_integration_test.go`: owner, concurrency, stale-job, and failure tests behind `TEST_DATABASE_URL`.
- Create `src/lib/shadowing.ts`: pure status parsing and scheduling/polling/staleness decisions.
- Create `scripts/shadowing.test.mjs`: Node tests for frontend decisions and UI contract.
- Create `.env.example`: non-secret Cartesia placeholders.
- Modify the existing migration, recording read/write queries, response model, server wiring, deletion path, Redux model, details UI, CSS, API docs, Docker configuration, README, and package scripts listed in the tasks below.

---

### Task 1: Isolated Cartesia TTS Client

**Files:**
- Create: `backend/internal/tts/cartesia.go`
- Create: `backend/internal/tts/cartesia_test.go`

**Interfaces:**
- Consumes: environment values supplied by `ConfigFromEnv()` and a complete corrected English transcript.
- Produces: `type Synthesizer interface { Synthesize(context.Context, string) ([]byte, error) }`, `type Config`, `func ConfigFromEnv() Config`, and `func NewCartesia(config Config) Synthesizer`.

- [ ] **Step 1: Write failing tests for the exact Cartesia request**

Use an `httptest.Server`, capture the JSON request, return `Content-Type: audio/mpeg`, and assert this contract:

```go
func TestCartesiaSynthesizeSendsConfiguredRequest(t *testing.T) {
	var got cartesiaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { t.Fatalf("method = %s", r.Method) }
		if r.Header.Get("Authorization") != "Bearer sk_test" { t.Fatalf("authorization was not bearer token") }
		if r.Header.Get("Cartesia-Version") != "2026-08-14" { t.Fatalf("version = %q", r.Header.Get("Cartesia-Version")) }
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil { t.Fatal(err) }
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3-audio"))
	}))
	defer server.Close()

	client := NewCartesia(Config{
		APIKey: "sk_test", VoiceID: "female-us", Model: "sonic-3.6",
		APIURL: server.URL, APIVersion: "2026-08-14", HTTPClient: server.Client(),
	})
	audio, err := client.Synthesize(context.Background(), "I went to the store.")
	if err != nil || string(audio) != "ID3-audio" { t.Fatalf("audio=%q err=%v", audio, err) }
	if got.Transcript != "I went to the store." || got.ModelID != "sonic-3.6" || got.Voice != "female-us" { t.Fatalf("request=%#v", got) }
	if got.Language != "en" || got.OutputFormat.Container != "mp3" { t.Fatalf("request=%#v", got) }
	if got.GenerationConfig.Speed != 1 || got.GenerationConfig.Volume != 1 { t.Fatalf("request=%#v", got) }
}
```

- [ ] **Step 2: Add failing table tests for safety boundaries**

Cover these named cases with explicit assertions that returned audio is nil and the error contains only a safe category, never the response body or key:

```go
cases := []struct {
	name, apiKey, voiceID, contentType string
	status int
	body []byte
}{
	{"missing api key", "", "female-us", "audio/mpeg", 200, []byte("audio")},
	{"missing voice", "sk_test", "", "audio/mpeg", 200, []byte("audio")},
	{"provider unauthorized", "sk_test", "female-us", "application/json", 401, []byte(`{"error":"secret detail"}`)},
	{"non audio response", "sk_test", "female-us", "application/json", 200, []byte(`{"ok":true}`)},
	{"empty audio", "sk_test", "female-us", "audio/mpeg", 200, nil},
	{"oversized audio", "sk_test", "female-us", "audio/mpeg", 200, bytes.Repeat([]byte{'a'}, maxAudioBytes+1)},
}
```

Add a timeout test whose handler blocks until `r.Context().Done()` and whose client timeout is 20 ms.

- [ ] **Step 3: Run the package test and confirm the red state**

Run: `cd backend && go test ./internal/tts -run Cartesia -count=1`

Expected: FAIL because `Config`, `NewCartesia`, and the request types do not exist.

- [ ] **Step 4: Implement the minimal provider client**

Use these public types and defaults:

```go
const (
	defaultAPIURL     = "https://api.cartesia.ai/tts/bytes"
	defaultAPIVersion = "2026-08-14"
	defaultModel      = "sonic-3.6"
	maxAudioBytes     = 25 * 1024 * 1024
)

type Synthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}

type Config struct {
	APIKey, VoiceID, Model, APIURL, APIVersion string
	HTTPClient *http.Client
}

type cartesiaRequest struct {
	ModelID string `json:"model_id"`
	Transcript string `json:"transcript"`
	Voice string `json:"voice"`
	OutputFormat struct {
		Container string `json:"container"`
		SampleRate int `json:"sample_rate"`
		BitRate int `json:"bit_rate"`
	} `json:"output_format"`
	Language string `json:"language"`
	Normalization string `json:"normalization"`
	GenerationConfig struct {
		Volume float64 `json:"volume"`
		Speed float64 `json:"speed"`
	} `json:"generation_config"`
}
```

`ConfigFromEnv()` reads `CARTESIA_API_KEY`, `CARTESIA_VOICE_ID`, `CARTESIA_MODEL`, `CARTESIA_API_URL`, and `CARTESIA_API_VERSION`; defaults model, URL, and version, and uses an `http.Client{Timeout: 90 * time.Second}`. `Synthesize` trims and rejects empty transcripts, creates the request with `sample_rate: 44100` and `bit_rate: 128000`, accepts only 2xx plus `audio/*`, reads through `io.LimitReader(response.Body, maxAudioBytes+1)`, and returns safe sentinel-style messages such as `cartesia authentication failed` and `cartesia returned invalid audio`.

- [ ] **Step 5: Format and run the focused tests**

Run: `gofmt -w backend/internal/tts/cartesia.go backend/internal/tts/cartesia_test.go`

Run: `cd backend && go test ./internal/tts -count=1`

Expected: PASS, including the timeout and 25 MB boundary cases.

- [ ] **Step 6: Commit the provider boundary**

```bash
git add backend/internal/tts/cartesia.go backend/internal/tts/cartesia_test.go
git commit -m "feat: add Cartesia speech client"
```

---

### Task 2: Persist and Return Independent Shadowing State

**Files:**
- Modify: `backend/migrations/0001_init.sql`
- Modify: `backend/internal/db/migrations_test.go`
- Modify: `backend/internal/domain/domain.go`
- Create: `backend/internal/domain/domain_test.go`
- Modify: `backend/internal/httpapi/helpers.go`
- Modify: `backend/internal/httpapi/recordings_handlers.go`
- Modify: `backend/internal/httpapi/recording_sessions_handlers.go`
- Modify: `backend/internal/httpapi/user_handlers.go`
- Modify: `backend/internal/httpapi/recording_analysis_test.go`

**Interfaces:**
- Consumes: existing `recordings` rows and generic upload URL normalization.
- Produces: database columns `shadowing_status`, `shadowing_audio_url`, `shadowing_error`, `shadowing_updated_at`; JSON fields `shadowingStatus`, `shadowingAudioUrl`, `shadowingError`, `shadowingUpdatedAt`; `normalizeShadowingStatus(string) string`; `domain.NormalizeStoredShadowingAudioSource(string) *string`.

- [ ] **Step 1: Pin the migration and normalization behavior with failing tests**

Extend `TestInitialMigrationContainsCurrentTables` with these exact fragments:

```go
"ADD COLUMN IF NOT EXISTS shadowing_status TEXT NOT NULL DEFAULT 'pending'",
"ADD COLUMN IF NOT EXISTS shadowing_audio_url TEXT",
"ADD COLUMN IF NOT EXISTS shadowing_error TEXT",
"ADD COLUMN IF NOT EXISTS shadowing_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
```

Add normalization tests:

```go
func TestNormalizeShadowingStatus(t *testing.T) {
	for _, value := range []string{"pending", "processing", "ready", "failed"} {
		if got := normalizeShadowingStatus(value); got != value { t.Fatalf("%q => %q", value, got) }
	}
	if got := normalizeShadowingStatus("unexpected"); got != "pending" { t.Fatalf("unexpected => %q", got) }
}
```

Create `domain_test.go` with one accepted URL, `/uploads/shadowing/user-1/recording-1.mp3`, and rejected cases for a data URL, remote URL, non-MP3 extension, wrong uploads directory, extra segment, and `..` traversal.

- [ ] **Step 2: Run focused tests and confirm they fail**

Run: `cd backend && go test ./internal/db ./internal/httpapi -run 'InitialMigration|NormalizeShadowing' -count=1`

Expected: FAIL because the columns and normalizer do not exist.

- [ ] **Step 3: Add the migration and response fields**

Append these idempotent migration statements:

```sql
ALTER TABLE recordings ADD COLUMN IF NOT EXISTS shadowing_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE recordings ADD COLUMN IF NOT EXISTS shadowing_audio_url TEXT;
ALTER TABLE recordings ADD COLUMN IF NOT EXISTS shadowing_error TEXT;
ALTER TABLE recordings ADD COLUMN IF NOT EXISTS shadowing_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
```

Extend `recordingResponse` exactly as follows:

```go
ShadowingStatus    string  `json:"shadowingStatus"`
ShadowingAudioURL  *string `json:"shadowingAudioUrl"`
ShadowingError     *string `json:"shadowingError"`
ShadowingUpdatedAt string  `json:"shadowingUpdatedAt"`
```

Implement `normalizeShadowingStatus`, add a strict domain regex for `/uploads/shadowing/<safe-user-segment>/<safe-recording-segment>.mp3`, expose it through `NormalizeStoredShadowingAudioSource`, reuse `normalizeOptionalProcessingError` for the safe shadowing error, and format timestamps with `UTC().Format(time.RFC3339Nano)`. Shadowing normalization must not accept data URLs, remote URLs, other upload directories, or other extensions.

- [ ] **Step 4: Update every recording query and scanner explicitly**

Add the four columns, variables, scan destinations, and response assignments in these paths:

- `handleCreateRecording` INSERT `RETURNING` and response construction in `recordings_handlers.go`;
- recording-session finish INSERT `RETURNING` and response construction in `recording_sessions_handlers.go`;
- `recordingForUser` SELECT/scanner/response in `recording_sessions_handlers.go`;
- `/api/user/data` SELECT/scanner/response in `user_handlers.go`.

Keep the scan order identical to the SELECT order:

```sql
shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at
```

New records use database defaults and therefore return `pending`, a nil URL/error, and a valid timestamp.

- [ ] **Step 5: Run backend tests and fix only contract mismatches introduced here**

Run: `cd backend && go test ./internal/db ./internal/httpapi -count=1`

Expected: PASS; integration tests may skip only when `TEST_DATABASE_URL` is absent.

- [ ] **Step 6: Commit the persistence contract**

```bash
git add backend/migrations/0001_init.sql backend/internal/db/migrations_test.go backend/internal/domain/domain.go backend/internal/domain/domain_test.go backend/internal/httpapi/helpers.go backend/internal/httpapi/recordings_handlers.go backend/internal/httpapi/recording_sessions_handlers.go backend/internal/httpapi/user_handlers.go backend/internal/httpapi/recording_analysis_test.go
git commit -m "feat: persist shadowing audio state"
```

---

### Task 3: Idempotent Background Generation, Retry, Storage, and Deletion

**Files:**
- Create: `backend/internal/httpapi/shadowing.go`
- Create: `backend/internal/httpapi/shadowing_test.go`
- Create: `backend/internal/httpapi/shadowing_integration_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/contract_test.go`
- Modify: `backend/internal/httpapi/recording_processing.go`
- Modify: `backend/internal/httpapi/recording_deletion.go`
- Modify: `backend/internal/httpapi/recording_deletion_integration_test.go`
- Modify: `backend/internal/httpapi/uploads.go`
- Modify: `backend/internal/httpapi/uploads_test.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: `tts.Synthesizer`, the four database fields from Task 2, `recordingForUser`, uploads root, and authenticated user lookup.
- Produces: `POST /api/recordings/{recordingID}/shadowing`, `scheduleShadowing(context.Context, string, string) (recordingResponse, bool, error)`, atomic `/uploads/shadowing/<user>/<recording>.mp3` storage, and independent cancellation.

- [ ] **Step 1: Write failing storage and path-safety tests**

In `shadowing_test.go`, assert that `saveShadowingAudio("user-1", "recording-1", []byte("ID3"))` creates exactly:

```text
<UPLOADS_DIR>/shadowing/user-1/recording-1.mp3
/uploads/shadowing/user-1/recording-1.mp3
```

Assert file bytes equal `ID3`, no `.tmp-*` file remains, and an empty payload is rejected. Extend `uploads_test.go` to accept the valid shadowing URL while rejecting `/uploads/shadowing/../../secret` and any fourth path segment.

- [ ] **Step 2: Write failing route and integration tests**

Add the shadowing POST path to `TestUnauthorizedAPIContractWithoutCookie`.

In `shadowing_integration_test.go`, follow the existing `TEST_DATABASE_URL` setup and use this fake:

```go
type fakeSynthesizer struct {
	mu sync.Mutex
	calls int
	audio []byte
	err error
}
func (f *fakeSynthesizer) Synthesize(_ context.Context, _ string) ([]byte, error) {
	f.mu.Lock(); f.calls++; f.mu.Unlock()
	return f.audio, f.err
}
```

Create ready owner records and assert these named tests:

- `TestShadowingRetryRequiresOwner`: a different authenticated user receives 404 and fake call count remains zero.
- `TestShadowingRetryRequiresCorrectedTranscript`: owner receives 409 and state remains `pending`.
- `TestShadowingConcurrentRequestsClaimOnce`: send two POST requests concurrently, wait for terminal state, and assert fake call count is one.
- `TestShadowingRecentProcessingIsNotDuplicated`: set `processing` with `NOW()`, POST, and assert zero calls.
- `TestShadowingStaleProcessingCanBeReclaimed`: set `processing` with `NOW() - INTERVAL '6 minutes'`, POST, and assert one call plus `ready` URL.
- `TestShadowingFailurePreservesReadyRecording`: use a fake error, wait for `failed`, and assert main status is still `ready`, corrected text is unchanged, and user-safe error excludes fake internal text.

- [ ] **Step 3: Run focused tests and confirm the red state**

Run: `cd backend && go test ./internal/httpapi -run 'Shadowing|UnauthorizedAPIContract|StoredUploadPath' -count=1`

Expected: FAIL because the endpoint, storage helper, server dependency, and shadowing directory are absent.

- [ ] **Step 4: Wire the synthesizer and independent cancellation map**

Extend server configuration without affecting existing tests:

```go
type Config struct {
	DB *db.DB
	NextURL string
	Synthesizer tts.Synthesizer
}
```

`NewServer` uses `config.Synthesizer` when non-nil and otherwise constructs `tts.NewCartesia(tts.ConfigFromEnv())`. Add a dedicated `shadowingProcessingCancels map[string]context.CancelFunc` guarded by its own mutex so scheduling TTS cannot cancel the transcription job. `handleDeleteRecording` cancels both job types.

`backend/cmd/api/main.go` may rely on the `NewServer` default construction; it must not read or log the key itself.

- [ ] **Step 5: Implement atomic job claiming and the owner-only endpoint**

Place the POST routing case before the generic recording GET/DELETE cases. Parse exactly one recording ID followed by `/shadowing`; reject extra path segments with 404.

Use this claim condition so PostgreSQL is the concurrency authority:

```sql
UPDATE recordings
SET shadowing_status = 'processing',
    shadowing_error = NULL,
    shadowing_updated_at = NOW()
WHERE id = $1
  AND user_id = $2
  AND BTRIM(corrected_transcript) <> ''
  AND (
    shadowing_status IN ('pending', 'failed')
    OR (shadowing_status = 'processing' AND shadowing_updated_at < NOW() - INTERVAL '5 minutes')
  )
RETURNING corrected_transcript
```

If the update returns no row, load through `recordingForUser`: return 404 for no owned record, 409 for blank corrected text, and 200 with the existing record for `ready` or recent `processing`. If claimed, launch one background job and return the refreshed record with `processing`.

- [ ] **Step 6: Implement generation and atomic persistence**

`runShadowing` performs these operations in order:

```go
audio, err := s.synthesizer.Synthesize(ctx, correctedTranscript)
saved, err := saveShadowingAudio(userID, recordingID, audio)
_, err = s.db.Exec(ctx, `
  UPDATE recordings
  SET shadowing_status = 'ready', shadowing_audio_url = $2,
      shadowing_error = NULL, shadowing_updated_at = NOW()
  WHERE id = $1 AND user_id = $3 AND shadowing_status = 'processing'`,
  recordingID, saved.publicURL, userID)
```

`saveShadowingAudio` sanitizes both identifiers, makes the target directory, writes to `os.CreateTemp(targetDir, ".shadowing-*.tmp")`, closes it, applies `0644`, and uses `os.Rename` in the same directory. Any failure removes the temp file. If the final database update affects zero rows or fails, remove the newly saved file.

On failure, update only `shadowing_status`, `shadowing_error`, and `shadowing_updated_at`; use the fixed user message `Pronunciation audio could not be generated. Check the Cartesia configuration or try again.` and log only category, recording ID, duration, and safe status metadata. On success, additionally log the generated byte count; never log the transcript or audio bytes.

- [ ] **Step 7: Schedule automatically after rewriting**

In `processSavedRecording`, preserve the existing main update and return behavior, then schedule only after the update succeeds:

```go
if _, err := s.db.Exec(ctx, readySQL, recordingID, correctedTranscript); err != nil {
	return err
}
_, _, scheduleErr := s.scheduleShadowing(context.Background(), userID, recordingID)
if scheduleErr != nil {
	logger.Warn("shadowing.schedule_failed", map[string]any{"recordingId": recordingID})
}
return nil
```

The scheduling call starts its own bounded background context and must not turn the completed recording analysis back into `failed`.

- [ ] **Step 8: Include shadowing files in validated deletion**

Allow exactly the `recordings`, `feed-replies`, and `shadowing` top-level upload directories in `storedUploadPath`. Extend the deletion transaction to select both `audio_data_url` and `shadowing_audio_url`, enqueue both through the existing deduplicating `appendFileURL`, and extend the integration test with a real shadowing file plus `pending_file_deletions` and post-restart removal assertions.

- [ ] **Step 9: Format and run focused plus full backend tests**

Run: `gofmt -w backend/internal/tts/*.go backend/internal/httpapi/*.go backend/cmd/api/main.go`

Run: `cd backend && go test ./internal/httpapi ./internal/tts -count=1`

Run: `npm run backend:test`

Expected: PASS. With no `TEST_DATABASE_URL`, only the explicitly guarded PostgreSQL integration tests skip.

- [ ] **Step 10: Commit the backend workflow**

```bash
git add backend/internal/httpapi backend/cmd/api/main.go
git commit -m "feat: generate and store shadowing audio"
```

---

### Task 4: Redux Contract and Shadowing Practice Card

**Files:**
- Create: `src/lib/shadowing.ts`
- Create: `scripts/shadowing.test.mjs`
- Modify: `src/lib/data.ts`
- Modify: `src/store/slices/appSlice.ts`
- Modify: `src/components/DetailsScreen.tsx`
- Modify: `app/globals.css`
- Modify: `package.json`

**Interfaces:**
- Consumes: recording JSON fields and the protected POST endpoint from Tasks 2 and 3.
- Produces: `ShadowingStatus`, pure scheduling/polling helpers, `generateShadowingAudio` thunk, and the `Shadowing practice` card.

- [ ] **Step 1: Write failing pure frontend tests**

Export these helpers from `src/lib/shadowing.ts`:

```ts
export type ShadowingStatus = "pending" | "processing" | "ready" | "failed";
export const parseShadowingStatus: (value: unknown) => ShadowingStatus;
export const shouldScheduleShadowing: (args: {
  recordingStatus: "processing" | "ready" | "failed";
  correctedTranscript: string;
  shadowingStatus: ShadowingStatus;
  requestLoading: boolean;
}) => boolean;
export const shouldPollRecording: (recordingStatus: string, shadowingStatus: ShadowingStatus) => boolean;
export const isShadowingStale: (status: ShadowingStatus, updatedAt: string, nowMs?: number) => boolean;
export const shadowingProgressLabel: (status: ShadowingStatus, stale: boolean) => string;
```

In `scripts/shadowing.test.mjs`, transpile the TypeScript module like `recording-processing.test.mjs` and assert:

- known statuses parse unchanged and unknown values become `pending`;
- pending + ready main status + non-empty corrected text schedules exactly when no request is loading;
- main processing or shadow processing keeps polling;
- a timestamp at four minutes is recent and one at six minutes is stale;
- labels are `Creating pronunciation audio...` and `Pronunciation audio is taking longer than expected.`.

Also assert the details source contains `Shadowing practice`, the guidance copy, an `<audio` element bound to `shadowingAudioUrl`, and a Retry dispatch.

- [ ] **Step 2: Register and run the failing Node test**

Add `"test:shadowing": "node --test scripts/shadowing.test.mjs"` and include it in `quality` after `test:recording-processing`.

Run: `npm run test:shadowing`

Expected: FAIL because the module and UI contract do not exist.

- [ ] **Step 3: Extend the TypeScript recording model and parser**

Add to `Recording`:

```ts
shadowingStatus: ShadowingStatus;
shadowingAudioUrl: string | null;
shadowingError: string | null;
shadowingUpdatedAt: string;
```

Use `parseShadowingStatus`. Validate `shadowingAudioUrl` with a dedicated `^/uploads/shadowing/[a-z0-9_-]+/[a-z0-9_-]+\.mp3$` case-insensitive regex; do not accept data URLs, remote URLs, or another uploads directory. Accept `shadowingUpdatedAt` only when `new Date(value)` is valid; otherwise use the recording timestamp so stale calculations remain deterministic. Trim the error string or store null.

Initialize optimistic local recordings with `pending`, null URL/error, and the draft timestamp.

- [ ] **Step 4: Add the scheduling thunk and reducer state**

Add `shadowingRequestStatus: AuthStatus` and `shadowingRequestError: string | null` to `AppState`, initialize them, and clear them when authentication or current recording state is reset.

Implement:

```ts
export const generateShadowingAudio = createAsyncThunk<Recording, string, { rejectValue: string }>(
  "app/generateShadowingAudio",
  async (recordingId, { rejectWithValue }) => {
    try {
      const response = await fetch(`/api/recordings/${encodeURIComponent(recordingId)}/shadowing`, { method: "POST" });
      const payload = (await response.json().catch(() => null)) as { recording?: unknown; error?: string } | null;
      if (response.status === 401) return rejectWithValue("Unauthorized");
      if (!response.ok) return rejectWithValue(payload?.error ?? "Failed to generate pronunciation audio.");
      const recording = parseRecording(payload?.recording);
      if (!recording) return rejectWithValue("Invalid recording payload from server.");
      return recording;
    } catch {
      return rejectWithValue("Cannot connect to pronunciation service.");
    }
  }
);
```

Pending sets request loading and clears the request error. Fulfilled replaces/inserts the recording through the same logic as `fetchRecording` and returns the request state to idle. Rejected handles Unauthorized through `clearAuthenticatedState`; other errors become the Retry-card error.

- [ ] **Step 5: Convert the natural-version section into Shadowing practice**

In `DetailsScreen`:

- dispatch `generateShadowingAudio(recording.id)` once when `shouldScheduleShadowing` returns true;
- poll `fetchRecording` every three seconds while `shouldPollRecording` returns true;
- keep the original player state/ref untouched;
- render the corrected transcript inside the renamed card;
- render `<audio controls preload="metadata" src={recording.shadowingAudioUrl}>` only for `ready` plus a non-null URL;
- show the progress label for pending/processing;
- show the safe server/request error and Retry for `failed` or stale `processing`;
- disable Retry while `shadowingRequestStatus === "loading"`.

Use the exact visible copy:

```tsx
<div className="section-title">Shadowing practice</div>
<p className="shadowing-hint">Listen, then repeat with the same rhythm and pronunciation.</p>
```

The card must still show the corrected text if audio generation fails.

- [ ] **Step 6: Style the card without changing the original player**

Add `.shadowing-section` beside the existing transcript/suggestions/natural section selectors, plus `.shadowing-hint`, `.shadowing-audio`, and `.shadowing-actions`. Keep native audio controls at `width: 100%`, preserve focus outlines, and include the class in the flat-shadow responsive override. Do not restyle `.player` or reuse the global Redux playback state.

- [ ] **Step 7: Run frontend tests, typecheck, and lint**

Run: `npm run test:shadowing`

Run: `npm run typecheck`

Run: `npm run lint`

Expected: PASS with no hook dependency warning and no TypeScript widening of shadowing statuses.

- [ ] **Step 8: Commit the client experience**

```bash
git add src/lib/shadowing.ts src/lib/data.ts src/store/slices/appSlice.ts src/components/DetailsScreen.tsx app/globals.css scripts/shadowing.test.mjs package.json
git commit -m "feat: add shadowing practice player"
```

---

### Task 5: Configuration, API Documentation, and End-to-End Verification

**Files:**
- Create: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `README.md`
- Modify: `docs/api/openapi.json`
- Regenerate: `docs/api/openapi.spec.js`
- Modify: `scripts/api-docs.test.mjs`

**Interfaces:**
- Consumes: the endpoint, JSON model, and five environment variables implemented above.
- Produces: a safe setup path for the user, documented public contract, generated Swagger artifact, and final verification evidence.

- [ ] **Step 1: Add failing API documentation assertions**

Add `/api/recordings/{recordingId}/shadowing` to `documentedPaths` and route checks in `scripts/api-docs.test.mjs`. Add assertions that the `Recording` schema requires and defines:

```js
for (const field of ["shadowingStatus", "shadowingAudioUrl", "shadowingError", "shadowingUpdatedAt"]) {
  assert.ok(openapi.components.schemas.Recording.properties[field], `missing Recording.${field}`);
}
```

Run: `npm run test:api-docs`

Expected: FAIL because the new endpoint and fields are not documented.

- [ ] **Step 2: Add safe local and Docker configuration**

Create `.env.example` with no secret values:

```dotenv
CARTESIA_API_KEY=
CARTESIA_VOICE_ID=
CARTESIA_MODEL=sonic-3.6
CARTESIA_API_URL=https://api.cartesia.ai/tts/bytes
CARTESIA_API_VERSION=2026-08-14
```

Pass the same five names through the `app.environment` section in `docker-compose.yml` with empty/default substitutions. Do not read the ignored `.env.local`, print the key, or add a client-prefixed variable.

- [ ] **Step 3: Document the exact user setup**

Add a `Cartesia shadowing audio` README section that tells the user to create repository-root `.env` from `.env.example`, paste the key after `CARTESIA_API_KEY=`, paste the chosen natural female American voice UUID after `CARTESIA_VOICE_ID=`, and restart with:

```bash
docker compose up --build -d app postgres
docker compose logs -f app
```

Explain that `.env` is ignored, the key must not be posted in chat or committed, new recordings synthesize automatically, and old recordings synthesize when opened.

- [ ] **Step 4: Document and regenerate the API**

Add the POST path with cookie authentication and `200`, `401`, `404`, `409`, and `500` responses. Extend `Recording` with the four fields and enum `[pending, processing, ready, failed]`. Then run:

Run: `npm run build:api-docs`

Run: `npm run test:api-docs`

Expected: PASS and `docs/api/openapi.spec.js` exactly matches `openapi.json`.

- [ ] **Step 5: Run the complete automated quality gate**

Run: `npm run quality`

Expected: PASS for typecheck, lint, Go packages, all Node suites, API docs, and CI workflow checks. PostgreSQL integration tests may skip only if `TEST_DATABASE_URL` is not configured; if configured, they must pass.

- [ ] **Step 6: Run a local non-paid UI smoke check**

Start the application without Cartesia credentials, open a completed recording, and verify:

- corrected text remains visible;
- the new card reaches `failed` with safe configuration guidance;
- the original audio player still works;
- Retry is available and does not expose secrets in browser network responses.

Do not call the production Cartesia endpoint during this step.

- [ ] **Step 7: Commit configuration and documentation**

```bash
git add .env.example docker-compose.yml README.md docs/api/openapi.json docs/api/openapi.spec.js scripts/api-docs.test.mjs package.json
git commit -m "docs: configure Cartesia shadowing"
```

- [ ] **Step 8: Perform the paid smoke test only after the user adds credentials**

With the user-managed `.env` present, rebuild the app, create one short recording, and verify this sequence in the details screen:

```text
processing transcript -> corrected text ready -> shadowing processing -> MP3 player ready
```

Play the original and reference audio independently, reload and replay the persisted MP3, and confirm the server logs contain no key, authorization header, transcript, or provider body. If credentials are intentionally made invalid for Retry testing, restore the valid `.env` value immediately afterward without copying it into command output.
