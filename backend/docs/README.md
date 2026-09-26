# DailySpeak API Docs

The backend serves Swagger UI at `/docs` and its canonical OpenAPI document at
`/openapi.json`.

The stable mobile surface starts at `/api/v1`. Compatibility and deprecation
rules are documented in `../../docs/api-compatibility.md`; the unversioned
`/api/*` routes remain the legacy web contract.

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
