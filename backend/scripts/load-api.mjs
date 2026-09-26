import { performance } from "node:perf_hooks";

function positiveNumber(name, fallback) {
  const raw = process.env[name]?.trim();
  if (!raw) return fallback;
  const value = Number(raw);
  if (!Number.isFinite(value) || value <= 0) throw new Error(`${name} must be positive`);
  return value;
}

const baseURL = (process.env.API_BASE_URL || "http://localhost:3219").replace(/\/$/, "");
const path = process.env.LOAD_PATH || "/api/v1";
const durationSeconds = positiveNumber("LOAD_DURATION_SECONDS", 30);
const concurrency = Math.floor(positiveNumber("LOAD_CONCURRENCY", 20));
const requestTimeoutMs = positiveNumber("LOAD_REQUEST_TIMEOUT_MS", 5000);
const maximumP95Ms = positiveNumber("LOAD_MAX_P95_MS", 500);
const maximumErrorRate = positiveNumber("LOAD_MAX_ERROR_RATE", 0.01);
const authorization = process.env.LOAD_BEARER_TOKEN?.trim();
const deadline = performance.now() + durationSeconds * 1000;
const latencies = [];
const statuses = new Map();
let transportErrors = 0;

async function worker() {
  while (performance.now() < deadline) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), requestTimeoutMs);
    const started = performance.now();
    try {
      const response = await fetch(`${baseURL}${path}`, {
        headers: authorization ? { Authorization: `Bearer ${authorization}` } : {},
        signal: controller.signal,
      });
      await response.arrayBuffer();
      latencies.push(performance.now() - started);
      statuses.set(response.status, (statuses.get(response.status) || 0) + 1);
    } catch {
      transportErrors += 1;
      latencies.push(performance.now() - started);
    } finally {
      clearTimeout(timer);
    }
  }
}

function percentile(values, fraction) {
  if (values.length === 0) return 0;
  return values[Math.min(values.length - 1, Math.ceil(values.length * fraction) - 1)];
}

await Promise.all(Array.from({ length: concurrency }, () => worker()));
latencies.sort((left, right) => left - right);
const serverErrors = [...statuses.entries()]
  .filter(([status]) => status >= 500)
  .reduce((total, [, count]) => total + count, 0);
const failures = transportErrors + serverErrors;
const total = latencies.length;
const errorRate = total === 0 ? 1 : failures / total;
const summary = {
  target: `${baseURL}${path}`,
  durationSeconds,
  concurrency,
  requests: total,
  requestsPerSecond: Number((total / durationSeconds).toFixed(2)),
  latencyMs: {
    p50: Number(percentile(latencies, 0.5).toFixed(2)),
    p95: Number(percentile(latencies, 0.95).toFixed(2)),
    p99: Number(percentile(latencies, 0.99).toFixed(2)),
  },
  statuses: Object.fromEntries([...statuses.entries()].sort(([left], [right]) => left - right)),
  transportErrors,
  errorRate: Number(errorRate.toFixed(5)),
};
process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);

if (summary.latencyMs.p95 > maximumP95Ms || errorRate > maximumErrorRate) {
  process.stderr.write(`Load thresholds failed: p95<=${maximumP95Ms}ms, errorRate<=${maximumErrorRate}\n`);
  process.exitCode = 1;
}
