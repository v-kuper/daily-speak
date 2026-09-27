# DailySpeak API Docs

The backend serves Swagger UI at `/docs` and its canonical OpenAPI document at
`/openapi.json`.

The shared web/mobile application surface starts at `/api/v1`. Compatibility
and deprecation rules are documented in `../../docs/api-compatibility.md`.

Swagger's **Authorize** action accepts the short-lived Bearer access token.
Refresh tokens are intentionally entered only in `POST /api/v1/auth/refresh`.

`openapi.json` is the canonical OpenAPI 3.1 document. It is deterministically
formatted and embedded in the backend binary; no browser-side generated copy is
maintained.

After changing `openapi.json`, run:

```bash
cd backend && node scripts/build-api-docs.mjs --write
```

To validate the docs against the current gateway route list:

```bash
cd backend && node scripts/build-api-docs.mjs --check && node --test scripts/api-docs.test.mjs
```
