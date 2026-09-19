# Topic Interview Recording Design

## Goal

Make topic-based recording feel like a guided interview without making any Ollama calls while the microphone is active.

## Scope

- Applies only to topic recordings selected from daily questions or a custom topic.
- Free Talk and Photo Description keep their current recording interfaces.
- The selected topic question is the first interview question.
- One pre-recording `/api/topic-guidance` request returns 17 ordered follow-up questions and 16 useful words or short phrases, producing 18 interview steps in total.

## Interview generation

The Ollama prompt must ask for one coherent interview sequence rather than unrelated prompts. The sequence progresses through: opening context, personal experience, concrete details, reasons, comparison, consequences, a hypothetical situation, practical advice, reflection, and conclusion. Questions must not assume a specific answer, because they are generated before recording starts.

The existing JSON response remains `{ "questions": string[], "words": string[] }`; only the required counts and semantic prompt change. Parsing rejects incomplete responses so the existing retry loop can request a valid result.

## Recording interface

During topic recording:

- A continuous horizontal ticker at the top contains the 16 useful words or phrases.
- The ticker moves slowly and continuously.
- Holding it pauses movement, horizontal dragging moves it in either direction, and releasing resumes from the new position.
- Users with reduced-motion enabled get a stationary but manually draggable ticker.
- The current interview question appears alone in a large central card.
- Clicking the card advances to the next question.
- Previous and next buttons allow corrections after accidental navigation.
- Progress is shown as `current / 18` with a subtle progress bar.
- On the final question, forward navigation is disabled and the interface says the interview is complete; the user may continue speaking or stop.

The pre-recording guidance preview may continue showing the generated question and vocabulary lists. Only the active recording screen changes from a full list to the focused interview view.

## State and failure behavior

The current question index is local recording-screen state. It resets when the selected topic changes or a new topic recording begins. No interview navigation is persisted to the database.

While topic guidance is loading, the start button waits for the pre-generated interview. If generation fails, recording remains available as a fallback: the selected topic is shown as the sole question, and the ticker is omitted when there are no words.

Switching topics, navigating away, regenerating, or unmounting the screen aborts the superseded guidance request. Redux also matches completion actions by request ID, so a late response cannot replace the current interview or enable recording early.

Microphone and upload-session startup is treated as part of recording: topic regeneration is blocked synchronously from the Start click until startup succeeds or fails, preventing a new Ollama request from overlapping microphone activation.

## Components

- `InterviewQuestionCard`: builds the selected topic plus follow-ups, owns accessible previous/next controls, and reports index changes.
- `GuidanceWordTicker`: owns animation, seamless repetition, pointer capture, drag normalization, reduced-motion behavior, and keyboard pause/scroll controls.
- `interviewGuidance.ts`: pure sequence and offset helpers used by UI and Node tests.

## Verification

- Go tests pin expanded topic-guidance parsing and reject incomplete interview payloads.
- Node tests pin question ordering, navigation boundaries, and ticker offset wrapping.
- Existing typecheck, lint, backend tests, and quality gates run in CI; no local Mac build is required.
