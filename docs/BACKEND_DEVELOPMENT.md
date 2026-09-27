# Backend development guide

Last reviewed: 2026-09-27

This is the working agreement for extending the Go backend. It complements
[`ARCHITECTURE.md`](ARCHITECTURE.md), which describes the running system, and
[`api-compatibility.md`](api-compatibility.md), which defines the public mobile
contract.

## Core rule

Dependencies point inward toward feature policy:

```text
HTTP handler / worker runner
            |
            v
feature service and feature-owned types
            |
            v
repository or provider interface
            |
            v
PostgreSQL, storage, queue, or external-provider adapter
```

`cmd/api` and `cmd/worker` start processes. `internal/app` composes concrete
dependencies. Neither place owns business rules.

Use this placement rule when reviewing a change:

| Change | Owner |
| --- | --- |
| HTTP shape, status, or serialization | `internal/httpapi` and OpenAPI |
| Validation, authorization, or state policy | owning feature service |
| SQL or transaction mechanics | owning feature repository |
| External API or object storage details | provider/storage adapter |
| Environment selection and dependency wiring | `internal/app`, `cmd`, or config package |
| Slow, paid, or retryable processing | durable worker job |

## Layer responsibilities

### HTTP transport: `internal/httpapi`

HTTP code may:

- match routes and methods;
- authenticate the request and extract identity;
- enforce transport limits and decode input;
- call one feature service;
- map feature errors to stable HTTP errors;
- serialize feature results.

HTTP code must not contain SQL, `pgx`, transactions, provider calls, storage
DTOs, environment-driven construction, background processing, or business
state transitions. Authorization that depends on resource ownership belongs in
the feature service or repository, not only in a handler.

### Feature application package: `internal/<feature>`

Each feature owns its input/output models, policy errors, service interfaces,
validation, authorization rules, idempotency, and state transitions. Examples
are `recording`, `media`, `guestpreview`, `shadowing`, and `auth`.

A service coordinates policy through small interfaces. It must not know HTTP
status codes, JSON response shapes, environment variables, or concrete `pgx`
types. Return feature-owned errors and use `errors.Is`/`errors.As`; never branch
on error strings.

Shared vocabulary needs a clear owner. Learner data belongs to `learner`, quota
rules to `quota`, and media formats to `media`. Do not introduce generic
`domain`, `common`, `utils`, or cross-feature helper packages.

### Repository and unit of work

SQL and transaction mechanics live in the owning feature package, normally in
`*_repository.go`. A multi-step invariant uses a feature-owned unit-of-work or
transaction interface so the service can be tested without PostgreSQL.

Repositories may import `db`, `pgx`, and `workqueue`. They translate expected
adapter outcomes such as `pgx.ErrNoRows` into feature results or errors before
transport sees them. Persisting resource state and its durable job must be one
transaction when partial success would be unsafe.

Schema migrations are immutable, ordered files under `backend/migrations`.
Change an applied schema with a new migration; never rewrite historical
migrations merely to make the current schema look cleaner.

### Infrastructure adapters

Provider, queue, and storage packages implement feature-owned ports. Convert
their DTOs into feature-owned models at the boundary. Provider-specific
request formats, errors, retry details, and credentials must not leak into HTTP
or core feature policy.

Media bytes belong in the storage abstraction, not PostgreSQL. PostgreSQL owns
media identity, ownership, state, location, size, and checksum.

### Workers

Transcription, analysis, TTS, media cleanup, and other paid, slow, or retryable
work run through durable PostgreSQL jobs. API handlers persist work and return;
they do not start fire-and-forget goroutines.

Worker handlers must be safe to retry, respect leases and cancellation, use
bounded concurrency, and expose terminal failure through persisted state.

### Composition: `internal/app` and `cmd`

Construct repositories, providers, storage, services, and workers in the
composition root. Environment parsing belongs in process/configuration code.
Feature and HTTP packages receive explicit constructor dependencies and must
not silently create global clients or choose adapters.

## Adding a backend feature

Use this order:

1. Define the client-visible behavior. New application endpoints use `/api/v1`.
2. Add feature-owned inputs, outputs, errors, ports, and service policy.
3. Add repository/provider adapters and a migration when persistence changes.
4. Wire concrete dependencies in `internal/app`.
5. Add the thin HTTP handler or durable worker entry point.
6. Update `backend/docs/openapi.json` for a public API change.
7. Add tests at the lowest useful layer plus contract/integration coverage.
8. Update the owning canonical document when architecture or operations change.

A typical feature package stays responsibility-oriented rather than
file-size-oriented:

```text
internal/example/
  model.go             feature types and errors
  service.go           policy and orchestration
  repository.go        PostgreSQL adapter
  processor.go         durable processing policy, if needed
  provider_adapter.go  concrete external adapter, if feature-owned
  *_test.go
```

Split a file when responsibilities differ. Do not split code only to satisfy a
line-count target.

## API contract rules

- `/api/v1` is the only application contract shared by web and mobile.
- Protected resources use the same Bearer access token on web and mobile.
- V1 failures use a stable machine-readable code and include `X-Request-ID`.
- Retryable create/mutation flows use bounded idempotency keys.
- Timestamps cross the API in UTC RFC3339 form.
- Lists are bounded and paginated; request bodies have explicit size limits.
- Do not expose provider responses, SQL errors, internal paths, credentials, or
  storage implementation details.
- Backward-incompatible V1 changes require a new version or an explicit
  deprecation/migration plan.

The OpenAPI document is the public source of truth. After editing it, run:

```bash
cd backend && node scripts/build-api-docs.mjs --write
npm run test:api-docs
```

## Go style

- Always use `gofmt`; separate standard-library imports from non-standard
  imports consistently with the existing packages.
- Pass `context.Context` first to I/O and service methods; do not store it in a
  long-lived struct.
- Prefer explicit constructors and narrow interfaces declared by the consumer.
- Keep exported feature errors/types stable when transport depends on them.
- Avoid package globals except immutable constants, compiled expressions, and
  sentinel errors.
- Use structured logging with bounded fields. Never log tokens, credentials,
  raw media, full provider bodies, or sensitive transcripts.
- Comments should explain an invariant or a surprising decision, not restate
  the code.
- Delete obsolete paths cleanly. Add a compatibility layer only when a real
  deployed client or data migration requires it.

## Tests and completion

Use service unit tests for policy, repository integration tests for SQL and
transactions, HTTP tests for status/error mapping, and boundary tests for
dependency rules. A bug fix should include the smallest regression test that
would have caught it.

Backend work is complete when:

- the feature owner contains the business rule;
- transport and adapters expose only feature-owned contracts;
- authorization, idempotency, and failure behavior are covered;
- OpenAPI and canonical docs match the implementation;
- modified Go files are formatted;
- relevant tests pass, and unavailable environment-dependent checks are stated
  explicitly rather than assumed.
