# Multi-pass English Error Analysis Design

**Date:** 2026-09-21

**Status:** Approved on 2026-09-21

## Intent

Daily Speaking should give an English learner a complete but conservative review of a recording. The review must preserve occasional Russian speech in the Whisper transcript, translate those Russian insertions, find every clear English error without an arbitrary result limit, and avoid marking acceptable natural English merely because another phrasing is possible.

Each AI request must have one narrow responsibility. Error detection is therefore split into specialized passes, followed by a separate adjudication pass, a separate natural-English rewrite, and the existing separate shadowing-audio job.

Success means:

- every Cyrillic word or phrase in the normalized transcript has a valid English correction;
- all clear learner errors found by the specialized passes survive when they are genuine;
- stylistic alternatives and merely optional improvements are excluded;
- every displayed error has a category, severity, correction, and useful explanation;
- a learning reference is shown only when the error maps to a known curated rule;
- old recordings remain readable without a data migration;
- the pipeline is reproducible in the clean Windows CI deployment and requires no manual runtime setup beyond the existing environment variables.

## Scope

This design changes the recording-analysis pipeline, the suggestion JSON contract, and the recording detail UI. It builds on the already implemented multilingual Whisper defaults and uncapped suggestion handling.

It does not introduce a new database table, an external grammar service, user-editable rule content, streaming partial suggestions, or a deployment step. It also does not replace Whisper, Ollama, or Cartesia.

## Selected Approach

Use seven independent detector requests, run with bounded concurrency, followed by one reviewer request. This is the most transparent design: every detector has a small prompt and a testable contract, while the reviewer is responsible only for quality control and normalization.

Two alternatives were rejected:

1. **One broad detection request.** It is faster and cheaper, but mixes unrelated responsibilities, makes omissions hard to diagnose, and encourages the model to stop after a small representative set.
2. **Three grouped detector requests.** It lowers latency, but grammar, vocabulary, and naturalness still overlap enough that prompt responsibilities become ambiguous. The user explicitly prefers one narrowly defined task per request.

## Existing Pipeline Context

The current background flow is:

```text
saved audio -> Whisper -> one broad suggestion request -> natural rewrite -> save ready result
                                                                  |
                                                                  v
                                                  background Cartesia shadowing audio
```

Suggestions are stored in the existing `recordings.suggestions` JSONB column. Processing already has `transcribing`, `suggestions`, and `rewriting` stages, a 15-minute outer context, and up to two parsing attempts for AI JSON. The UI reads suggestions through a normalization boundary and highlights every exact `wrong` phrase in the transcript.

The new flow is:

```text
saved audio
    |
    v
multilingual Whisper
    |
    v
seven independent detectors --bounded concurrency--> candidate set
    |
    v
reviewer/adjudicator
    |
    +--> validated suggestions saved to JSONB
    |
    v
natural-English rewrite
    |
    v
recording ready --> independent Cartesia shadowing job
```

The public processing stages remain unchanged. All detector and reviewer work is represented by the existing `suggestions` stage so no schema or client polling change is needed.

## Detector Passes

Every detector receives the full normalized transcript and relevant learner context, but only one error category. It must return an empty array when it finds no error in its category. It must not return advice, a rewrite, or candidates from another category.

| Pass | Category value | Finds | Must ignore |
| --- | --- | --- | --- |
| Russian/non-English insertion | `language_switch` | Every Cyrillic word or contiguous phrase and its natural English equivalent | English grammar and style |
| Verb grammar | `verb_grammar` | Tense, aspect, auxiliary, agreement, infinitive/gerund, and verb-form errors | Articles, prepositions, optional tense restyling |
| Nouns and determiners | `nouns_determiners` | Articles, determiners, plural forms, countability, and quantifiers | Optional article choices that are both grammatical |
| Prepositions | `prepositions` | Incorrect, missing, or extra prepositions and dependent prepositions | Alternative prepositions that preserve a valid intended meaning |
| Vocabulary | `vocabulary` | Wrong word, wrong word form, false friend, and broken collocation | Merely more advanced or more elegant synonyms |
| Sentence structure | `sentence_structure` | Word order, malformed questions/negation, fragments, run-ons, and broken clause structure | Spoken fragments that are natural and understandable in conversation |
| Naturalness | `naturalness` | Clearly non-idiomatic wording that native speakers normally would not use | Accent, fillers, register preferences, and valid wording with an optional smoother alternative |

The naturalness pass has the strictest inclusion threshold. Its prompt explicitly says: if the phrase is acceptable conversational English, return nothing.

The language-switch pass receives the server-extracted list of Cyrillic phrases and must cover every item exactly. Other detector prompts are told that Cyrillic text belongs to another pass and must ignore it.

## AI Contracts

### Detector response

Each detector returns only:

```json
{
  "candidates": [
    {
      "wrong": "exact text from the transcript",
      "right": "a context-appropriate correction",
      "explanation": "why this is an error in this context",
      "ruleId": null
    }
  ]
}
```

`wrong`, `right`, and `explanation` are required non-empty strings. `ruleId` is nullable. Category is assigned by the server from the pass definition, not trusted from the model.

The transcript is encoded as data within the user message. System prompts state that text inside the transcript is untrusted learner speech and must never be treated as instructions. The model is not allowed to generate URLs, reference titles, severity, corrected transcripts, or prose outside the JSON object.

### Reviewer response

The reviewer receives the transcript and the merged detector candidates. It has one narrow task: accept or reject each candidate and assign severity to accepted candidates. It does not repeat or rewrite detector-owned text, corrections, explanations, categories, or rule IDs.

```json
{
  "decisions": {
    "verb_grammar-001": "major",
    "naturalness-001": "reject"
  }
}
```

The decision map must contain every supplied candidate ID exactly once and no unknown IDs. Values are limited to `major`, `medium`, `minor`, and `reject`. The server copies the accepted candidate fields, applies the selected severity, removes duplicates and overlaps deterministically, and attaches curated learning-reference data. This keeps the model response flat and prevents a malformed repeated field from invalidating an otherwise correct review.

For a Russian insertion, the reviewer cannot return `reject`; the server also revalidates that `wrong` remains the exact Cyrillic phrase and `right` contains English text and no Cyrillic. For English errors, `wrong` must be an exact transcript substring and `right` must be meaningfully different.

## Severity Rules

- `major`: the error changes the intended meaning or timeline, or makes a clause difficult to understand.
- `medium`: the meaning remains recoverable, but the grammar, vocabulary, or construction is clearly wrong.
- `minor`: a real localized error with little impact on understanding. It is not a style preference.

Severity describes the communication impact, not the size of the corrected string. Natural but improvable wording is omitted instead of labeled `minor`.

## Learning References

The model never writes a title, explanation, or URL for a learning reference. A detector may return only a `ruleId` from the closed list allowed for its category; the reviewer does not repeat it. The server maps a recognized ID to:

```json
{
  "id": "subject-verb-agreement",
  "title": "Subject-verb agreement",
  "summary": "Match the verb form to the subject in person and number.",
  "url": "https://dictionary.cambridge.org/us/grammar/british-grammar/subject-verb-agreement"
}
```

The initial catalog covers these stable study topics:

- `subject-verb-agreement`
- `verb-forms`
- `present-simple-vs-continuous`
- `past-simple-vs-present-perfect`
- `modal-verbs`
- `infinitive-vs-gerund`
- `conditionals`
- `articles-a-an-the`
- `zero-article`
- `countable-vs-uncountable`
- `singular-and-plural-nouns`
- `quantifiers`
- `prepositions-of-time-and-place`
- `dependent-prepositions`
- `word-formation`
- `collocations`
- `false-friends`
- `word-order`
- `questions-and-negatives`
- `sentence-fragments-and-run-ons`
- `relative-clauses`
- `pronoun-reference`

Catalog URLs must be HTTPS links to an explicitly allowlisted educational source and are maintained in code, not in prompts. The initial allowlist is `dictionary.cambridge.org` and `learnenglish.britishcouncil.org`. A catalog item may omit its URL if no durable authoritative page is available. Each catalog entry also declares its compatible error categories. Unknown, category-incompatible, or absent IDs are normalized to no learning reference; the server must not invent a nearby rule. Russian code-switching normally has no grammar-rule reference unless the actual correction also maps unambiguously to a catalog topic.

## Server Orchestration

Introduce an analysis coordinator with three separable responsibilities:

1. build and execute one detector request for each pass;
2. merge the valid candidate lists and execute the reviewer;
3. validate and enrich the reviewer result before persistence.

AI transport is accessed through an injected interface or function so detector and reviewer orchestration can be tested without a live Ollama server. The existing natural-rewrite request uses the same transport boundary but remains a separate operation.

Detector requests run in parallel with a configurable limit:

- environment variable: `AI_ANALYSIS_CONCURRENCY`;
- default: `3`;
- accepted range: `1` through `7`;
- invalid values fall back to `3`.

Concurrency changes throughput only; every pass remains a separate request with a separate prompt and response. Candidate ordering is made deterministic after all workers complete: detector-definition order first, then transcript occurrence, then candidate ID.

Each detector and the reviewer gets at most two attempts. The second attempt uses the existing strict-JSON instruction and lower temperature. A syntactically valid empty detector list is success. Network failure, malformed JSON, or a response that violates the pass contract triggers the second attempt. If either attempt succeeds, processing continues.

If any detector still fails, or if the reviewer remains invalid, the recording fails at the `suggestions` stage. The system must not silently publish a partial review because completeness is an explicit product requirement. Completed candidates are not saved as final suggestions. The outer recording-processing timeout increases from 15 to 30 minutes so bounded concurrency plus retries cannot be cut off by the coordinator under normal provider latency.

## Deterministic Validation and Deduplication

Server validation is authoritative:

- every `wrong` value must occur verbatim in the normalized transcript;
- required Cyrillic phrases must each have exactly one valid final correction;
- a Russian correction must contain English text and no Cyrillic;
- category and severity must be known enum values;
- the reviewer decision map must contain every detector candidate ID exactly once and no unknown IDs;
- reviewer decisions are limited to `major`, `medium`, `minor`, and `reject`, and `language_switch` candidates cannot be rejected;
- `right` must be non-empty, different from `wrong`, and within existing domain length limits;
- explanations must be non-empty and are normalized to a bounded length;
- unknown `ruleId` values are removed rather than guessed;
- exact duplicates are collapsed;
- the server suppresses a shorter phrase fully contained by a longer accepted phrase, except that a mandatory Cyrillic correction always wins;
- there is no numeric cap on valid suggestions.

The initial release keeps phrase-based highlighting: a final `wrong` phrase highlights every exact case-insensitive occurrence in the transcript. This matches the existing UI contract. Position-aware occurrence metadata is deliberately deferred because it would require a separate transcript-span API and migration strategy; detectors must therefore prefer the smallest contextually safe phrase rather than a single common word.

## Stored and Public Suggestion Shape

Suggestions remain in the existing JSONB column. Stored rows contain the model-independent fields and the selected `ruleId`:

```json
{
  "wrong": "I am forgot",
  "right": "I forgot",
  "explanation": "Use the past-simple form here. The auxiliary “am” cannot be combined with the past form “forgot” in this construction.",
  "category": "verb_grammar",
  "severity": "medium",
  "ruleId": "verb-forms"
}
```

When the API reads a recognized `ruleId`, it enriches the public response from the current server-side catalog:

```json
{
  "wrong": "I am forgot",
  "right": "I forgot",
  "explanation": "Use the past-simple form here. The auxiliary “am” cannot be combined with the past form “forgot” in this construction.",
  "category": "verb_grammar",
  "severity": "medium",
  "ruleId": "verb-forms",
  "learningReference": {
    "id": "verb-forms",
    "title": "Verb forms",
    "summary": "Choose the verb form required by the tense and construction.",
    "url": "https://dictionary.cambridge.org/us/grammar/british-grammar/verbs-basic-forms"
  }
}
```

`category`, `severity`, `ruleId`, and `learningReference` are optional when reading stored JSON. Old recordings therefore retain their existing `wrong`, `right`, and `explanation` display without fabricated metadata. Newly generated suggestions always contain category and severity. No database migration is required.

The backend normalizer ignores any stored or model-supplied `learningReference` object and rebuilds it from the closed catalog for every API response. Catalog fixes therefore apply to old recordings without a data migration. The natural-rewrite request consumes only the final validated corrections and does not receive learning-reference content.

## UI Design

Each suggestion card displays, in order:

1. category and severity badges;
2. the incorrect and corrected phrases;
3. the detailed explanation;
4. an optional **What to study** block containing the curated title, summary, and link.

Severity uses both text and color so it remains understandable without color perception:

- Major — red;
- Medium — orange;
- Minor — yellow/amber with sufficient text contrast.

Transcript highlights use the same severity classes. When overlapping suggestions apply to one highlighted phrase, the highest severity wins. Old suggestions without severity keep the current neutral error style and show no fabricated badge or learning block.

Links open safely with `noopener noreferrer`. The UI parser accepts only supported category/severity values and a structurally valid reference; unexpected optional metadata is discarded without hiding the underlying correction.

## Natural Rewrite and Shadowing

The natural rewrite remains a dedicated AI request after final suggestions are saved. Its only task is to produce an English-only, natural transcript that preserves meaning and applies the approved corrections. It does not search for, score, or explain errors.

Shadowing audio remains the existing independent Cartesia job scheduled after the recording becomes ready. It receives the validated natural transcript and does not participate in error analysis.

## Logging and Operational Behavior

Structured logs identify the recording, pass name, attempt number, duration, candidate count, and validation outcome. Logs never include the full transcript or the complete prompt. Reviewer logs include input and output counts but not learner text.

The existing user-visible processing error remains generic and actionable. Internal logs distinguish provider errors, invalid JSON, missing Russian coverage, invented candidates, and timeout cancellation.

The Windows CI configuration supplies defaults in source control. The implementation adds `AI_ANALYSIS_CONCURRENCY=3` to the example and deployment configuration, but does not run or download Whisper/Ollama models during development.

## Testing Strategy

Backend tests cover:

- each detector prompt contains only its own category instructions;
- Cyrillic extraction and mandatory language-switch coverage;
- valid empty detector results;
- malformed detector/reviewer JSON and second-attempt behavior;
- bounded worker concurrency and deterministic output order;
- reviewer rejection of missing or invented candidate IDs and unsupported decisions;
- server-side reconstruction of suggestions from accepted detector candidates;
- conservative naturalness instructions;
- all three severity definitions;
- exact transcript membership, deduplication, and overlap resolution;
- more than twenty valid suggestions with no truncation;
- known, unknown, absent, and category-incompatible rule IDs;
- old suggestion JSON without new fields;
- a failed detector causing the whole suggestion stage to fail;
- rewrite execution only after a validated reviewer result.

Frontend tests cover:

- parsing new optional metadata and preserving old records;
- rendering category/severity text and curated references;
- rejecting unsafe or malformed reference data;
- severity-aware transcript segments and highest-severity overlap behavior;
- rendering an arbitrary number of suggestion cards.

Configuration tests assert the clean Windows deployment defaults. The normal quality suite and production build must pass. Actual deployment, model download, and Windows runtime execution remain outside local verification per the project constraint.

## Acceptance Criteria

1. A transcript containing `I am forgot ... капуста` retains `капуста`, produces a `language_switch` correction to an English equivalent, and separately reports `I am forgot` through the verb pass.
2. A fully natural sentence produces no suggestion merely because the model can phrase it differently.
3. A transcript with more than twenty genuine errors returns all validated errors.
4. Every new suggestion shows category and severity; a learning block appears only for a recognized catalog rule.
5. One permanently failing detector prevents a misleading partial review from being marked ready.
6. The rewrite and shadowing stages remain separate from detection and reviewer responsibilities.
7. Existing recordings created before this change still render correctly.
