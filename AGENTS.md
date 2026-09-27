# Project instructions for coding agents

These instructions apply to the whole repository. Keep changes scoped to the
application being modified: `web/` and `backend/` are independent projects.

## Backend work

Before changing Go code, read:

1. [`docs/BACKEND_DEVELOPMENT.md`](docs/BACKEND_DEVELOPMENT.md)
2. [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
3. [`docs/api-compatibility.md`](docs/api-compatibility.md) when an HTTP contract changes

The non-negotiable dependency direction is:

```text
HTTP or worker transport -> feature application service -> repository/provider port -> adapter
cmd -> app composition -> concrete dependencies
```

- Keep business rules, ownership checks, idempotency, and state transitions in
  the owning feature package.
- Keep HTTP parsing, authentication gates, status/error mapping, and response
  serialization in `backend/internal/httpapi`.
- Keep SQL, `pgx`, and transactions in feature repositories. Translate adapter
  errors to feature-owned errors before returning to transport.
- Select implementations and read environment configuration only in `cmd` or
  `backend/internal/app` and dedicated configuration packages.
- Run paid, slow, or retryable work through the durable worker queue; do not
  start it in API goroutines.
- Do not create catch-all `domain`, `utils`, or `helpers` packages. Shared
  vocabulary must have an explicit feature owner.
- New native-client endpoints belong under `/api/v1`; do not extend legacy
  `/api/*` merely for mobile development.
- Update OpenAPI, tests, and the owning canonical document in the same change
  when their contract changes.

Before completing backend work, format modified Go files and run the relevant
package tests. When the environment permits, also run:

```bash
cd backend && go test ./...
cd backend && go vet ./...
npm run test:api-docs
npm run test:infra
```

Do not claim database integration, Docker, or deployment verification when the
required environment was not actually available.

