# Changelog

All notable changes to Bridge are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Bridge uses
[Semantic Versioning](https://semver.org/). Before 1.0, minor versions may contain breaking changes;
they are always called out here with migration steps.

## [Unreleased]

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
