import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import ts from "typescript";

async function importTypeScriptModule(path) {
  const source = readFileSync(path, "utf8");
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2022,
      target: ts.ScriptTarget.ES2022,
    },
  });

  const moduleUrl = `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`;
  return import(moduleUrl);
}

const browserMedia = await importTypeScriptModule("src/lib/browserMedia.ts");

test("remote HTTP origins explain that microphone access requires HTTPS or localhost", () => {
  assert.equal(
    browserMedia.resolveBrowserRecordingSupportError({
      isBrowser: true,
      isSecureContext: false,
      protocol: "http:",
      hostname: "192.168.0.115",
      hasGetUserMedia: false,
      hasMediaRecorder: false,
    }),
    browserMedia.MICROPHONE_SECURE_CONTEXT_ERROR,
  );
});

test("localhost HTTP can still request browser microphone permission", () => {
  assert.equal(
    browserMedia.resolveBrowserRecordingSupportError({
      isBrowser: true,
      isSecureContext: true,
      protocol: "http:",
      hostname: "localhost",
      hasGetUserMedia: true,
      hasMediaRecorder: true,
    }),
    null,
  );
});

test("secure pages without getUserMedia keep the unsupported-browser message", () => {
  assert.equal(
    browserMedia.resolveBrowserRecordingSupportError({
      isBrowser: true,
      isSecureContext: true,
      protocol: "https:",
      hostname: "example.com",
      hasGetUserMedia: false,
      hasMediaRecorder: true,
    }),
    "Your browser does not support microphone recording.",
  );
});

test("permission policy errors are reported separately from user denial", () => {
  assert.equal(
    browserMedia.resolveMicrophoneError(
      { name: "SecurityError" },
      {
        isBrowser: true,
        isSecureContext: true,
        protocol: "https:",
        hostname: "example.com",
        hasGetUserMedia: true,
        hasMediaRecorder: true,
      },
    ),
    browserMedia.MICROPHONE_POLICY_ERROR,
  );
});

test("user denial remains a browser settings permission message on secure origins", () => {
  assert.equal(
    browserMedia.resolveMicrophoneError(
      { name: "NotAllowedError" },
      {
        isBrowser: true,
        isSecureContext: true,
        protocol: "https:",
        hostname: "example.com",
        hasGetUserMedia: true,
        hasMediaRecorder: true,
      },
    ),
    "Microphone access denied. Allow microphone permission in your browser settings.",
  );
});
