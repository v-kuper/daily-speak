# Mobile API Contract Implementation Plan

## Goal

Establish a stable `/api/v1` boundary for mobile clients without changing the
legacy `/api/*` contract used by the web sandbox.

## Scope

1. Add a version discovery endpoint and the first paginated mobile resource:
   recordings.
2. Add a validated request ID to every HTTP response and expose it to browser
   clients.
3. Return structured, machine-readable errors from `/api/v1` only.
4. Use opaque cursor pagination with bounded page sizes.
5. Document and test the compatibility policy in OpenAPI.

Access and refresh tokens, anonymous principals, durable jobs, and object
storage remain separate later epics. During the transition, protected v1
resources accept the existing session cookie; adding bearer tokens later is an
additive authentication method.

## Contract

- `GET /api/v1` returns the active major version and stability state.
- `GET /api/v1/recordings?limit=20&cursor=...` returns `{items, page}`.
- `GET /api/v1/recordings/{recordingId}` returns the existing recording shape.
- Errors use `{error: {code, message, requestId}}`.
- Every response includes `X-Request-ID`; safe caller-provided IDs are retained.
- Legacy `/api/*` paths retain their existing response shapes.

## Verification

- Unit tests cover request IDs, error envelopes, invalid pagination and cursor
  round-trips.
- PostgreSQL-backed CI tests cover stable ordering and traversal between pages.
- OpenAPI tests require all v1 operations to document request IDs, stable error
  responses, and pagination.
- Full backend, API documentation, and infrastructure test suites remain green.
