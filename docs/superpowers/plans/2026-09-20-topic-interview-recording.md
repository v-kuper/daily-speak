# Topic Interview Recording Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a pre-generated topic interview with one-question navigation and an interactive useful-word ticker during recording.

**Architecture:** Expand the existing topic-guidance response to 17 ordered follow-ups and 16 words in one Ollama request. Keep navigation and ticker motion client-side in focused components, with pure helpers for deterministic tests and no AI calls during recording.

**Tech Stack:** Go HTTP API, Ollama JSON chat response, React 19, Redux Toolkit, TypeScript, CSS, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-20-topic-interview-recording-design.md`

## Global Constraints

- Apply the feature only to topic recordings; do not change Free Talk or Photo Description.
- Use exactly 17 generated follow-ups and 16 useful words/phrases, with the selected topic as interview question 1.
- Never call Ollama while recording is active.
- Abort superseded topic-guidance requests and ignore stale completion actions.
- Wait for guidance while it is loading, but preserve recording as a fallback if generation fails.
- Do not run builds or tests locally on the Mac; CI performs execution verification.

## Review Focus

- Empty or failed guidance still leaves the selected topic usable for recording.
- Previous/next navigation never leaves the valid question range.
- Clicking the final question does not wrap to the start.
- Dragging across repeated ticker content normalizes offsets in both directions.
- Reduced-motion users receive manual scrolling without automatic animation.

---

### Task 1: Expand the ordered interview contract

**Files:**
- Modify: `backend/internal/httpapi/ai_handlers.go`
- Create: `backend/internal/httpapi/ai_handlers_test.go`
- Modify: `src/store/slices/appSlice.ts`

**Interfaces:**
- Consumes: existing `GET /api/topic-guidance` query and response shape.
- Produces: `{ questions: string[17], words: string[16] }` in logical interview order.

- [ ] **Step 1: Add failing parser tests**

Create tests that pass a literal JSON payload with 17 numbered questions and 16 words, assert the first and last items remain ordered, and assert payloads with 16 questions or 15 words are rejected.

- [ ] **Step 2: Update the backend contract**

Set `topicGuidanceQuestionsCnt = 17` and `topicGuidanceWordsCnt = 16`. Require both full counts in `parseTopicGuidance`. Update `topicGuidancePrompt` to request a coherent answer-independent interview arc and an exact 17-question/16-word JSON payload.

- [ ] **Step 3: Align the frontend parser**

Set `MIN_TOPIC_GUIDANCE_QUESTIONS = 17`, add a 16-word minimum, reject incomplete responses, and slice the returned arrays to 17 and 16.

### Task 2: Add pure interview navigation and ticker math

**Files:**
- Create: `src/lib/interviewGuidance.ts`
- Create: `scripts/interview-guidance.test.mjs`
- Modify: `package.json`

**Interfaces:**
- Produces: `buildInterviewQuestions(topic, followUps)`, `moveInterviewQuestion(index, direction, count)`, and `normalizeTickerOffset(offset, cycleWidth)`.

- [ ] **Step 1: Add failing Node tests**

Test that the selected topic stays first, blank/duplicate follow-ups are removed without reordering, navigation clamps at both ends, and negative/overflow ticker positions wrap into `[0, cycleWidth)`.

- [ ] **Step 2: Implement minimal pure helpers**

Implement generic string normalization, index clamping, and modular offset normalization without React or browser dependencies.

- [ ] **Step 3: Add the test to `quality`**

Add `test:interview-guidance` and include it in the existing quality command.

### Task 3: Build the focused recording UI

**Files:**
- Create: `src/components/InterviewQuestionCard.tsx`
- Create: `src/components/GuidanceWordTicker.tsx`
- Modify: `src/components/SpeakScreen.tsx`
- Modify: `app/globals.css`

**Interfaces:**
- `InterviewQuestionCard({ topic, followUps })` renders one question, progress, and bounded navigation.
- `GuidanceWordTicker({ words })` renders a seamless, draggable, pausable ticker.

- [ ] **Step 1: Implement `InterviewQuestionCard` against the pure helpers**

Reset its local index when `topic` changes. Advance on card click, expose previous/next buttons, disable boundaries, and show completion copy on the last question.

- [ ] **Step 2: Implement `GuidanceWordTicker` against the pure helpers**

Duplicate the word track, animate with `requestAnimationFrame`, use elapsed time for stable speed, pause with pointer capture, manually drag by `clientX`, resume on release, support Space and arrow keys, and disable automatic motion under `prefers-reduced-motion`.

- [ ] **Step 3: Replace the recording question list**

In the topic-only `speakState === "recording"` branch, render the ticker above the recording card and the interview card below the timer. Leave Free Talk and Photo Description unchanged and omit empty guidance components.

- [ ] **Step 4: Add responsive styling**

Match existing panel, border, radius, accent, and typography tokens. Keep controls at least 44px on touch devices and ensure the ticker clips cleanly at narrow widths.

### Task 4: Static verification and handoff

**Files:**
- Review all files above.

- [ ] **Step 1: Format changed Go files**

Run `gofmt` only on modified Go files.

- [ ] **Step 2: Perform non-executing repository checks**

Run `git diff --check`, inspect `git status --short`, and review the final diff. Do not run local Mac builds or tests.

- [ ] **Step 3: Report CI verification requirements**

State that CI must run `npm run quality`, including Go parser tests and the new Node interview-guidance tests.
