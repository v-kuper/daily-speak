# Backend scale and operations implementation plan

## Goal

Add an operational boundary around the existing API and durable workers without
changing recording, identity, or media business flows. The backend must expose
separate liveness and readiness signals, reject abusive expensive traffic across
API replicas, publish low-cardinality metrics, and correlate requests with W3C
trace context.

## Scope

1. Add a PostgreSQL-backed fixed-window limiter for authentication, writes, and
   resource-expensive recording/guest-preview requests. Store only hashed
   subjects and trust forwarded addresses only from configured proxy CIDRs.
2. Keep `/healthz` process-only and add `/readyz` for PostgreSQL and durable queue
   admission checks.
3. Add a protected Prometheus text endpoint containing HTTP, PostgreSQL pool,
   and durable queue measurements. Metrics never contain user IDs, media names,
   transcripts, tokens, or raw IP addresses.
4. Propagate valid W3C `traceparent`, generate one otherwise, and include its
   trace ID in structured request logs.
5. Add API security headers, configuration tests, middleware tests, readiness
   tests, and a production runbook covering thresholds, scaling, backups, and
   fault/load verification.

## Non-goals

- Running Docker or local infrastructure in this workspace.
- Installing Prometheus, Grafana, an OpenTelemetry collector, or a managed load
  balancer.
- Moving local media to S3. Existing storage abstraction remains unchanged.
- Changing mobile API response models or the recording processing pipeline.

## Acceptance

- Existing API and worker tests remain green.
- Liveness remains healthy during a dependency outage; readiness fails closed.
- Rate-limit decisions are shared through PostgreSQL and return stable `429`
  responses with retry metadata.
- Metrics use bounded route/status labels and require an operations bearer token.
- Forwarded client information is ignored unless the direct peer is trusted.
- The deployment variables are optional with safe defaults and are documented.
