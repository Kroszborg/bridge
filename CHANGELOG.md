# Changelog

All notable changes to Bridge are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Bridge uses
[Semantic Versioning](https://semver.org/): from 1.0, breaking changes to the REST API, the SDK
and the CLI come only with a new major version, and are called out here with migration steps.

## [Unreleased]

### Added

- Bot protection for the account forms, all optional. With `BRIDGE_TURNSTILE_SITE_KEY` and
  `BRIDGE_TURNSTILE_SECRET_KEY` (both or neither), sign-up and password-reset requests need a
  Cloudflare Turnstile token (`turnstile_token`), and so does sign-in after 3 failed attempts for an
  address or 5 from an IP in 15 minutes. `GET /v1/auth/config` has `turnstile_site_key`, and the
  dashboard renders the widget (themed, usually invisible) on those pages. Tokens are verified
  server-side with the client IP, hostname and per-form action. New error codes:
  `captcha_required`, `captcha_failed` and `captcha_unavailable`; sign-up and reset fail closed
  while Turnstile is unreachable, sign-in falls back to its rate limits.
- Sign-up and reset forms carry a hidden honeypot field; requests that fill it are dropped.
- `BRIDGE_BLOCK_DISPOSABLE_EMAIL` (default on with `BRIDGE_CLOUD`) refuses sign-ups and email changes
  to well-known disposable email providers with `422 email_not_allowed`.
- API responses carry `X-Robots-Tag: noindex`.

- Email verification: sign-up emails a 6-digit code (15 minutes, 5 attempts, stored hashed), and
  `POST /v1/me/email/verification` and `/v1/me/email/verification/confirm` send and check it. The
  dashboard shows a dismissible "Verify your email address" banner and a Verified badge under
  Account. Needs `BRIDGE_SMTP_*`.
- Changing your email address: `POST /v1/me/email/change` (needs your password) emails a code to
  the new address and a notice to the old one; `POST /v1/me/email/change/confirm` switches the
  account to it. Open password reset links stop working; sessions stay signed in.
- Phone verification for accounts, sent by Bridge's own Verify: set `BRIDGE_ACCOUNT_VERIFY_API_KEY`
  to an API key of one of your projects (and optionally `BRIDGE_ACCOUNT_VERIFY_APP`). A live key
  texts the code through that project's paired phones or providers, so it doubles as an end-to-end
  check of Verify; a test key sends nothing and returns the code. `POST /v1/me/phone/verification`,
  `/v1/me/phone/verification/confirm` and `DELETE /v1/me/phone`; a number can be verified on one
  account only.
- `User` has `phone` and `phone_verified`; `GET /v1/auth/config` has `email_verification` and
  `phone_verification`. New error codes: `invalid_code`, `code_expired`, `already_verified`,
  `email_in_use`, `phone_in_use`, `email_verification_unavailable` and
  `phone_verification_unavailable`.
- Insights for operators: `GET /v1/system/insights?days=30` (7 to 180 days) and a dashboard page
  under System health with accounts, workspaces, phones, sign-ups per day, live and test SMS per
  day, delivery and Verify rates, top failure codes, routes, the busiest workspaces, the newest
  accounts and, with `BRIDGE_CLOUD`, plan mix, estimated MRR and recent subscription changes. It is
  computed from Bridge's own database; no analytics service is involved. A migration adds indexes
  on `messages.created_at` and `otp_verifications.created_at`.

### Changed

- System health and Insights are hidden from anyone who is not an operator: `GET /v1/system` and
  `GET /v1/system/insights` answer them with the same `404 not_found` as an unknown route (System
  health returned `403 forbidden` before), and the dashboard shows its 404 page instead of an
  "operators only" notice.

## [1.0.0] - 2026-10-09

Bridge 1.0, the first stable release. It brings together everything built since the first Android
gateway: sending through your own phones or SMS providers, Verify, messaging tools, webhooks, the
SDK, the CLI and the MCP server. New in this release:

- **Hosted Bridge** at [bridge.kroszborg.co](https://bridge.kroszborg.co), running the same code as
  self-hosted servers, with Free, Pro ($5/month) and Team ($15/month) plans paid through Dodo
  Payments. Self-hosted servers have no plans or limits.
- **The Android app** can sign in to your account to pair without a QR code, and shows Messages,
  Send, Phones and Account tabs, a connection log and home-screen status widgets.
- **A daily send cap per phone** keeps SIMs within their operator's daily allowance.
- **Documentation site** at [bridge.kroszborg.co/docs](https://bridge.kroszborg.co/docs/), built from
  `docs/`, plus a privacy policy and terms.
- **Security hardening**: password reset by single-use, one-hour links that sign out every other
  session; billing webhooks verified and applied from the subscription's state re-read from Dodo; and
  a documented threat model in the [security model](docs/security/README.md).

### Added

- Password reset by email: `POST /v1/auth/password-reset` (never reveals whether an account
  exists) and `POST /v1/auth/password-reset/confirm`, which signs out every other session. Uses
  the `BRIDGE_SMTP_*` server. `GET /v1/auth/config` tells the sign-in pages what the server
  offers.
- Sign-in and sign-up pages: "Forgot password?", a password strength hint, a Caps Lock warning,
  a clear page when sign-ups are closed, and links to the terms and privacy policy when
  `BRIDGE_SITE_URL` is set.
- Hosted plans (`BRIDGE_CLOUD=true`): Free, Pro and Team with limits on phones, live SMS per
  month, projects and members, paid through Dodo Payments. Limits answer
  `402 plan_limit_reached`. Self-hosted installs are never limited. See
  [docs/hosted/billing.md](docs/hosted/billing.md).
- Dashboard: Send now has One message, Bulk (CSV) and Scheduled tabs; the sidebar is grouped
  by job; Devices is called Phones; Team, Audit log and Billing moved to the account menu.
- Android app: sign in with your Bridge account to pair without a QR code, see messages, send,
  manage phones and see the workspace plan. F-Droid metadata.
- Website: privacy policy and terms pages.
- A daily send cap per phone (`daily_send_limit`, default 100 in any 24 hours) keeps SIMs within
  their operator's daily SMS allowance. Dispatch skips full phones, broadcasts pace to what is left,
  and a message that no phone can take fails with `daily_limit_reached`. Set it per phone under
  Phones -> Settings.

- `bridgectl mcp`: a Model Context Protocol server on stdio (official Go SDK) so AI assistants
  can send SMS, run verifications and read message status with an API key. See
  [docs/mcp](docs/mcp/README.md).

- `GET /v1/request-logs` for API keys: the project's requests in the key's environment.
- SDK: `bridge.events.stream()` (live events with reconnects), `bridge.requestLogs.list()` and
  `listAll()`, and `bridge.usageHistory()`. The SDK is published to npm as `@kroszborg/bridge`.
- `bridgectl logs` with status, method and path filters.
- SMS providers: MSG91 (DLT templates through the Flow API, with separate OTP
  and message templates and configurable variables), Twilio (From number or Messaging Service),
  Vonage (SMS API) and Plivo, one account of each per project, tried in priority order. Retryable
  errors (timeouts, `429`, `5xx`) are retried up to 6 attempts; others fail the message with a
  `<provider>_<code>` error. A provider accepting a message counts as `sent`. See
  [docs/providers](docs/providers/README.md).
- Per-project routing: `phones` (default), `phones_then_providers` and `providers`, with
  `fallback_after_seconds` (default 60). A message falls back when there is no paired phone, no
  phone can take it in time, it reaches the queue timeout, a phone fails it for good (except
  `invalid_destination`), or phones do not accept it. Never for test keys or messages sent with a
  `device_id`.
- Provider delivery reports at `/v1/provider-callbacks/{id}/{token}`: Bridge passes the URL with
  each message to Twilio, Vonage and Plivo; for MSG91 you set it once as the delivery-report webhook.
  Reports move messages to `delivered` or `failed`.
- `BRIDGE_SECRET_KEY` (32 bytes, base64 or hex): encrypts provider credentials and integration
  secrets with AES-256-GCM, bound to their row. Without it, they cannot be saved.
- Provider management for owners and admins: add, edit, enable, prioritise, remove and check
  credentials without sending (`POST …/providers/{id}/check`). Credentials are never returned; a
  hint such as the Twilio account SID is shown. Audited as `provider.added`, `provider.updated`,
  `provider.removed` and `routing.updated`.
- Integrations, starting with the Supabase Auth Send SMS hook at
  `POST /v1/hooks/supabase/{integrationId}`: verifies Supabase's Standard Webhooks signature
  (`v1,whsec_…` secret) and sends Supabase's code with the project's Verify template, masked like
  Bridge's own codes. Live or test (simulator). Audited as `integration.created`, `.updated` and
  `.deleted`. See [docs/integrations](docs/integrations/README.md), with guides for Better Auth,
  Auth0, n8n, Zapier, Make, Firebase and Clerk.
- Message timeline events `provider_fallback` (with a `reason`) and `provider_accepted`.
- Docs: iPhones as recipients (code autofill) and why they cannot be gateways.
- Verify apps: up to 50 per project, each with its own name, message template,
  code settings, failover, fraud protection, widget, signing secret and statistics. The default app
  (slug `default`) holds the project's previous settings. `POST /v1/otp` takes `app` (ID or slug)
  and `client_ip` (the end user's IP address); `POST /v1/otp/verify` takes an optional `app`.
  Verifications show `app_id`. Audited as `verify_app.created`, `.updated`, `.deleted`,
  `.secret_revealed` and `.secret_rotated`.
- Verify delivery failover: a live code whose SMS was not sent within the app's
  `failover_after_seconds` (default 30, 0 turns it off), or whose send failed unambiguously, is
  resent once through the project's SMS providers or another online phone. Never after an
  ambiguous failure or once a phone is sending or has sent it. Verifications show
  `failover_message_id` and `failover_message_status`. See [docs/otp](docs/otp/README.md#delivery-failover).
- Verify fraud protection per app: allowed countries, codes per end-user IP per hour (default 10),
  per number range (default 20 per hour), and an optional per-country hourly cap. Refusals return
  `otp_blocked` (`403`, or `429` with `Retry-After`), are kept for 30 days in a blocked-attempt
  report, and are announced as the new `otp.blocked` webhook event.
- Verify drop-in widget and hosted page: a publishable key (`bpk_…`), allowed origins, exact
  redirect URIs, a test environment that returns the code, and optional Cloudflare Turnstile.
  Public endpoints under `/v1/widget/{publishableKey}`. A successful check returns a token (HS256
  JWT signed with the app's `bvs_…` secret; claims `iss`, `aud`, `sub`, `vid`, `env`, `iat`, `exp`,
  `jti`) that servers check locally or with `POST /v1/otp/tokens/verify`. See
  [docs/otp](docs/otp/README.md#drop-in-widget).
- SDK: `verifyWidgetToken()` checks widget tokens locally with WebCrypto (HS256 only, constant-time
  signature check, `aud`, `iss`, `exp` and `env`) and throws `BridgeTokenError` with the same
  reasons as the API; `bridge.otp.verifyToken()` calls `POST /v1/otp/tokens/verify`. The SDK's
  webhook types include `otp.blocked`, and `BridgeErrorCode` includes `otp_blocked`.
- Broadcasts: `POST /v1/broadcasts` sends one template with `{placeholders}`
  to up to 10,000 recipients, each with its own `vars`. Numbers are normalised, repeated numbers are
  sent once and opted-out numbers are skipped. `dry_run` previews unique recipients, opted-out and
  duplicate counts, total segments and the first 5 rendered messages without creating anything.
  `scheduled_at` starts it later (up to a year ahead). Messages go through the normal pipeline with
  `metadata.broadcast_id`, paced to the project's phone capacity (the sum of the phones' send limits,
  10 to 500 waiting at once; 500 with providers or test keys). Broadcast messages skip the
  per-message hourly limits; a project may create 20 broadcasts an hour. List, get with live counts,
  and cancel (`POST /v1/broadcasts/{id}/cancel`), which skips recipients not yet sent to and cancels
  messages still waiting for a phone (`error_code: canceled`). See
  [docs/broadcasts](docs/broadcasts/README.md).
- Scheduled messages: `POST /v1/schedules` sends a message once, daily, weekly (chosen weekdays) or
  monthly (day clamped to the month's end) at a wall-clock time in an IANA time zone. Daylight
  saving is handled (a skipped time runs as far past the gap, a repeated time runs once). Update,
  delete, pause, resume (missed runs skipped) and run now; `ends_at`; `last_error` explains a run
  that sent nothing. Runs missed while the worker was down are sent once, not replayed, and each run
  is idempotent. See [docs/schedules](docs/schedules/README.md).
- Opt-out list: `GET`, `POST /v1/opt-outs`, `GET` and `DELETE /v1/opt-outs/{number}` (`404` means
  the number may be messaged), plus a CSV export in the dashboard. Messages, broadcasts, schedules
  and phone forwards to an opted-out number are refused with the new error code `opted_out`
  (`409`); one-time passwords and auto-replies still go.
- Auto-reply rules (dashboard): exact, starts-with or contains keyword matching, a reply sent
  through the phone that received the SMS, and `opt_out` or `opt_in` actions, tried by priority.
  Every project starts with `STOP`/`UNSUBSCRIBE`/`CANCEL`/`END`/`QUIT`, `START`/`UNSTOP` and `HELP`
  rules. Loop protection: one reply per rule per number every 10 minutes, never to sender IDs, short
  codes or numbers without a country code, and Bridge's own forwards arriving back are ignored.
  Message timeline events `auto_reply`, `automation_skipped` and `canceled`.
- Forwarding rules (dashboard): copy incoming SMS matching senders (exact or prefix) and a keyword
  to up to 5 destinations: a phone number, a Telegram chat (bot token encrypted under
  `BRIDGE_SECRET_KEY`), a webhook signed with the rule's Standard Webhooks secret (JSON, Slack or
  Discord format), or email through SMTP (`BRIDGE_SMTP_HOST`, `_PORT`, `_USERNAME`, `_PASSWORD`,
  `_FROM`, `_TLS`). Failed deliveries are retried 8 times over about four hours, with a per-rule
  delivery log. See [docs/automation](docs/automation/README.md).
- Webhook and event-stream events `broadcast.completed` (the broadcast with final counts) and
  `message.auto_replied` (the incoming message, the rule, the keyword, the action and the reply).
- Audit log entries for broadcasts, schedules, opt-outs, auto-reply rules and forwarding rules
  changed in the dashboard.
- SDK: `bridge.broadcasts` (`create`, `preview`, `get`, `list`, `listAll`, `cancel`, `waitFor`),
  `bridge.schedules` (`create`, `get`, `list`, `listAll`, `update`, `delete`, `pause`, `resume`,
  `run`) and `bridge.optOuts` (`list`, `listAll`, `add`, `remove`, `isOptedOut`). Webhook types
  include `broadcast.completed` and `message.auto_replied`, and `BridgeErrorCode` includes
  `opted_out`. Broadcast and schedule creation and `schedules.run` are not retried automatically.
- CLI: `bridgectl broadcast send --csv FILE --template "Hi {name}"` (with `--dry-run`, `--at`,
  `--name`, `--device` and `--test`), `bridgectl broadcasts`, `broadcast get` and `broadcast cancel`,
  `bridgectl schedules`, and `bridgectl optouts` with `add`, `remove` and `check`. MCP tools
  `create_broadcast` (with `dry_run`), `get_broadcast`, `list_schedules` and `check_opt_out`.

### Changed

- The SDK package is renamed from the `@bridge/sdk` placeholder to `@kroszborg/bridge`.
- The dashboard's request log endpoint's operation ID is now `listProjectRequestLogs`;
  `listRequestLogs` is the API-key endpoint.
- A message's `provider` can now be `fallback` (waiting for an SMS provider) or `msg91`, `twilio`,
  `vonage` or `plivo`, besides `android` and `simulator`.
- Verify settings are now per app. The resend cooldown (30 seconds) and the hourly cap (5 codes per
  number) count per app, and a new code cancels only the same app's pending code. Migration
  `00010` moves each project's `otp_settings` into its default app and drops that table; the
  project's Verify settings endpoints now read and write the default app.
- The text of a Verify SMS is kept until its verification finishes, at most 1 hour, instead of
  being erased as soon as the phone is done with it, so failover can resend it. It stays masked
  everywhere.

### Fixed

- Incoming SMS from senders of 5 or 6 characters (short codes) crashed the phone's connection while
  the sender was masked for the logs.
- Verify failover now skips every phone that already had the message, not only the one it was last
  assigned to.
- Android: ambiguous send failures (such as `generic_failure` and unknown result codes) are no
  longer retried, because the carrier may already have accepted the SMS; retrying sent duplicate
  codes in a real-phone test. Only failures where nothing left the phone (radio off, no service,
  modem not ready, explicit retry) are retried. More radio (RIL) result codes are mapped to stable
  error codes, and the modem's cause code is included in the error message.

## [0.4.0-rc.1] - 2026-10-07

The first pre-release: everything from the Android gateway (v0.1) through inbound SMS and
webhooks (v0.2), the developer platform (v0.3) and the Verify API (v0.4). Bridge has not yet
been verified on enough real phones to call it stable, so this is a release candidate for
testing, not for production.

### Added

- Verify (v0.4): one-time passwords that Bridge generates, sends and checks. `POST /v1/otp` sends a
  code, `POST /v1/otp/verify` checks it (by number or verification ID), `GET /v1/otp/{id}` reads
  it. Codes expire (10 minutes by default), allow a limited number of attempts (5), can be resent
  after 30 seconds (which cancels the previous code), and are capped at 5 per number per hour.
  Codes are stored only as an HMAC under a per-installation key, erased when the verification
  finishes; OTP messages are always shown masked and their text is erased once sent.
- Per-project message template with `{code}`, `{app}` and `{minutes}`, code length, lifetime and
  attempts, plus autofill: an Android SMS Retriever hash per request, and a WebOTP domain line
  (`@example.com #123456`) for browsers and iOS.
- Test keys return the code in the response, so automated tests need no phone.
- Dashboard Verify page: conversion, median time to verify, a send-and-check playground, settings
  with a live preview and segment count, and recent verifications.
- `bridge.otp.send()`, `verify()` and `get()` in the TypeScript SDK; `bridgectl otp send`,
  `verify` (exits 1 when not valid) and `get`.
- Messages have a `purpose` (`message` or `otp`).
- Webhook and event-stream events `otp.verified`, `otp.failed` and `otp.expired`, carrying the
  verification without its code.
- Teams: invite people to an organization with single-use links (7 days, no email needed), roles
  (owner, admin, member) enforced by the API, role changes, removal and leaving, with at least one
  owner always kept. Sign-up through an invite joins that organization instead of creating a new
  workspace, and works even when `BRIDGE_ALLOW_SIGNUP=false`.
- Account security: change your name and password (other sessions are signed out), see every
  signed-in browser with its device and IP, sign out one or all others, and delete your account.
- Audit log page for owners and admins, with category, project and actor filters. Invites, role
  changes, member removal, password changes, session revocations and project deletion are audited.
- Project deletion by owners, confirmed by typing the project name. Paired phones are unpaired.
- Status monitoring: a public status page at `/status` on the dashboard with 90-day uptime per
  component (API, database, message processing, webhook delivery, and phones for information),
  backed by `GET /v1/status`. Operators (`BRIDGE_OPERATOR_EMAILS`, or the first account) get a
  System health page with processes, job queues, backlog, database size and retention.
- API servers and workers check in every 15 seconds, and the worker samples every component once a
  minute. Samples are kept for 90 days.
- Command palette (Ctrl+K or ⌘K) to jump to any page or project and run common actions.
- Getting-started checklist on the project overview, with a webhook step.
- Pairing QR codes are drawn with Rune and carry the Bridge mark; scans were verified with a
  decoder at both display sizes.
- Releases: a tag builds `bridgectl` and `bridge` for Linux, macOS and Windows (GoReleaser),
  multi-architecture images on GHCR and Docker Hub, and signed Android APKs, all with build
  provenance attestations and checksums. `BRIDGE_IMAGE_PREFIX` makes Compose run a published
  release. See [docs/releasing.md](docs/releasing.md).
- `apps/web`: the public website, a static Next.js export with real console screenshots, code
  samples and an honest list of limits. Configure it with `NEXT_PUBLIC_DASHBOARD_URL` and
  `NEXT_PUBLIC_REPO_URL`.

- Request logs: every developer API request (method, path, status, error code, duration, key, IP,
  user agent, and the message it created or read) is recorded asynchronously in batches. No bodies,
  headers or query strings. Kept for `BRIDGE_REQUEST_LOG_RETENTION` (default 14 days). Dashboard
  Logs page with filters.
- Usage history: `GET /v1/usage/history` and the dashboard Usage page with daily charts (outgoing
  by status, incoming, API requests and errors, p95 latency), per-phone breakdown, a table view, and
  day boundaries in your time zone.
- Playground: send test or live messages from the dashboard, with simulator numbers, a live segment
  counter, the live timeline, and the equivalent curl, TypeScript and CLI code.
  `POST /v1/projects/{id}/messages` backs it.
- Event stream: `GET /v1/events/stream` (Server-Sent Events, API-key authenticated) delivers the
  same events as webhooks, live, across API instances.
- `bridgectl` CLI: `login`, `whoami`, `send [--wait]`, `messages [get|tail]`, `devices`, `usage`, and
  `listen --forward-to` to develop webhook handlers locally with correctly signed requests.

- Webhooks in the Standard Webhooks format: up to 10 endpoints per project, per-endpoint
  `whsec_` signing secrets (reveal, rotate), event subscriptions, test events, and a delivery log
  with status, response and latency for every attempt.
- Events: `message.sent`, `message.delivered`, `message.failed`, `message.received`,
  `device.online`, `device.offline` (after a 2-minute grace period).
- Webhook retries for about 3 days (5 s, 5 min, 30 min, 2 h, 5 h, then every 10 h), with jitter.
  Endpoints failing for 5 days are disabled and show why. Webhook delivery is SSRF-safe;
  `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS` allows endpoints on your own network.
- Incoming SMS: per-device "Forward incoming SMS" setting (off by default). The Android app
  forwards received SMS (multipart joined, SIM slot reported) and keeps them until the server
  confirms, and the server de-duplicates resends. Incoming SMS are stored as `inbound` messages.
- `GET /v1/messages` filters `direction` and `from`. Messages have a `from` field.
- Dashboard: Webhooks pages, an Incoming/Outgoing filter on Messages, and the forwarding setting
  on devices.
- `devicesim -inbound-every 20s` simulates incoming SMS.
- TypeScript SDK (`packages/sdk`, MIT): `bridge.messages.send/get/list/listAll/waitFor`,
  `bridge.devices.list/get/test`, `usage()`, `whoami()`, typed errors, safe retries with automatic
  idempotency keys, timeouts and cancellation, and `verifyWebhook` (WebCrypto, Standard Webhooks).
  Zero dependencies; runs on Node.js 20+, Bun, Deno and edge runtimes.
- Node.js examples: send and follow a message, and a verifying webhook receiver.

- Go API server, worker and migrations in one binary (`bridge serve`, `worker`, `migrate`,
  `openapi`, `healthcheck`).
- PostgreSQL schema for users, sessions, organizations, projects, API keys, devices, pairing tokens,
  messages, message events and audit logs.
- River (Postgres) job queue with a periodic maintenance job.
- Email and password accounts with argon2id hashing and cookie sessions.
- Organizations and projects, with a workspace and default project created at sign-up.
- Project API keys (`bk_live_` / `bk_test_`) with checksums, expiry, revocation and `GET /v1/whoami`.
- Fixed-window rate limits on sign-up, login and API-key requests.
- Structured error envelope with stable codes and request IDs, plus OpenAPI 3.1 at `/openapi.json`
  and an interactive reference at `/docs`.
- Message status state machine (`internal/message`).
- Next.js dashboard: sign-in, sign-up, project overview, API keys, project settings, account, with
  dark (default) and light themes.
- Docker images and a Compose file for self-hosting.
- Android gateway app (`android/gateway`, Kotlin + Compose, Android 8.0+) in two builds: `foss`
  (UnifiedPush, no Google code) and `gms` (Firebase Cloud Messaging).
- Device pairing: single-use, 10-minute pairing codes shown as a QR code in the dashboard; the app
  always confirms the server before trusting it; re-pairing rotates the credential.
- Gateway WebSocket (`/v1/device/connect`) with adaptive heartbeats, presence tracking, connection
  replacement and cross-instance delivery through PostgreSQL `LISTEN/NOTIFY`.
- Background reliability: foreground service, 15-minute WorkManager check-ins over HTTP, restart
  after reboot, and push wake-ups through UnifiedPush (WebPush, RFC 8291/8292) or FCM HTTP v1.
- Dashboard Devices page: live presence, battery, network and app details, offline diagnosis,
  wake, rename and remove.
- Developer API: `GET /v1/devices`, `GET /v1/devices/{id}`.
- `cmd/devicesim`, a simulated phone for developing without an Android device.
- Sending SMS: `POST /v1/messages` with idempotency keys, metadata, optional `device_id` and
  `sim_slot`; `GET /v1/messages` (cursor pagination, filters) and `GET /v1/messages/{id}` with the
  delivery timeline; `GET /v1/usage`; `POST /v1/devices/{id}/test`.
- Dispatch on River: picks an online phone under its send limit (prefers charging, Wi-Fi and the
  least-used allowance), wakes offline phones, reassigns unaccepted jobs, retries retryable failures
  up to 3 attempts, and never reports `delivered` without a carrier delivery report.
- Per-device sending settings: preferred SIM and a send limit (default 30 per 30 minutes, matching
  Android's approval threshold).
- Test-mode simulator with deterministic test numbers.
- GSM-7/UCS-2 segment counting.
- Message bodies are redacted after `BRIDGE_MESSAGE_RETENTION` (default 30 days).
- Android: sends through the chosen SIM (multipart supported), reports accepted, sent, failed and
  delivered, stores reports until acknowledged, and de-duplicates jobs by message and attempt.
- Dashboard: Messages page with live status and timelines, overview health stats, per-device SIM
  and send-limit settings, test sends.

### Changed

- Creating or revoking API keys, pairing, changing or removing phones, managing webhooks and
  projects, and live sends from the playground now need the admin or owner role. Members can see
  everything but change nothing.
- IDs are strictly increasing within a millisecond, so timelines always list events in order.
- Dashboard tabs are titled after the page.
- `GET /v1/system` reports retention as `message_bodies_seconds` and `request_logs_seconds`
  instead of Go duration strings.
