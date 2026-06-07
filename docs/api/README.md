# DailySpeak API Docs

Open `docs/api/swagger.html` in a browser to inspect the API with Swagger UI.

`openapi.json` is the canonical OpenAPI 3.1 document. `openapi.spec.js` is generated from it so `swagger.html` can be opened directly from the filesystem without fetching a local JSON file.

After changing `openapi.json`, run:

```bash
npm run build:api-docs
```

To validate the docs against the current gateway route list:

```bash
npm run test:api-docs
```
