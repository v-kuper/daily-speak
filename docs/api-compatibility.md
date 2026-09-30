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
