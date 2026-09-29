import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const api = load("src/lib/apiClient.ts");
const media = load("src/lib/mediaUpload.ts");

const json = (body, status = 200) => Response.json(body, { status });

test("multipart upload sends at most three parts concurrently and completes them in order", async (t) => {
  api.configureApiClient("https://api.example.test");
  let active = 0;
  let maximumActive = 0;
  const started = [];
  let completedBody = null;

  t.mock.method(globalThis, "fetch", async (url) => {
    const partNumber = Number(new URL(String(url)).pathname.split("-").at(-1));
    started.push(partNumber);
    active += 1;
    maximumActive = Math.max(maximumActive, active);
    await new Promise((resolve) => setTimeout(resolve, 15));
    active -= 1;
    return new Response(null, { status: 200, headers: { ETag: `etag-${partNumber}` } });
  });

  const request = async (path, init) => {
    if (path === "/api/v1/media/uploads") {
      return json({
        asset: { id: "asset-1", state: "pending" },
        upload: { id: "upload-1", state: "pending", partSizeBytes: 3, partCount: 4 },
      }, 201);
    }
    if (path === "/api/v1/media/uploads/upload-1/parts") {
      const descriptors = JSON.parse(init.body).parts;
      return json({
        parts: descriptors.map((part) => ({
          partNumber: part.partNumber,
          request: {
            method: "PUT",
            url: `https://storage.example.test/part-${part.partNumber}`,
            headers: {},
          },
        })),
      });
    }
    if (path === "/api/v1/media/uploads/upload-1/complete") {
      completedBody = JSON.parse(init.body);
      return json({ asset: { id: "asset-1", state: "ready" } });
    }
    throw new Error(`Unexpected request: ${path}`);
  };

  const assetId = await media.uploadMedia({
    blob: new Blob(["abcdefghijkl"], { type: "audio/webm" }),
    purpose: "recording_audio",
    idempotencyKey: "test-upload",
    request,
  });

  assert.equal(assetId, "asset-1");
  assert.equal(maximumActive, 3);
  assert.deepEqual(started.slice(0, 3).sort(), [1, 2, 3]);
  assert.deepEqual(completedBody.parts.map(({ partNumber }) => partNumber), [1, 2, 3, 4]);
});
