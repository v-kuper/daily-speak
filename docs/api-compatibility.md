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

The unversioned `/api/*` routes are the legacy web contract. They remain in
place while the web sandbox migrates, but new mobile clients must use
`/api/v1/*`. Mobile authentication uses short-lived Bearer access tokens and
single-use opaque refresh tokens. A successful refresh replaces the submitted
refresh token; submitting an already used token revokes that device session.
Protected recording routes also accept the existing cookie during the web
migration. Cookies are not part of the mobile contract.

Clients must keep the access token in memory and the refresh token in secure
device storage. They must serialize refresh attempts per device and replace the
stored refresh token atomically after every successful refresh. Tokens, bearer
headers, and passwords must never be logged.

If a v1 operation must be retired, it will first return standards-based
`Deprecation` and `Sunset` headers for at least 90 days. An incompatible change
requires a new major path such as `/api/v2`.
