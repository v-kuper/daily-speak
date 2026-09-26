# Mobile identity implementation plan

## Outcome

Add a backend-owned identity layer for mobile without changing the legacy web
cookie contract. Existing users keep their ids and receive matching `user`
principals. New installations can receive expiring `guest` principals and later
merge them into a registered account.

## Database

- add `principals` and backfill every existing user;
- create future user principals with a database trigger so every writer obeys
  the invariant;
- store one independently revocable `device_session` per installation/login;
- store opaque refresh tokens only as SHA-256 hashes;
- keep consumed refresh rows so replay can revoke the complete device family;
- record guest-to-user merges once and revoke all guest sessions atomically.

## HTTP contract

- `POST /api/v1/auth/anonymous` creates a bounded guest grant;
- `POST /api/v1/auth/register` registers and optionally upgrades a guest;
- `POST /api/v1/auth/login` signs in and optionally merges a guest;
- `POST /api/v1/auth/refresh` rotates the single-use refresh token;
- `GET /api/v1/auth/session` reads the current identity;
- `POST /api/v1/auth/logout` and `/logout-all` revoke grants;
- `GET /api/v1/auth/sessions` lists active devices;
- `DELETE /api/v1/auth/sessions/{sessionId}` revokes one owned device;
- protected v1 recording reads accept Bearer JWTs and retain cookie fallback.

## Security and rollout

Access tokens use HS256 with issuer, audience, expiry, session, principal, kind,
and unique token id claims. Every authenticated request verifies both the
signature and the server-side device session, so logout is immediate. Access
tokens default to 15 minutes; refresh grants to 30 days; guests to 24 hours.

`AUTH_ACCESS_TOKEN_SECRET` is intentionally deployment-owned. Missing config
does not break the existing web deployment: only mobile identity returns a
stable `503 identity_unavailable` until the secret is installed. A weak secret
or invalid duration fails startup.

## Verification

Unit tests cover signing, expiry, tamper detection, random opaque refresh
tokens, configuration validation, stable error envelopes, and unchanged CORS.
CI PostgreSQL tests cover principal backfill, guest registration/login merge,
Bearer access, hash-only refresh storage, rotation, replay-family revocation,
individual device revocation, and logout-all. OpenAPI and infrastructure
contract suites remain mandatory.
