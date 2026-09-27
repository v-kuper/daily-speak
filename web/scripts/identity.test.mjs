import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const api = load("src/lib/apiClient.ts");
const identity = load("src/lib/identity.ts");

const grant = (accessToken = "access-1") => ({
  principal: { id: "user-1", type: "user" },
  user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" },
  session: { id: "session-1", platform: "web" },
  tokens: {
    tokenType: "Bearer",
    accessToken,
    accessTokenExpiresAt: "2099-01-01T00:15:00Z",
  },
});

const json = (body, status = 200) => new Response(JSON.stringify(body), {
  status,
  headers: { "Content-Type": "application/json" },
});

const browser = (t, handler) => {
  identity.forgetBrowserIdentity();
  t.after(() => identity.forgetBrowserIdentity());
  t.mock.method(globalThis, "fetch", handler);
  api.configureApiClient("https://api.example.test");
};

test("web login uses the v1 contract and keeps refresh credentials out of JavaScript", async (t) => {
  const calls = [];
  browser(t, async (url, init = {}) => {
    calls.push({ path: new URL(String(url)).pathname, init });
    if (calls.length === 1) return json(grant());
    return json({ ok: true });
  });

  const active = await identity.authenticateBrowserIdentity("signIn", "person@example.test", "password123");
  assert.equal(active.user.email, "person@example.test");
  assert.equal(calls[0].path, "/api/v1/auth/login");
  assert.equal(JSON.parse(calls[0].init.body).platform, "web");
  assert.equal("refreshToken" in active, false);

  await api.apiFetch("/api/user/data");
  assert.equal(calls[1].init.headers.Authorization, "Bearer access-1");
});

test("browser restoration rotates the HttpOnly refresh cookie without reading it", async (t) => {
  const calls = [];
  browser(t, async (url, init = {}) => {
    calls.push({ path: new URL(String(url)).pathname, init });
    return json(grant("restored-access"));
  });

  const restored = await identity.restoreBrowserIdentity();
  assert.equal(restored.accessToken, "restored-access");
  assert.equal(calls[0].path, "/api/v1/auth/refresh");
  assert.equal(calls[0].init.credentials, "include");
  assert.deepEqual(JSON.parse(calls[0].init.body), {});
});

test("a protected request refreshes once and retries with the rotated access token", async (t) => {
  let resourceCalls = 0;
  const authorizations = [];
  browser(t, async (url, init = {}) => {
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/login") return json(grant("access-1"));
    if (path === "/api/v1/auth/refresh") return json(grant("access-2"));
    resourceCalls += 1;
    authorizations.push(init.headers.Authorization);
    return resourceCalls === 1 ? json({ error: "Unauthorized" }, 401) : json({ ok: true });
  });

  await identity.authenticateBrowserIdentity("signIn", "person@example.test", "password123");
  const response = await api.apiFetch("/api/user/data");
  assert.equal(response.status, 200);
  assert.deepEqual(authorizations, ["Bearer access-1", "Bearer access-2"]);
});

test("late 401 responses reuse the already rotated access token", async (t) => {
  let refreshCalls = 0;
  let staleCalls = 0;
  let releaseLateResponse;
  const lateResponse = new Promise((resolve) => { releaseLateResponse = resolve; });
  browser(t, async (url, init = {}) => {
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/login") return json(grant("access-1"));
    if (path === "/api/v1/auth/refresh") {
      refreshCalls += 1;
      return json(grant("access-2"));
    }
    if (init.headers.Authorization === "Bearer access-1") {
      staleCalls += 1;
      if (staleCalls === 2) await lateResponse;
      return json({ error: "Unauthorized" }, 401);
    }
    return json({ ok: true });
  });

  await identity.authenticateBrowserIdentity("signIn", "person@example.test", "password123");
  const first = api.apiFetch("/api/user/data");
  const second = api.apiFetch("/api/user/subscription");
  assert.equal((await first).status, 200);
  releaseLateResponse();
  assert.equal((await second).status, 200);
  assert.equal(refreshCalls, 1);
});
