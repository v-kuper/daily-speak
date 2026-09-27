# Production readiness backlog

Last reviewed: 2026-09-27

The architecture refactor is complete. Web and backend build independently;
API and worker composition lives outside HTTP; feature packages own business
rules and persistence; media supports local and S3-compatible storage; and CI
enforces the main package boundaries. Do not reopen a broad structural rewrite
without evidence from a concrete feature or production measurement.

Mobile development can start on the existing `/api/v1` identity, guest preview,
recording, and media contracts. Add missing v1 feature operations incrementally
when the mobile client needs profile, practice, deletion/retry, shadowing, or
subscription flows. New mobile code must not depend on legacy `/api/*` routes.

## P0 before production release

### Security and identity

- Add an access-token key ring with `kid`, overlapping verification keys, and a
  rehearsed rotation/revocation procedure.
- Require a separate `MEDIA_URL_SIGNING_SECRET`; production must not reuse the
  access-token secret for media URLs.
- Persist bounded audit events for login, refresh replay, device revocation,
  recovery, and administrative actions.
- Add verified email ownership and password recovery without account discovery.
- Add account-aware login throttling alongside source-IP limits.
- Complete the threat model for token theft, browser refresh, lost devices,
  provider compromise, and sensitive logs.
- Configure complete HTTP timeouts, header limits, and uniform request-body
  bounds.

### Reliability, data, and evidence

- Run the load probe in a production-like environment and record capacity,
  p95/p99 latency, queue age, provider saturation, database usage, and cost per
  recording.
- Rehearse PostgreSQL loss, stopped workers, expired leases, AI/TTS failure,
  object-storage failure, and disk exhaustion.
- Restore a matched PostgreSQL/media backup and record demonstrated RPO/RTO.
- Define retention, account deletion/export, and privacy behavior for audio,
  transcripts, AI output, guest identities, and audit events.

Configured thresholds are hypotheses until these checks are measured remotely.

## P1 scale and operations

### Providers and workers

- Keep Ollama, Whisper, and Cartesia behind feature-owned ports/adapters.
- Standardize deadlines, retry budgets, non-retryable errors, circuit breakers,
  and provider-specific concurrency limits.
- Export bounded latency, outcome, retry, saturation, and cost metrics per
  provider; add actionable alerts and dashboards.
- Keep PostgreSQL as the durable queue until measured workload proves it is the
  bottleneck.

### Data and deployment

- Move production media to private S3-compatible storage with encryption,
  lifecycle rules, migration verification, and disaster recovery.
- Set explicit PostgreSQL connection budgets per API and worker replica; add a
  pooler only when measurements justify it.
- Run migrations as a controlled deployment step using expand/contract changes
  compatible with rolling releases.
- Deploy web, API, and worker independently with DNS, TLS, least-privilege
  identities, readiness, rollout, and rollback.
- Add metrics scraping, centralized logs, trace export, SLOs, and alerts.

### CI/CD and supply chain

- Add Go vulnerability/static analysis and JavaScript dependency review.
- Scan final images, produce an SBOM, and automate dependency updates.
- Pin third-party GitHub Actions to reviewed commit SHAs.
- Add secret scanning and ensure diagnostics cannot print credentials.
- Reject unapproved breaking changes to `/api/v1` in CI.

## Product decision: retained Feed backend

Feed data, handlers, migrations, and OpenAPI operations remain isolated, but no
web or mobile UI exposes them. Restoring Feed requires publication consent,
deletion, moderation, abuse handling, retention, and accessibility. Removing it
requires a reviewed data/media migration and rollback plan.
