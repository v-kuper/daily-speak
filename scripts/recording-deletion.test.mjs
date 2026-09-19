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

const deletion = await importTypeScriptModule("src/lib/recordingDeletion.ts");

test("removing a recording also removes its feed publication", () => {
  const result = deletion.removeRecordingAndFeedPost(
    [{ id: "recording-1" }, { id: "recording-2" }],
    [
      { id: "post-1", sourceRecordingId: "recording-1" },
      { id: "post-2", sourceRecordingId: "recording-2" },
    ],
    "recording-1",
  );

  assert.deepEqual(result.recordings, [{ id: "recording-2" }]);
  assert.deepEqual(result.feedPosts, [{ id: "post-2", sourceRecordingId: "recording-2" }]);
});

test("late responses cannot restore deleted recordings or feed posts", () => {
  assert.deepEqual(
    deletion.filterDeletedRecordings([{ id: "deleted" }, { id: "kept" }], ["deleted"]),
    [{ id: "kept" }],
  );
  assert.deepEqual(
    deletion.filterDeletedFeedPosts(
      [
        { id: "deleted-post", sourceRecordingId: "deleted" },
        { id: "kept-post", sourceRecordingId: "kept" },
      ],
      ["deleted"],
    ),
    [{ id: "kept-post", sourceRecordingId: "kept" }],
  );
});
