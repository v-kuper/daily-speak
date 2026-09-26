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
`/api/v1/*`. Protected v1 routes temporarily accept the existing session cookie.
Bearer access and refresh tokens will be added as an additional authentication
method in the identity epic.

If a v1 operation must be retired, it will first return standards-based
`Deprecation` and `Sunset` headers for at least 90 days. An incompatible change
requires a new major path such as `/api/v2`.
