# Chunked Async Recordings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make recording upload non-blocking by streaming audio chunks to the backend while recording, finalizing into a persisted recording, and processing transcription/suggestions asynchronously.

**Architecture:** The backend owns recording sessions, chunk storage, final audio assembly, recording status, and background processing. The browser and future iOS app act as clients: create a session, upload ordered chunks, finish, poll recording status, and render processing/ready/failed states.

**Tech Stack:** Go HTTP API, PostgreSQL migrations, local filesystem uploads, ffmpeg/Whisper/Ollama, Redux Toolkit, MediaRecorder.

---

### Task 1: Backend Contract And Persistence

**Files:**
- Modify: `backend/migrations/0001_init.sql`
- Modify: `backend/internal/httpapi/helpers.go`
- Modify: `backend/internal/httpapi/server.go`
- Create: `backend/internal/httpapi/recording_sessions_handlers.go`
- Test: `backend/internal/httpapi/recording_sessions_test.go`

- [ ] Add `recording_upload_sessions` table with session metadata and status.
- [ ] Add status fields to `recordings`: `status`, nullable `transcript`, `suggestions`, and `processing_error`.
- [ ] Add response fields so recordings can be `processing`, `ready`, or `failed`.
- [ ] Add endpoints:
  - `POST /api/recording-sessions`
  - `POST /api/recording-sessions/:id/chunks`
  - `POST /api/recording-sessions/:id/finish`
  - `GET /api/recordings/:id`

### Task 2: Chunk Storage And Finalization

**Files:**
- Modify: `backend/internal/httpapi/recording_sessions_handlers.go`
- Modify: `backend/internal/httpapi/uploads.go`
- Test: `backend/internal/httpapi/recording_sessions_test.go`

- [ ] Store chunks under `UPLOADS_DIR/tmp/recording-sessions/<session-id>/<index>.<ext>`.
- [ ] Accept `multipart/form-data` chunk uploads with `chunkIndex` and `audio`.
- [ ] Finalize by concatenating chunks into `UPLOADS_DIR/recordings/<user-id>/<recording-id>.<ext>`.
- [ ] Create a `recordings` row immediately with `status='processing'`.

### Task 3: Async Processing

**Files:**
- Modify: `backend/internal/httpapi/recordings_handlers.go`
- Create: `backend/internal/httpapi/recording_processing.go`
- Test: `backend/internal/httpapi/recording_processing_test.go`

- [ ] Extract the existing Whisper/Ollama processing into a reusable method.
- [ ] Run processing in a goroutine after finalize.
- [ ] Update recording to `ready` with transcript/suggestions, or `failed` with an error message.

### Task 4: Browser Client

**Files:**
- Modify: `src/components/SpeakScreen.tsx`
- Modify: `src/store/slices/appSlice.ts`
- Modify: `src/lib/data.ts`
- Modify: `src/components/DetailsScreen.tsx`

- [ ] Start upload session when recording starts.
- [ ] Use `MediaRecorder.start(5000)` so chunks arrive every five seconds.
- [ ] Upload each chunk sequentially with retry-friendly ordering.
- [ ] On Save, call finish and open Details immediately.
- [ ] Details polls `GET /api/recordings/:id` while status is `processing`.

### Task 5: Verification

**Files:**
- Modify: `scripts/*test.mjs` only if contracts/docs need coverage.

- [ ] Run focused Go tests for recording sessions.
- [ ] Run TypeScript typecheck.
- [ ] Run `npm run quality`.
