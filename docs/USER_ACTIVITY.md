# Cumulative user activity

The profile shows lifetime speaking time and a contribution calendar, without
streaks, rewards for consecutive days, or penalties. `internal/activity` owns
validation, intensity, overlap policy and the permanent activity ledger.

## Accounting

- Speaking means recording an answer (including local repetition or a saved
  retake), or playing an interviewer question. Microphone mute, question loading
  and turn-boundary waits do not earn recording time.
- Review means visible time on a ready recording's focused-feedback screen.
  Trusted pointer, keyboard, scroll and input interactions on that screen renew
  a 120-second idle deadline. Review then pauses until another interaction.
  Returning to a tab does not renew the deadline.
- Speaking takes priority over simultaneous review in the current web client.
  The backend unions overlapping intervals across tabs and devices. For
  concurrent categories, the first accepted interval keeps its category.
- Hidden pages do not earn time. A timer delayed by more than 30 seconds is
  discarded, so browser suspension cannot create unattended practice time.
- The large counter sums **speaking** time over the account's entire history.
  Calendar intensity sums speaking and review: no time = 0, positive time up to
  5 minutes inclusive = 1, more than 5 and less than 15 minutes = 2, and 15 minutes
  or more = 3. Sub-minute practice earns level 1 and displays “меньше минуты”.

`activity_events` stores original requests for permanent idempotency.
`activity_credits` stores their uncovered intervals. Acceptance is serialized
by a user-row lock; a failed batch rolls back entirely. Neither table references
recordings or interview artifacts. Deleting a recording cannot reduce the
counter. There is no statistics reset endpoint. Account deletion follows the
existing identity lifecycle and removes account-owned data.

Migration 0023 preserves pre-feature recordings once using their stored duration
(bounded to the existing 600-second recording limit). This is a **historical
estimate**: old whole-session audio can include pauses and does not distinguish
question listening, speech or review. The profile identifies this limitation.
Historical review time and previously deleted recordings cannot be reconstructed.
New activity is measured independently of saving or processing a recording.

## Shared web/native contract

`GET /api/v1/profile/activity?timezone=Europe/Minsk` requires an account Bearer
token. It returns lifetime speaking milliseconds, the historical portion, and
366 calendar days including today. Intervals crossing midnight are split in the
requested IANA timezone, including daylight saving transitions. The web uses
the browser timezone, displays the full window on desktop and the last 24
Monday-aligned weeks on screens up to 640 px wide. Missing/future grid cells are
blank. Day buttons expose the speaking/review breakdown on hover, keyboard
focus or tap. Failed reads show retry instead of fabricated empty statistics.

`POST /api/v1/profile/activity` accepts `{ intervals: [...] }`. Each interval has
`id`, `kind` (`speaking` or `review`), `startedAt`, and `endedAt` (UTC RFC3339).
Intervals are positive and at most 30 seconds, their end cannot exceed server
time by more than 10 seconds, and they cannot predate account creation. The API
accepts 1–100 intervals within its 16 KiB body limit; the web sends at most 50.
Repeated identical keys are no-ops; changed data returns `activity_conflict`.
Invalid input or timezone returns `invalid_activity`. This lightweight telemetry
trusts the client's activity classification; it is not a provider-verified
speech measurement or a reward/payment authority.

The web settles every 15 seconds and on source/visibility/page transitions.
It keeps a per-account, per-event retry outbox in browser storage, with an
in-memory fallback when storage is unavailable. No auth tokens enter storage.
Failed/uncertain sends retain their original keys; online recovery and profile
opening drain the outbox. Token refresh verifies the original principal before
retrying, preventing one account's activity from reaching another account.
Browser storage is a temporary retry buffer; PostgreSQL is the source of truth.
Abrupt crashes can lose the unsettled fraction of the current 15-second chunk;
clearing browser storage can lose unsent telemetry. Already accepted time is
permanent. Guests and older native clients must adopt interval telemetry for
new activity to appear; migration only covers recordings existing at rollout.

## Verification

Service and web-clock tests cover boundaries, idle pause/resume, hidden pages,
overlap, short contributions, leap day and mobile calendar alignment. PostgreSQL
integration tests cover concurrent devices, retries, account isolation, atomic
rollback, local midnight and historical time surviving recording deletion.
Run them with `TEST_DATABASE_URL` pointing to an isolated test database.
