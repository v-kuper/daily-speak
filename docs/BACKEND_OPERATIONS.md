# Backend operations and scaling runbook

This runbook applies to the standalone Go API and durable worker. It does not
couple either process to the Next.js sandbox. The current Compose deployment is
a single-host test topology; the same signals and controls are intended for a
future independently scaled deployment.

## Traffic admission and trusted proxies

The API uses PostgreSQL-backed counters, so authentication and expensive-work
limits are shared by every API replica:

| Scope | Default | Subject |
| --- | ---: | --- |
| register, login, anonymous identity, refresh | 20/minute | client IP |
| recording creation and guest preview | 20/hour | principal and client IP |
| other API mutations | 120/minute | principal, falling back to client IP |

Rejected requests return `429`, `Retry-After`, and the standard
`RateLimit-Limit`, `RateLimit-Remaining`, and `RateLimit-Reset` headers. A
limiter storage failure rejects a protected mutation with `503`; it never
silently permits costly work.

`X-Forwarded-For` and `X-Forwarded-Proto` are ignored unless the direct peer is
inside `TRUSTED_PROXY_CIDRS`. Configure only the actual load-balancer or reverse
proxy subnet. A broad public or office CIDR lets callers spoof their source and
must not be used. HTTPS requests from a trusted proxy receive HSTS; API
responses also receive CSP, frame, content-type, referrer and permissions
policies.

Tune limits with `RATE_LIMIT_*` only after reviewing measured traffic. Increasing
an expensive limit without increasing worker/provider capacity merely moves the
failure from admission to the queue.

## Liveness, readiness, and metrics

- `/healthz` is process liveness and deliberately does not query dependencies.
  Container restart policies use this endpoint.
- `/readyz` checks PostgreSQL and the active durable queue. Load balancers use
  it to admit traffic. It returns `503` when queue depth exceeds
  `READINESS_MAX_QUEUE_DEPTH` or the oldest active job exceeds
  `READINESS_MAX_OLDEST_JOB_AGE`.
- `/metrics` emits Prometheus text only when `METRICS_BEARER_TOKEN` is configured
  and the same token is supplied as `Authorization: Bearer ...`. Keep this token
  in the deployment secret store, not GitHub variables or repository files. A
  configured token must contain at least 32 characters.

Metrics have bounded route/status labels and never include principal IDs, IP
addresses, tokens, filenames, recordings, or transcript text. They include HTTP
request count/duration/in-flight gauges, PostgreSQL pool use, durable job counts,
oldest active job age, and terminal outcomes completed during the last hour.
Every response includes `X-Request-ID` and `Traceparent`;
structured request logs include both the request ID and W3C trace ID.

Recommended first alerts:

| Signal | Initial warning | Initial critical |
| --- | ---: | ---: |
| API 5xx ratio (5 min) | > 1% | > 5% |
| API p95 latency (10 min) | > 500 ms | > 2 s |
| readiness unavailable | 2 min | 5 min |
| acquired DB connections / max | > 70% | > 85% |
| oldest `recording.process` job | > 10 min | > 30 min |
| oldest `guest.preview` job | > 2 min | > 10 min |
| oldest `interview.process` job | > 2 min | > 10 min |
| terminal/failed jobs | any sustained increase | page when user work is affected |

External Ollama, Whisper, and Cartesia outcome/duration counters are the next
provider-specific dashboard increment. Existing structured worker events remain
available until those counters are exported.

## Scaling policy

Scale API replicas for HTTP concurrency only; no request handler owns paid
background processing. Scale worker pools independently by job kind:

1. Add API replicas when p95 latency or in-flight requests rise while the DB
   pool stays below 70% utilization.
2. Add recording workers when `recording.process` age rises. Increase
   `WORKER_RECORDING_CONCURRENCY` only within Whisper/Ollama CPU, memory, and
   provider limits.
3. Scale adaptive interview workers with `WORKER_INTERVIEW_CONCURRENCY` when
   preparation, answer transcription, or question refill waits grow. Measure
   answer upload time, queue wait, local Whisper time, question generation time,
   and the fraction of transitions using an adaptive question separately.
4. Scale guest preview and shadowing pools independently; do not let a Cartesia
   slowdown consume recording workers.
5. Before adding replicas, size PostgreSQL `max_connections` and per-process
   `pool_max_conns` in `DATABASE_URL`. Reserve connections for migrations,
   administration, and backup jobs.
6. Keep media outside API container layers. Local mode requires one shared,
   persistent host volume; multi-host workers require the existing S3 storage
   driver or an equivalent shared object store.

Terminal durable-job bookkeeping is retained for 30 days by default and pruned
in bounded worker maintenance batches (`WORKER_JOB_RETENTION`). This keeps queue
metrics and indexes bounded without deleting recordings, media, or audit-worthy
authentication sessions. Expired distributed rate-limit counters are pruned by
the same bounded maintenance loop rather than during user requests.

Interview answer WAV files are temporary media with a 24-hour retention time.
The regular media sweep enqueues their deletion after expiry; it does not delete
the complete recording or its separate question timeline. A guest may upload
at most 8 MiB of answer WAV data per interview; an account may upload 24 MiB.
The final audio is transcribed independently as one file. Monitor media cleanup
age as well as interview job age if live transcription is heavily used.

## Backup and recovery

For the current Windows/local-media deployment, a recoverable backup is a
matched PostgreSQL dump and uploads snapshot. Quiesce new mutations or record a
common snapshot time, then:

```powershell
cmd /c "docker compose exec -T postgres pg_dump -U postgres -d daily_speaking -Fc > D:\DailySpeaking\backups\daily-speaking.dump"
robocopy D:\DailySpeaking\data\uploads D:\DailySpeaking\backups\uploads /E /COPY:DAT /R:2 /W:2
```

Restore into a separate database and uploads directory first; run API
readiness, ownership/download, recording retry, and deletion checks before
promoting it. Never validate a backup by overwriting the active volume. For S3,
enable bucket versioning/lifecycle protection and pair the object snapshot time
with the PostgreSQL recovery point.

Run a restore rehearsal at least monthly and before schema/storage migrations.
Record dump time, restore time, row counts, sampled media checksums, RPO, and
RTO. A backup that has not passed restore verification is not considered a
production backup.

## Load and failure verification

Run the read-only load probe against staging or an already-running remote
environment; it never starts Docker locally:

```bash
API_BASE_URL=https://api.example.com \
LOAD_PATH=/api/v1 \
LOAD_DURATION_SECONDS=60 \
LOAD_CONCURRENCY=50 \
npm run test:load-api
```

The probe reports request rate, status counts and p50/p95/p99, and fails when
p95 exceeds `LOAD_MAX_P95_MS` or 5xx/transport error rate exceeds
`LOAD_MAX_ERROR_RATE`. Repeat at increasing concurrency while watching DB pool
and queue metrics; the last passing level, not the configured worker count, is
the initial capacity claim.

Before production, rehearse these failures in staging:

1. make PostgreSQL temporarily unreachable: `/healthz` stays `200`, `/readyz`
   becomes `503`, protected writes fail closed;
2. stop workers while API stays up: queue age/depth grows, then readiness blocks
   admission at the configured bound; restarting workers drains leased work;
3. fail each AI/TTS provider: jobs retry with backoff and become terminal without
   losing source media or successful earlier stages;
4. interrupt a worker during each job kind: the expired lease is reclaimed once
   and idempotent storage prevents duplicate user-visible results;
5. fill local storage or deny S3 writes: media creation fails safely and no DB
   row advertises an unavailable ready object.

Keep dated results beside the deployment change record. Thresholds in this file
are starting hypotheses until a production-like run supplies evidence.
