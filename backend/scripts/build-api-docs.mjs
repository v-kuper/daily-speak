import { readFileSync, writeFileSync } from "node:fs";

const mode = process.argv[2];
if (mode !== "--check" && mode !== "--write") {
  process.stderr.write("Usage: node scripts/build-api-docs.mjs --check|--write\n");
  process.exit(2);
}

const path = "docs/openapi.json";
const source = readFileSync(path, "utf8");
const document = JSON.parse(source);
const canonical = `${JSON.stringify(document, null, 2)}\n`;
const comparableSource = source.replace(/\r\n/g, "\n");

if (mode === "--check" && comparableSource !== canonical) {
  process.stderr.write("OpenAPI formatting drifted. Run node scripts/build-api-docs.mjs --write\n");
  process.exit(1);
}
if (mode === "--write") {
  writeFileSync(path, canonical);
}
