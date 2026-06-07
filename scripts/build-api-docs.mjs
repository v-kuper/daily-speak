import { readFileSync, writeFileSync } from "node:fs";

const openapiPath = "docs/api/openapi.json";
const generatedPath = "docs/api/openapi.spec.js";

const spec = JSON.parse(readFileSync(openapiPath, "utf8"));
const output = `window.DAILY_SPEAKING_OPENAPI = ${JSON.stringify(spec, null, 2)};\n`;

writeFileSync(generatedPath, output);
