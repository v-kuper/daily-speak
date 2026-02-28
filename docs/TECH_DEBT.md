# Technical Debt Audit

Last updated: 2026-03-01

## Scope
Audit of maintainability/readability risks for `app/` and `src/` with focus on:
- oversized files and mixed responsibilities
- code duplication and drift risk
- missing quality gates
- refactorability without behavior changes

## Snapshot Metrics
Top large files by LOC:
- `src/store/slices/appSlice.ts`: 2536
- `app/globals.css`: 1275
- `src/components/SpeakScreen.tsx`: 845
- `app/api/user/recordings/route.ts`: 643
- `src/server/whisper.ts`: 564

## Prioritized Findings

### P1 - Quality Gates
1. ESLint command is broken and cannot protect refactors.
- File: `eslint.config.mjs`
- Symptom: `npm run lint` fails because `eslint-config-next/core-web-vitals` import is unresolved in current setup.
- Impact: No static quality gate before merge.

2. No tests in repo (unit/integration/e2e).
- File: `package.json` (no `test` script), no `*.test.*` / `*.spec.*` files.
- Impact: high regression risk for auth/audio/AI/subscription flows.

### P2 - Structural Hotspots
3. `appSlice` is a god-file with mixed domains.
- File: `src/store/slices/appSlice.ts`
- Contains: domain constants, payload parsers, async thunks, reducers, UI state transitions.
- Impact: hard onboarding, risky changes, low locality.

4. `POST /api/user/recordings` route is overloaded.
- File: `app/api/user/recordings/route.ts`
- One file does validation, quota checks, file IO, Whisper call, Ollama call, DB persistence.
- Impact: hard to test, hard to reason about failures and rollback behavior.

5. Duplicated media validation/parsing logic.
- Files:
  - `app/api/user/recordings/route.ts`
  - `app/api/user/data/route.ts`
  - `src/store/slices/appSlice.ts`
- Repeated regex and normalization for audio/photo URLs.
- Impact: drift and inconsistent behavior client/server.

### P3 - Readability/Scalability
6. `SpeakScreen` mixes recording runtime, fetch orchestration, and multi-mode rendering.
- File: `src/components/SpeakScreen.tsx`
- Impact: difficult edits, accidental cross-mode regressions.

7. Global CSS monolith.
- File: `app/globals.css`
- Impact: low style locality, hard conflict resolution, fragile style evolution.

8. Ollama prompt/parse/retry pipeline duplicated across API routes.
- Files:
  - `app/api/daily-questions/route.ts`
  - `app/api/topic-guidance/route.ts`
  - `app/api/study-words/route.ts`
- Impact: repeated fixes and behavior drift between endpoints.

## Refactor Roadmap

### Phase 0 - Stabilize Tooling (required before large refactor)
- Fix ESLint config and migrate lint script to a working CLI flow.
- Add minimal CI checks: `tsc --noEmit` + lint.
- Add initial smoke tests for critical API routes.

Acceptance:
- `npm run lint` passes locally and in CI.
- CI blocks merge on lint/type failures.

### Phase 1 - Shared Validation/Parsing Modules
- Extract media validators/parsers to `src/lib/mediaValidation.ts`.
- Extract shared API parsing helpers for Ollama JSON extraction.

Acceptance:
- No duplicated audio/photo regex logic across client/server.
- All three locations consume shared helper(s).

### Phase 2 - Store Decomposition
Split `appSlice.ts` into feature slices:
- `sessionSlice` (auth/session/profile basics)
- `practiceSlice` (question/topic/study generation state)
- `recordingsSlice` (recording lifecycle/playback/history)
- `billingSlice` (quota/subscription)

Acceptance:
- each slice < 800 LOC (target)
- each async thunk colocated with its domain
- selectors exported per slice

### Phase 3 - Recording Route Service Layer
- Extract `recordings` route internals into services:
  - `recordingPayload.ts` (validation)
  - `recordingStorage.ts` (file save/delete)
  - `recordingAnalysis.ts` (Whisper + suggestions)
  - `recordingRepository.ts` (DB persistence)

Acceptance:
- route file focused on orchestration and HTTP mapping
- unit tests for each service module

### Phase 4 - UI Composition and Styles
- Split `SpeakScreen` by mode/components:
  - `SpeakIdleView`, `SpeakReadyView`, `SpeakRecordingView`, `SpeakRecordedView`
  - `useMicrophoneRecorder` hook for MediaRecorder state machine
- Move screen-scoped styles into CSS Modules per component.

Acceptance:
- `SpeakScreen.tsx` becomes thin composition shell
- mode-specific changes do not require touching all branches

## Suggested Execution Order (2-3 week safe path)
1. Phase 0
2. Phase 1
3. Phase 2
4. Phase 3
5. Phase 4

## Non-goals for this refactor wave
- No UX redesign.
- No schema changes unless required for testability.
- No behavior changes for quota/subscription rules.

## Risk Notes
- Any slice split without tests may silently break async flows.
- Recording pipeline changes must preserve failure cleanup (audio file rollback).
- Ollama/Whisper behavior is environment-sensitive; keep clear fallback/error messages.
