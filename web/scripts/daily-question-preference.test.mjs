import assert from "node:assert/strict";
import test from "node:test";
import { configureStore } from "@reduxjs/toolkit";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const api = load("src/lib/apiClient.ts");
const preference = load("src/lib/dailyQuestionPreference.ts");
const app = load("src/store/slices/appSlice.ts");

test("a dislike saves one question and requests one replacement with the other cards as context", async (t) => {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (url, init) => {
    calls.push({ url: String(url), init });
    if (init.method === "POST") return new Response(null, { status: 204 });
    return Response.json({ questions: ["What makes a new place feel welcoming?"] });
  });
  api.configureApiClient("https://api.example.test");

  await preference.dismissDailyQuestion("What food do you like?");
  const replacement = await preference.fetchDailyQuestionReplacement({
    dateKey: "2026-09-30",
    refreshToken: "12345",
    interestIds: ["travel", "food", "music"],
    englishLevel: "B1",
    currentQuestions: ["What song helps you relax?", "What food do you enjoy?"],
    avoidQuestions: ["What food do you like?"],
  });

  assert.equal(replacement, "What makes a new place feel welcoming?");
  assert.equal(calls.length, 2);
  assert.equal(new URL(calls[0].url).pathname, "/api/v1/practice/daily-questions/dismiss");
  assert.deepEqual(JSON.parse(calls[0].init.body), { question: "What food do you like?" });
  const request = new URL(calls[1].url);
  assert.equal(request.pathname, "/api/v1/practice/daily-questions");
  assert.equal(request.searchParams.get("count"), "1");
  assert.deepEqual(request.searchParams.getAll("current"), ["What song helps you relax?", "What food do you enjoy?"]);
  assert.deepEqual(request.searchParams.getAll("avoid"), ["What food do you like?"]);
  assert.equal(request.searchParams.getAll("interest").length, 3);
});

test("replacing a disliked card preserves the other two questions and their order", () => {
  const original = { ...app.default(undefined, { type: "init" }), topics: ["First?", "Second?", "Third?"] };
  const hidden = app.default(original, app.hideDailyQuestion("Second?"));
  assert.deepEqual(hidden.topics, ["First?", "Third?"]);
  const replaced = app.default(hidden, app.insertDailyQuestion({ index: 1, question: "New second?" }));
  assert.deepEqual(replaced.topics, ["First?", "New second?", "Third?"]);
});

test("an incidental daily-question refresh does not replace the two kept cards", async (t) => {
  let fetchCount = 0;
  t.mock.method(globalThis, "fetch", async () => {
    fetchCount += 1;
    return Response.json({ questions: ["Unexpected?", "Other?", "Third?"] });
  });
  const initial = app.default(undefined, { type: "init" });
  const store = configureStore({
    reducer: { app: app.default },
    preloadedState: {
      app: { ...initial, topics: ["First?", "Third?"], questionsDate: "2026-09-30", questionsStatus: "ready" },
    },
  });
  await store.dispatch(app.fetchDailyQuestions({ dateKey: "2026-09-30", interestIds: ["travel"] }));
  assert.equal(fetchCount, 0);
  assert.deepEqual(store.getState().app.topics, ["First?", "Third?"]);
});
