#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const DEFAULT_WEB_BASE_URL = "http://localhost:3218";
const DEFAULT_API_BASE_URL = "http://localhost:3219";
const STARTUP_TIMEOUT_MS = 120_000;
const POLL_INTERVAL_MS = 1_500;
const REQUEST_TIMEOUT_MS = 15_000;
const MAX_FAILURE_DETAIL_LENGTH = 240;

function normalizeBaseURL(value, label) {
  const normalized = value.trim().replace(/\/+$/, "");
  let parsed;
  try {
    parsed = new URL(normalized);
  } catch {
    throw new Error(`${label} must be an absolute http or https URL.`);
  }
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.username || parsed.password) {
    throw new Error(`${label} must be an absolute http or https URL without credentials.`);
  }
  return normalized;
}

export function selectStackURLs(env = process.env) {
  const webBaseURL = normalizeBaseURL(
    env.WEB_BASE_URL?.trim() || DEFAULT_WEB_BASE_URL,
    "WEB_BASE_URL",
  );
  const apiBaseURL = normalizeBaseURL(
    env.API_BASE_URL?.trim() || DEFAULT_API_BASE_URL,
    "API_BASE_URL",
  );
  const configuredWebRedirectBaseURL = env.EXPECTED_WEB_REDIRECT_BASE_URL?.trim();
  return {
    webBaseURL,
    apiBaseURL,
    webOrigin: new URL(webBaseURL).origin,
    expectedWebRedirectBaseURL: configuredWebRedirectBaseURL
      ? normalizeBaseURL(configuredWebRedirectBaseURL, "EXPECTED_WEB_REDIRECT_BASE_URL")
      : null,
  };
}

export function resolveApiUploadURL(value, apiBaseURL) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error("Recording response did not include an audioDataUrl.");
  }
  const apiURL = new URL(apiBaseURL);
  const uploadURL = new URL(value, `${apiURL.origin}/`);
  if (uploadURL.origin !== apiURL.origin) {
    throw new Error("Recording audioDataUrl must belong to the API origin.");
  }
  return uploadURL.href;
}

function splitCombinedSetCookie(value) {
  return value.split(/,\s*(?=[^=;,]+=[^;,]+)/);
}

export function extractCookieHeader(headersOrResponse) {
  const headers = headersOrResponse?.headers ?? headersOrResponse;
  if (!headers) return "";

  const setCookies = typeof headers.getSetCookie === "function"
    ? headers.getSetCookie()
    : splitCombinedSetCookie(headers.get?.("set-cookie") ?? "");

  return setCookies
    .map((cookie) => cookie.split(";", 1)[0]?.trim())
    .filter(Boolean)
    .join("; ");
}

function endpoint(baseURL, pathname) {
  return new URL(pathname, `${baseURL}/`).href;
}

function safeExcerpt(body) {
  return body.replace(/\s+/g, " ").trim().slice(0, 300);
}

function safeFailureDetail(error) {
  const parts = [];
  const seen = new Set();
  let current = error;

  while (current != null && parts.length < 3 && !seen.has(current)) {
    if (typeof current === "object" || typeof current === "function") {
      seen.add(current);
    }
    if (current instanceof Error) {
      parts.push(`${current.name || "Error"}: ${current.message || "No message supplied."}`);
      current = current.cause;
    } else {
      parts.push(String(current));
      break;
    }
  }

  let detail = parts.join("; caused by ") || "request failed without an error description";
  detail = detail.replace(/(https?:\/\/)[^/\s@]+@/gi, "$1");
  detail = detail.replace(/(https?:\/\/[^\s?#]+)\?[^\s#]*/gi, "$1?[redacted]");
  detail = detail.replace(
    /\b(response\s+body|response\s+headers?|headers?|cookie\s+jar|body)\s*[:=]\s*[^\r\n]*/gi,
    "$1=[redacted]",
  );
  detail = detail.replace(
    /\b(authorization|proxy-authorization|set-cookie|cookie)\s*[:=]\s*[^\r\n]*/gi,
    "$1=[redacted]",
  );
  detail = detail.replace(
    /\b(access[_-]?token|refresh[_-]?token|token|api[_-]?key)\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)/gi,
    "$1=[redacted]",
  );
  detail = detail.replace(/\s+/g, " ").trim();
  return detail.slice(0, MAX_FAILURE_DETAIL_LENGTH);
}

async function fetchWithTimeout(fetchImpl, url, options = {}) {
  return fetchImpl(url, {
    ...options,
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
}

async function assertStatus(name, response, expectedStatus) {
  if (response.status !== expectedStatus) {
    const body = safeExcerpt(await response.text());
    throw new Error(
      `${name} failed: expected ${expectedStatus}, got ${response.status}. Body: ${body}`,
    );
  }
  return response;
}

async function expectStatus(name, response, expectedStatus) {
  await assertStatus(name, response, expectedStatus);
  process.stdout.write(`✓ ${name}\n`);
  return response;
}

async function expectJSON(name, response, expectedStatus) {
  await expectStatus(name, response, expectedStatus);
  try {
    return await response.json();
  } catch {
    throw new Error(`${name} failed: response was not valid JSON.`);
  }
}

export async function verifyWebRoute(
  { webBaseURL, expectedWebRedirectBaseURL },
  fetchImpl,
) {
  const expectedRedirect = expectedWebRedirectBaseURL?.trim();
  if (expectedRedirect) {
    const response = await assertStatus(
      "web speak redirect",
      await request(fetchImpl, endpoint(webBaseURL, "/speak"), { redirect: "manual" }),
      308,
    );
    const expectedLocation = endpoint(
      normalizeBaseURL(expectedRedirect, "EXPECTED_WEB_REDIRECT_BASE_URL"),
      "/speak",
    );
    const actualLocation = response.headers.get("location");
    if (actualLocation !== expectedLocation) {
      throw new Error(
        `web speak redirect failed: expected redirect to ${expectedLocation}, got ${actualLocation ?? "no Location header"}.`,
      );
    }
    return "web speak redirect";
  }

  await assertStatus(
    "web speak route",
    await request(fetchImpl, endpoint(webBaseURL, "/speak")),
    200,
  );
  return "web speak route";
}

export async function waitForServices(
  { webBaseURL, apiBaseURL },
  fetchImpl,
  {
    startupTimeoutMs = STARTUP_TIMEOUT_MS,
    pollIntervalMs = POLL_INTERVAL_MS,
    now = Date.now,
    sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
  } = {},
) {
  const checks = [
    ["web", endpoint(webBaseURL, "/web-healthz")],
    ["API", endpoint(apiBaseURL, "/healthz")],
  ];
  const pending = new Map(checks);
  const lastFailures = new Map();
  const startedAt = now();

  while (pending.size > 0 && now() - startedAt < startupTimeoutMs) {
    for (const [name, url] of pending) {
      try {
        const response = await fetchWithTimeout(fetchImpl, url);
        if (response.ok) {
          pending.delete(name);
          lastFailures.delete(name);
        }
      } catch (error) {
        lastFailures.set(name, safeFailureDetail(error));
      }
    }
    if (pending.size > 0) {
      await sleep(pollIntervalMs);
    }
  }

  if (pending.size > 0) {
    const pendingNames = [...pending.keys()];
    const failureDetails = pendingNames
      .filter((name) => lastFailures.has(name))
      .map((name) => `${name}: ${lastFailures.get(name)}`);
    const detailSuffix = failureDetails.length > 0
      ? ` Last failures: ${failureDetails.join("; ")}.`
      : "";
    throw new Error(`Services did not become ready: ${pendingNames.join(", ")}.${detailSuffix}`);
  }
}

async function request(fetchImpl, url, { origin, cookie, json, ...options } = {}) {
  const headers = new Headers(options.headers);
  if (origin) headers.set("Origin", origin);
  if (cookie) headers.set("Cookie", cookie);
  if (json !== undefined) headers.set("Content-Type", "application/json");
  return fetchWithTimeout(fetchImpl, url, {
    ...options,
    headers,
    body: json === undefined ? options.body : JSON.stringify(json),
  });
}

async function cleanupSession({ fetchImpl, apiBaseURL, webOrigin, cookie, recordingID }) {
  const errors = [];
  if (recordingID) {
    try {
      const response = await request(
        fetchImpl,
        endpoint(apiBaseURL, `/api/recordings/${encodeURIComponent(recordingID)}`),
        { method: "DELETE", origin: webOrigin, cookie },
      );
      if (response.status !== 200 && response.status !== 404) {
        errors.push(`recording cleanup returned ${response.status}: ${safeExcerpt(await response.text())}`);
      }
    } catch (error) {
      errors.push(`recording cleanup failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  if (cookie) {
    try {
      const response = await request(fetchImpl, endpoint(apiBaseURL, "/api/auth/logout"), {
        method: "POST",
        origin: webOrigin,
        cookie,
      });
      if (response.status !== 200) {
        errors.push(`logout cleanup returned ${response.status}: ${safeExcerpt(await response.text())}`);
      }
    } catch (error) {
      errors.push(`logout cleanup failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return errors;
}

export async function runStackSmoke({ env = process.env, fetchImpl = fetch } = {}) {
  const urls = selectStackURLs(env);
  const {
    webBaseURL,
    apiBaseURL,
    webOrigin,
    expectedWebRedirectBaseURL,
  } = urls;
  await waitForServices(urls, fetchImpl);

  let cookie = "";
  let recordingID = "";
  let primaryError;

  try {
    const webHealth = await expectJSON(
      "web health",
      await request(fetchImpl, endpoint(webBaseURL, "/web-healthz")),
      200,
    );
    if (webHealth?.service !== "web") {
      throw new Error(`web health failed: expected service=web, got ${JSON.stringify(webHealth).slice(0, 300)}.`);
    }

    const webRouteCheck = await verifyWebRoute(
      {
        webBaseURL,
        expectedWebRedirectBaseURL,
      },
      fetchImpl,
    );
    process.stdout.write(`✓ ${webRouteCheck}\n`);

    await expectStatus(
      "API health",
      await request(fetchImpl, endpoint(apiBaseURL, "/healthz")),
      200,
    );

    const openapi = await expectJSON(
      "API OpenAPI document",
      await request(fetchImpl, endpoint(apiBaseURL, "/openapi.json")),
      200,
    );
    if (openapi?.openapi !== "3.1.0") {
      throw new Error(`API OpenAPI document failed: expected OpenAPI 3.1.0, got ${JSON.stringify(openapi?.openapi)}.`);
    }

    const docsResponse = await request(fetchImpl, endpoint(apiBaseURL, "/docs"));
    await expectStatus("API Swagger UI", docsResponse, 200);
    if (!(await docsResponse.text()).includes("SwaggerUIBundle")) {
      throw new Error("API Swagger UI failed: response did not include SwaggerUIBundle.");
    }

    const preflightResponse = await request(
      fetchImpl,
      endpoint(apiBaseURL, "/api/auth/session"),
      {
        method: "OPTIONS",
        origin: webOrigin,
        headers: {
          "Access-Control-Request-Method": "GET",
          "Access-Control-Request-Headers": "Content-Type",
        },
      },
    );
    await expectStatus("credentialed CORS preflight", preflightResponse, 204);
    if (
      preflightResponse.headers.get("access-control-allow-origin") !== webOrigin
      || preflightResponse.headers.get("access-control-allow-credentials") !== "true"
    ) {
      throw new Error("credentialed CORS preflight failed: expected exact origin and allow-credentials=true.");
    }

    const email = `stack-smoke-${Date.now()}-${randomUUID()}@example.com`;
    const registerResponse = await request(fetchImpl, endpoint(apiBaseURL, "/api/auth/register"), {
      method: "POST",
      origin: webOrigin,
      json: { email, password: "StackSmoke123!" },
    });
    await expectStatus("auth register", registerResponse, 201);
    cookie = extractCookieHeader(registerResponse);
    if (!cookie) throw new Error("auth register failed: response did not include a session cookie.");

    await expectStatus(
      "authenticated session",
      await request(fetchImpl, endpoint(apiBaseURL, "/api/auth/session"), {
        origin: webOrigin,
        cookie,
      }),
      200,
    );
    await expectStatus(
      "authenticated user data",
      await request(fetchImpl, endpoint(apiBaseURL, "/api/user/data"), {
        origin: webOrigin,
        cookie,
      }),
      200,
    );

    const recordingPayload = await expectJSON(
      "create disposable recording",
      await request(fetchImpl, endpoint(apiBaseURL, "/api/user/recordings"), {
        method: "POST",
        origin: webOrigin,
        cookie,
        json: {
          recording: {
            topic: "Stack smoke",
            duration: 1,
            practiceType: "free_talk",
            timestamp: new Date().toISOString(),
            audioDataUrl: "data:audio/webm;base64,AAAA",
          },
        },
      }),
      201,
    );
    recordingID = recordingPayload?.recording?.id;
    if (typeof recordingID !== "string" || recordingID === "") {
      throw new Error("create disposable recording failed: response did not include a recording id.");
    }

    const uploadURL = resolveApiUploadURL(recordingPayload?.recording?.audioDataUrl, apiBaseURL);
    const uploadResponse = await request(fetchImpl, uploadURL, { origin: webOrigin, cookie });
    await expectStatus("serve disposable recording audio", uploadResponse, 200);
    if ((await uploadResponse.arrayBuffer()).byteLength === 0) {
      throw new Error("serve disposable recording audio failed: response was empty.");
    }

    await expectStatus(
      "delete disposable recording",
      await request(
        fetchImpl,
        endpoint(apiBaseURL, `/api/recordings/${encodeURIComponent(recordingID)}`),
        { method: "DELETE", origin: webOrigin, cookie },
      ),
      200,
    );
    recordingID = "";

    await expectStatus(
      "auth logout",
      await request(fetchImpl, endpoint(apiBaseURL, "/api/auth/logout"), {
        method: "POST",
        origin: webOrigin,
        cookie,
      }),
      200,
    );
    cookie = "";
  } catch (error) {
    primaryError = error instanceof Error ? error : new Error(String(error));
  }

  const cleanupErrors = await cleanupSession({
    fetchImpl,
    apiBaseURL,
    webOrigin,
    cookie,
    recordingID,
  });
  if (primaryError) {
    if (cleanupErrors.length > 0) {
      primaryError.message += ` Cleanup: ${cleanupErrors.join("; ")}`;
    }
    throw primaryError;
  }
  if (cleanupErrors.length > 0) {
    throw new Error(`Stack smoke cleanup failed: ${cleanupErrors.join("; ")}`);
  }

  process.stdout.write("Separate web/backend stack smoke checks passed.\n");
}

const currentFile = fileURLToPath(import.meta.url);
const invokedFile = process.argv[1] ? path.resolve(process.argv[1]) : "";

if (invokedFile === currentFile) {
  runStackSmoke().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
