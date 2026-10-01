import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";
const load = createTypeScriptLoader();
const { ActivityClock, activityIntensity, activityTimeLabel, activityMinutesLabel, heatmapWeeks, parseActivitySummary } = load("src/lib/activity.ts");

const setup = () => {
  const intervals = [];
  const clock = new ActivityClock(0, (kind, from, to) => intervals.push({ kind, from, to }));
  const total = kind => intervals.filter(item => item.kind === kind).reduce((sum, item) => sum + item.to - item.from, 0);
  return { clock, intervals, total };
};

test("feedback pauses at exactly two idle minutes and resumes only on interaction", () => {
  const { clock, total } = setup();
  clock.start("feedback", "review", 0);
  for (let time = 15_000; time <= 150_000; time += 15_000) clock.settle(time);
  assert.equal(total("review"), 120_000);
  clock.interact(155_000); clock.settle(170_000);
  assert.equal(total("review"), 135_000);
});

test("an interaction before the timeout keeps review active without splitting every movement", () => {
  const { clock, total, intervals } = setup(); clock.start("feedback", "review", 0);
  for (let time = 15_000; time <= 180_000; time += 15_000) {
    if (time === 105_000) clock.interact(time);
    clock.settle(time);
  }
  assert.equal(total("review"), 180_000); assert.equal(intervals.length, 12);
});

test("recording a repetition earns speaking time without also earning review time", () => {
  const { clock, total } = setup(); clock.start("feedback", "review", 0);
  clock.start("mic", "speaking", 10_000); clock.start("question", "speaking", 15_000);
  clock.stop("mic", 20_000); clock.stop("question", 25_000); clock.stop("feedback", 30_000);
  assert.equal(total("speaking"), 15_000); assert.equal(total("review"), 15_000);
});

test("hidden pages, suspended timers and stopped microphones earn no unattended time", () => {
  const { clock, total } = setup(); clock.start("mic", "speaking", 0);
  clock.setVisible(false, 10_000); clock.settle(25_000); clock.setVisible(true, 50_000);
  clock.settle(65_000); clock.settle(200_000); clock.stop("mic", 205_000); clock.settle(220_000);
  assert.equal(total("speaking"), 30_000);
});

test("returning to a hidden review does not renew its idle deadline", () => {
  const { clock, total } = setup(); clock.start("feedback", "review", 0);
  clock.setVisible(false, 15_000); clock.setVisible(true, 150_000); clock.settle(165_000);
  assert.equal(total("review"), 15_000);
  clock.interact(170_000); clock.settle(185_000); assert.equal(total("review"), 30_000);
});

test("intensity preserves short contributions and the exact five and fifteen minute boundaries", () => {
  assert.deepEqual([0, 1, 60_000, 300_000, 300_001, 899_999, 900_000].map(activityIntensity), [0, 1, 1, 1, 2, 2, 3]);
  assert.equal(activityTimeLabel(3_660_000), "1 ч. 1 мин."); assert.equal(activityMinutesLabel(1000), "меньше минуты");
});

test("calendars align Monday, include leap day, and never include future days", () => {
  const start = new Date("2024-02-28T12:00:00Z");
  const days = Array.from({ length: 366 }, (_, index) => ({ date: new Date(start.getTime() + index * 86400000).toISOString().slice(0, 10), speakingMilliseconds: 0, reviewMilliseconds: 0, level: 0 }));
  const desktop = heatmapWeeks(days);
  assert.equal(desktop.flat().filter(Boolean).length, 366);
  assert.equal(desktop[0][0], null); assert.equal(desktop[0][2].date, "2024-02-28"); assert.equal(desktop[0][3].date, "2024-02-29");
  const mobile = heatmapWeeks(days, true); assert.equal(mobile.length, 24);
  assert.equal(new Date(`${mobile[0][0].date}T12:00:00Z`).getUTCDay(), 1);
  assert.equal(mobile.flat().filter(Boolean).at(-1).date, days.at(-1).date);
});

test("malformed or unsorted API statistics fail visibly instead of fabricating empty activity", () => {
  const day = { date: "2026-10-01", speakingMilliseconds: 1000, reviewMilliseconds: 0, level: 1 };
  const summary = { totalSpeakingMilliseconds: 1000, historicalSpeakingMilliseconds: 0, timezone: "UTC", from: day.date, to: day.date, days: [day] };
  assert.equal(parseActivitySummary(summary), summary);
  for (const invalid of [{ ...summary, totalSpeakingMilliseconds: -1 }, { ...summary, days: [day, day] }, { ...summary, days: [{ ...day, level: 3 }] }, { ...summary, days: [] }]) assert.throws(() => parseActivitySummary(invalid));
});

test("the account outbox survives a failed send, serializes retries and never sends another account's data", async () => {
  const stored = new Map();
  const previousStorage = globalThis.localStorage, previousFetch = globalThis.fetch;
  globalThis.localStorage = {
    get length() { return stored.size; }, key: index => [...stored.keys()][index] ?? null,
    getItem: key => stored.get(key) ?? null, setItem: (key, value) => stored.set(key, value), removeItem: key => stored.delete(key),
  };
  let status = 503; const writes = [];
  globalThis.fetch = async (url, init) => {
    if (url.endsWith("/auth/login")) return new Response(JSON.stringify({ principal: { id: "account-A", type: "user" }, user: { email: "activity@test", isSubscriber: false, englishLevel: "b1" }, tokens: { tokenType: "Bearer", accessToken: "test-token-A", accessTokenExpiresAt: "2099-01-01T00:00:00Z" } }));
    writes.push({ body: JSON.parse(init.body), headers: init.headers });
    return new Response(JSON.stringify({ accepted: true }), { status });
  };
  try {
    const { configureApiClient } = load("src/lib/apiClient.ts"); configureApiClient("https://test.example");
    const { authenticateBrowserIdentity, forgetBrowserIdentity } = load("src/lib/identity.ts");
    await authenticateBrowserIdentity("signIn", "activity@test", "test-password");
    const interval = { id: "outbox-test-1234", kind: "speaking", startedAt: "2026-10-01T12:00:00Z", endedAt: "2026-10-01T12:00:15Z" };
    stored.set("daily-speaking-activity-v1:account-A:outbox-test-1234", JSON.stringify(interval));
    stored.set("daily-speaking-activity-v1:account-B:other-event", JSON.stringify({ ...interval, id: "other-event" }));
    const { flushActivity } = load("src/lib/activityTracking.ts");
    await assert.rejects(flushActivity()); assert.equal(stored.size, 2);
    status = 200;
    await Promise.all([flushActivity(), flushActivity()]);
    assert.equal(writes.length, 2); assert.deepEqual(writes[0].body, writes[1].body);
    assert.equal(writes[1].headers.Authorization, "Bearer test-token-A");
    assert.deepEqual([...stored.keys()], ["daily-speaking-activity-v1:account-B:other-event"]);
    forgetBrowserIdentity(); await flushActivity(); assert.equal(writes.length, 2);
  } finally { globalThis.localStorage = previousStorage; globalThis.fetch = previousFetch; }
});
