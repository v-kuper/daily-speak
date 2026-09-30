# DailySpeak API compatibility policy

The application API uses a major version in its path, starting with `/api/v1`.
Within one major version, existing fields, meanings, status codes, and error
codes are not removed or repurposed. New optional response fields and new
endpoints may be added; clients must ignore fields they do not recognize.

List cursors are opaque. Clients must send them back unchanged and must not
construct or persist them as durable identifiers. Page limits are bounded by
the server.

Every response carries `X-Request-ID`. Clients should include it in support and
diagnostic reports. A caller may supply a safe `X-Request-ID`; otherwise the
server creates one.

Web and native clients use the same `/api/v1/*` contract. Unversioned
application routes are not supported. The web sandbox may move faster in its
UI, but it does not receive a private backend protocol.

A successful refresh replaces the single-use opaque refresh token; submitting
an already used token revokes that device session. Native clients submit the
token in the refresh request body. Browsers submit an empty JSON object and the
backend reads and rotates a scoped HttpOnly refresh cookie. The cookie is a
browser transport detail, not authorization for protected resources and not
part of the native contract.

Clients must keep the access token in memory. Native clients keep the refresh
token in secure device storage and replace it atomically after every successful
refresh. Browser clients rely on the backend's cookie rotation and must not
copy tokens into web storage. Every client must serialize refresh attempts per
device. Tokens, bearer headers, cookies, and passwords must never be logged.

If a v1 operation must be retired, it will first return standards-based
`Deprecation` and `Sunset` headers for at least 90 days. An incompatible change
requires a new major path such as `/api/v2`.

Interview `openingUsefulWords`, turn/candidate `usefulWords`, and the legacy
session-wide `usefulWords` mirror now allow up to 20 entries. New generation aims
for 20 relevant words or short phrases per question at the learner's profile level.
Older, shorter lists remain valid and are not regenerated. Manual microphone mute
is local capture state and adds no endpoint or session transition.

Recording feedback adds optional stable `id` and `span` fields. A span has
half-open UTF-16 `start` and `end` offsets; optional `turnSequence` makes them
relative to that stored learner answer rather than the full transcript.
Older feedback may omit locations. Clients must validate the quoted slice and
must not guess which repeated occurrence was erroneous.

Optional `strengthsStatus` distinguishes unknown legacy coverage, pending,
processing, ready (including an empty result), and failed positive-feedback
processing. `POST /api/v1/recordings/{recordingId}/strengths` schedules only good
examples; it never restarts corrections, rewriting, or shadowing. The operation
is owner-protected and returns an existing result while processing or ready.

Shadowing voices `correctedTranscript`, built from the learner's own answers and
immutable interview questions. `correctedAnswerText` retains its learner-correction
meaning. Rewriting uses the current profile level with only occasional, easy-to-repeat
phrasing from the next CEFR level, preserving learner facts and intent. The deprecated
optional `shadowingScript` identifies recordings from the discontinued fictional
sample experiment; no new samples are generated. The existing scheduling operation
replaces those experimental artifacts once, clearing that field. Ready learner audio
continues to reuse its cache. Free talk and photo description also use corrected
learner text.


Focused feedback adds optional `focusedFeedback` version 1 and optional immutable
question indexes to v1 recording and interview responses. Existing recordings
retain their original analysis; new recordings project blockers into `suggestions`
and up to three praises into `strengths` for older clients. Native tips and curated
three-point micro lessons are available through the new field. The current web account
client consumes only `focusedFeedback`; it does not parse or render the compatibility
`suggestions`, `strengths`, `strengthsStatus`, or discontinued `shadowingScript`.
Ready recordings without valid focused feedback offer explicit reanalysis through
the existing idempotent endpoint instead of an older feedback layout. Every new focus
uses mandatory one-based `occurrence` and exact answer-relative UTF-16 spans.
All confidently identified errors are returned as blockers, without a numeric cap
on errors or total items. Each retains its rule and explanation. At most one optional
praise and one native tip can accompany them per answer. Overlaps still resolve to
disjoint highlights and unverifiable anchors are rejected.

Optional `practiceContext` contains `originalText`, `correctedText`, and
`audioFeedbackId`. It derives a sentence or bounded excerpt from the learner's
canonical answer and changes only the selected anchored fragment. Clients use
that audio ID with the existing feedback audio resource to hear `correctedText`.
The original focus ID and legacy `practiceText` keep their existing meanings and
audio. Saved analyses gain context on read without reanalysis or overwriting
transcripts. Invalid anchors omit the new field. A context ID is stable for the
same text; identical legacy and contextual sentences share the original cache.
The current web instead shows and voices the separate illustrative `practiceText`
under “Пример применения”, using the original focus ID and its existing audio cache.

New audio resources support GET for state/link and POST for idempotent generation
or explicit failed-synthesis retry. Ready and processing POSTs are no-ops. A ready
response includes a short-lived protected media request; no private object key or
public URL is exposed. Clients begin each listen with GET and refresh an expired
link through GET without synthesis. Session question audio permits the owning
guest; saved question, feedback and attempt resources require the owning account.

`POST /api/v1/recordings/{recordingId}/feedback/reanalyze` explicitly replaces the
original analysis using a stable idempotency key. Existing attempts are preserved.
`/api/v1/recordings/{recordingId}/interview-turns/{sequence}/attempts` lists (limit
1–50, default 20, before=oldest returned ID) or creates separate saved attempts;
individual GET and POST retry resources expose their durable state. Creation
requires an owned ready `interview_attempt_audio` asset and a stable key in JSON
or `Idempotency-Key`. Original recordings and subsequent turns remain unchanged.
