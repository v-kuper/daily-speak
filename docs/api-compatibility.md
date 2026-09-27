# DailySpeak API compatibility policy

The mobile API uses a major version in its path, starting with `/api/v1`.
Within one major version, existing fields, meanings, status codes, and error
codes are not removed or repurposed. New optional response fields and new
endpoints may be added; clients must ignore fields they do not recognize.

List cursors are opaque. Clients must send them back unchanged and must not
construct or persist them as durable identifiers. Page limits are bounded by
the server.

Every response carries `X-Request-ID`. Clients should include it in support and
diagnostic reports. A caller may supply a safe `X-Request-ID`; otherwise the
server creates one.

The unversioned `/api/*` routes are the remaining legacy web sandbox contract.
All protected routes use the same short-lived Bearer access token as
`/api/v1/*`, but new mobile clients must use `/api/v1/*` exclusively. Missing
mobile feature operations are added to v1 when the mobile product needs them;
legacy routes are not promoted into the native contract.

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
