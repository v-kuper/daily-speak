# Durable background work

## Outcome

Move paid and media background processing out of the HTTP process. API calls
persist the domain state and a PostgreSQL job in one transaction. A separately
deployable worker claims jobs with leases, renews heartbeats, retries transient
failures with bounded exponential backoff, and records terminal failures.

## Delivery

1. Add a versioned `processing_jobs` migration and retain legacy cleanup work.
2. Add a queue store with `FOR UPDATE SKIP LOCKED`, fenced lease tokens,
   heartbeat renewal, retry scheduling, and transactional terminal failure.
3. Make recording creation, upload finalization, retry, shadowing, and media
   deletion enqueue work in the same transaction as their state changes.
4. Make job handlers resume from the persisted recording stage and no-op after
   an already-published result, so crash redelivery does not repeat completed
   work.
5. Add a `cmd/worker` entrypoint and a dedicated Compose service. The API image
   contains both binaries, but API and worker run as separate processes.
6. Cover concurrency, lease recovery, retry, and idempotency in PostgreSQL
   integration tests. CI is the authoritative PostgreSQL and Docker check.

## Rollout and rollback

Deploy the migration, API, and worker together. The worker owns transcription,
analysis, shadowing, and file deletion; the API never launches those jobs in
memory. Old `pending_file_deletions` rows are copied into the new queue during
migration. Completed and failed jobs remain as an operational audit trail.

Rollback is application-only: stop the worker and deploy the previous API.
The additive tables and nullable recording column are safe for the previous
release to ignore. Jobs created during the new release remain available for a
forward repair; the migration is not destructively reversed.
