# DailySpeak API Docs

The backend serves Swagger UI at `/docs` and its canonical OpenAPI document at
`/openapi.json`.

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
