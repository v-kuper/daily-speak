import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const suggestions = createTypeScriptLoader()("src/lib/suggestions.ts");

test("malformed location metadata is rejected instead of becoming legacy feedback", () => {
  assert.deepEqual(suggestions.parseSuggestions([{ wrong: "go", right: "went", explanation: "Past time.", span: { start: -1, end: 2 } }]), []);
});

test("all valid AI suggestions reach transcript highlighting and the review list", () => {
  const input = Array.from({ length: 25 }, (_, index) => ({
    wrong: `wrong ${index}`,
    right: `right ${index}`,
    explanation: `explanation ${index}`,
  }));

  const parsed = suggestions.parseSuggestions(input);

  assert.equal(parsed.length, 25);
  assert.deepEqual(parsed.at(-1), input.at(-1));
});

test("invalid suggestions are removed without limiting valid ones", () => {
  const parsed = suggestions.parseSuggestions([
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
    { wrong: "missing fields" },
    null,
  ]);

  assert.deepEqual(parsed, [
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
  ]);
});

test("valid metadata is kept and old suggestions stay metadata-free", () => {
  const [modern, legacy] = suggestions.parseSuggestions([
    {
      wrong: "she go",
      right: "she goes",
      explanation: "Use agreement.",
      category: "verb_grammar",
      severity: "medium",
      ruleId: "subject-verb-agreement",
      learningReference: {
        id: "subject-verb-agreement",
        title: "Subject-verb agreement",
        summary: "Match subject and verb.",
        url: "https://dictionary.cambridge.org/us/grammar/british-grammar/subject-verb-agreement",
      },
    },
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
  ]);

  assert.equal(modern.severity, "medium");
  assert.equal(modern.learningReference.id, "subject-verb-agreement");
  assert.equal(legacy.severity, undefined);
  assert.equal(legacy.learningReference, undefined);
});

test("unsafe reference URL is dropped without dropping the correction", () => {
  const [parsed] = suggestions.parseSuggestions([
    {
      wrong: "she go",
      right: "she goes",
      explanation: "Use agreement.",
      category: "verb_grammar",
      severity: "medium",
      learningReference: { id: "x", title: "X", summary: "X", url: "javascript:alert(1)" },
    },
  ]);

  assert.equal(parsed.wrong, "she go");
  assert.equal(parsed.learningReference, undefined);
});

test("verified strengths keep supported metadata and are capped at three", () => {
  const input = Array.from({ length: 4 }, (_, index) => ({
    excerpt: `Useful phrase ${index}`,
    explanation: `Explanation ${index}`,
    category: "naturalness",
    ruleId: "collocations",
    learningReference: {
      id: "collocations",
      title: "Collocations",
      summary: "Use words that naturally occur together.",
    },
  }));
  const parsed = suggestions.parseStrengths(input);
  assert.equal(parsed.length, 3);
  assert.equal(parsed[0].excerpt, "Useful phrase 0");
  assert.equal(parsed[0].learningReference.id, "collocations");
});

test("legacy recordings and malformed strengths safely become an empty list", () => {
  assert.deepEqual(suggestions.parseStrengths(undefined), []);
  assert.deepEqual(suggestions.parseStrengths([{ excerpt: "Missing metadata" }]), []);
});
