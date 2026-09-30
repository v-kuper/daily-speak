import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const InterviewQuestionCard = load("src/components/InterviewQuestionCard.tsx").default;

test("Listen selects the visible turn's audio even when question text is repeated", () => {
  const turns = [
    { seq: 1, questionIndex: 1, question: "Why?", askedAtMs: 0, endedAtMs: 1000, provisionalTranscript: "Because.", usefulWords: [] },
    { seq: 2, questionIndex: 7, question: "Why?", askedAtMs: 1000, endedAtMs: null, provisionalTranscript: "", usefulWords: [] },
  ];
  let selected;
  const tree = InterviewQuestionCard({
    turns, canAdvance: true, onNext() {}, onListen(turn) { selected = turn; }, onToggleSpeechMuted() {},
    speechState: "idle", speechError: null, speechMuted: false, liveTranscriptionAvailable: true,
    liveTranscriptionIssue: null, liveCaption: null, hasAnswerEvidence: false, boundaryPending: false,
  });
  const findListen = element => {
    if (!element || typeof element !== "object") return null;
    if (element.props?.className === "interview-question-listen") return element;
    return [element.props?.children].flat().map(findListen).find(Boolean) ?? null;
  };
  const listen = findListen(tree);
  assert.ok(listen);
  listen.props.onClick();
  assert.equal(selected, turns[1]);
  assert.equal(selected.questionIndex, 7);
});

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
