import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const InterviewQuestionCard = load("src/components/InterviewQuestionCard.tsx").default;

test("a live transcription timeout shows one nonblocking recording status", () => {
  const html = renderToStaticMarkup(InterviewQuestionCard({
    turns: [{
      seq: 2,
      question: "What is your favorite place in your city?",
      usefulWords: ["favorite", "peaceful"],
      askedAtMs: 1200,
      endedAtMs: null,
      provisionalTranscript: "",
      transcriptStatus: "pending",
    }],
    canAdvance: true,
    onNext() {},
    onListen() {},
    onToggleSpeechMuted() {},
    speechState: "idle",
    speechError: null,
    speechMuted: false,
    liveTranscriptionAvailable: false,
    liveTranscriptionIssue: "finalize_timeout",
    liveCaption: null,
    hasAnswerEvidence: true,
    boundaryPending: false,
  }));

  assert.equal((html.match(/interview-live-notice/g) ?? []).length, 1);
  assert.match(html, /Live captions paused while finishing the previous answer/);
  assert.match(html, /Recording continues; you can keep answering/);
  assert.doesNotMatch(html, /class="interview-question-advance"[^>]*disabled/);
});
