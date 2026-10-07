# Era 0 plan — Bridge Gateway

Status: **Milestones 1–3 complete; v0.1 acceptance pending a real-phone run** · Last updated: 2026-10-04

This document is the working plan for Era 0 (the v0.1 release). The long-term product
specification lives in the project brief; this file records what we are actually building
now and every technology decision behind it.

## 1. Repository assessment

`C:\coding\bridge` was empty at the start of Era 0. There is no prior code to preserve,
so everything below is greenfield. Two sibling projects were used as reference only:

| Reference | Used for |
| --- | --- |
| `paizy/paizy-admin` | Dashboard layout patterns: sidebar, top bar, page header, stat cards, tables, theme handling (Tailwind v4 + shadcn) |
| `kroszkit/kroszkit-web` | Dodo Payments integration pattern, recorded in [`future-billing.md`](./future-billing.md) for Era 1. **Not implemented in Era 0.** |

## 2. Decisions

Every choice below was confirmed with the maintainer before implementation.

| Area | Decision | Why |
| --- | --- | --- |
| Backend language | **Go 1.27** | Bottleneck is the SIM, not the CPU. Go gives cheap concurrent device WebSockets, one static binary, small images, low memory, and a large OSS infra contributor pool. |
| HTTP + contract | **Huma v2 on chi** | Code-first: Go structs define requests, responses and validation; OpenAPI 3.1 is generated, then TypeScript types for the dashboard and SDK are generated from it. |
| Database | **PostgreSQL 18** | Boring, reliable, one stateful dependency. |
| DB access | **pgx v5 + sqlc** | Plain SQL in `.sql` files, type-safe generated Go, no ORM magic. |
| Migrations | **goose** | Plain SQL up/down files embedded in the binary. |
| Jobs / queue | **River** (Postgres) | A job is inserted in the same transaction as the message row it belongs to, so a message can never exist without its job or vice versa. Built-in retries, uniqueness, periodic jobs. |
| Redis | **Not used in Era 0** | Self-hosting is one binary + Postgres. Rate limits live behind an interface (Postgres implementation today); Redis can be added later for very high volume. |
| Packaging | **One binary, subcommands** | `bridge serve`, `bridge worker`, `bridge migrate`, `bridge openapi`. `bridge serve --worker` runs both for tiny installs. |
| Dashboard | **Next.js 16, React 19, Tailwind v4, shadcn/ui (Radix)** | Layout language adapted from Paizy admin. |
| Frontend language/tooling | **TypeScript 7.0, Biome, pnpm 12, Turborepo** | TS 7 is the native compiler (verified to work, including Next.js builds). Biome replaces ESLint because typescript-eslint does not support TS 7 yet. The OpenAPI → TS generator also needs the old TS JS API, so it lives in `tools/openapi-codegen` pinned to TS 5.9; nothing we ship or type-check uses it. |
| Node runtime | **Node 24 LTS** | Current Active LTS for the dashboard container. |
| Android | **Kotlin, Jetpack Compose, manual DI, API 26+ (target 37)** | Covers old spare phones. Two flavors: `foss` (UnifiedPush, 100% FOSS, GitHub Releases) and `gms` (adds FCM). Foreground-service WebSocket + WorkManager refresh + push wake-up; adaptive heartbeats for battery. |
| License | **AGPL-3.0** (server, dashboard, Android) · **MIT** (SDK) | Fully open source; protects the hosted offering from closed forks. The SDK stays permissive so it can be embedded anywhere. |
| Brand | **Phosphor** accent, **Carbon** neutrals, **Red Hat** type, round corners | Chosen in the brand lab. Dark mode is the default; light mode is fully designed. |

## 3. Architecture

```text
              Developer app / curl / SDK            Browser
                         │  Bearer bk_live_…           │ cookie session
                         ▼                             ▼
                ┌───────────────────┐        ┌──────────────────┐
                │   bridge serve    │◄───────│  Next.js dashboard│ (proxies /api/* → API)
                │  REST /v1 (Huma)  │        └──────────────────┘
                │  device WebSocket │◄─────────── Android gateway (milestone 2)
                └─────────┬─────────┘
                          │ SQL + River insert (same tx)
                          ▼
                ┌───────────────────┐        ┌──────────────────┐
                │    PostgreSQL     │◄──────►│  bridge worker   │ River jobs: dispatch,
                │ data + job queue  │        │                  │ retries, webhooks, cleanup
                └───────────────────┘        └──────────────────┘
```

* **API (`bridge serve`)** handles REST, dashboard sessions and, from milestone 2, device WebSockets.
* **Worker (`bridge worker`)** runs River jobs. Periodic maintenance (expired sessions,
  rate-limit counters, pairing tokens) runs here from day one.
* **Job → device routing** (milestone 2): the worker cannot hold the device socket, so
  it publishes `pg_notify('device_jobs', device_id)`. Whichever API instance holds that
  device's socket wakes up, claims queued messages and pushes them. Devices that are
  offline pick up their queue on reconnect, and FCM can wake them.
* **Provider abstraction**: dispatch goes through a `Provider` interface. Era 0 ships
  `android` (and a zero-cost `simulator` used by test-mode keys). MSG91, Twilio and others
  plug in later without touching message or OTP code.

### Request lifecycle (milestone 3)

```text
POST /v1/messages
  → validate + normalize E.164 → rate limits (IP → key → project → destination)
  → BEGIN; insert message(status=queued) + message_event; River insert dispatch job; COMMIT
  → worker picks device → pg_notify → API pushes over WebSocket
  → device ACKs → status sending → device reports sent/failed → delivery report → delivered
```

All status changes go through one state machine (`internal/message/state.go`); every
transition writes a `message_events` row, which is the timeline the dashboard shows.

## 4. Repository structure

```text
bridge/
├── apps/
│   ├── api/                  Go module: cmd/bridge + internal/*
│   │   ├── cmd/bridge/       entrypoint and subcommands
│   │   ├── internal/
│   │   │   ├── config/       env parsing + validation
│   │   │   ├── db/           pool, embedded goose migrations, sqlc queries + generated code
│   │   │   ├── httpapi/      router, middleware, error model, route registration
│   │   │   ├── auth/         passwords, sessions, API keys, principals
│   │   │   ├── ratelimit/    limiter interface + Postgres implementation
│   │   │   ├── worker/       River client + jobs
│   │   │   ├── message/      message state machine
│   │   │   └── id/           prefixed, sortable IDs
│   │   └── sqlc.yaml
│   └── dashboard/            Next.js app
├── packages/
│   └── api-types/            generated OpenAPI document + TypeScript types
├── android/gateway/          Kotlin gateway (milestone 2)
├── docker/                   Dockerfiles
├── docs/                     architecture, security, self-hosting, providers
├── examples/                 curl first; SDK examples arrive with the SDK
└── docker-compose.yml        self-hosted stack
```

## 5. Database schema (migration `00001_init.sql`)

| Table | Purpose / notable columns |
| --- | --- |
| `users` | `email` (unique, case-insensitive), `password_hash` (argon2id PHC string), `email_verified_at` |
| `sessions` | `token_hash` (SHA-256 of the cookie token), `expires_at`, `last_seen_at`, `ip`, `user_agent` |
| `organizations` | `name`, `slug` |
| `organization_members` | `role` (`owner`, `admin`, `member`), unique per org + user |
| `projects` | `organization_id`, `name`, `slug` |
| `api_keys` | `environment` (`live`/`test`), `key_prefix` (display only), `key_hash` (SHA-256), `last_used_at`, `expires_at`, `revoked_at` |
| `devices` | `installation_id` (random per install, never a hardware ID), `credential_hash`, `status`, heartbeat, battery, network, carrier, Android + app version |
| `device_pairing_tokens` | short-lived (10 min), single-use, hashed |
| `messages` | `direction`, `status`, `environment`, `provider`, `device_id`, recipient, body, segments, error code/message, `metadata` jsonb, `idempotency_key` (unique per project), timestamps per state |
| `message_events` | append-only timeline: `type`, `from_status`, `to_status`, `detail` |
| `audit_logs` | actor, action, target, metadata, IP |
| `rate_limit_counters` | UNLOGGED fixed-window counters |

River manages its own tables (`river_job`, …) through its migrator, which `bridge migrate` also runs.

**Deviation from the brief:** `environment` lives on API keys and messages (Stripe-style
`live`/`test`) instead of on the project. One project then holds both environments, and
test keys can never send a real SMS.

## 6. API contract

* Base path `/v1`, JSON only, snake_case fields, prefixed IDs (`msg_01j…`, `dev_…`, `prj_…`).
* Errors: `{"error": {"code": "…", "message": "…", "details": [...], "request_id": "req_…"}}`.
  Codes: `invalid_request`, `validation_failed`, `unauthenticated`, `forbidden`, `not_found`,
  `conflict`, `rate_limited`, `internal_error`.
* Every response carries `X-Request-Id`. 429 responses carry `Retry-After`.
* OpenAPI 3.1 at `/openapi.json`, interactive reference at `/docs`.
* Auth: `Authorization: Bearer bk_<env>_…` for developer endpoints; `bridge_session`
  HttpOnly cookie for dashboard endpoints. The two never mix on one route.

| Milestone | Endpoints |
| --- | --- |
| 1 Foundation | `POST /v1/auth/signup`, `/login`, `/logout`, `GET /v1/me`, organizations, projects, API keys (create / list / revoke), `GET /v1/whoami` (key check), `/healthz`, `/readyz` |
| 2 Gateway | `POST /v1/projects/{id}/pairing-tokens`, `POST /v1/devices/pair`, `GET /v1/devices/connect` (WebSocket), devices list/detail |
| 3 Messages | `POST /v1/messages`, `GET /v1/messages`, `GET /v1/messages/{id}`, `GET /v1/devices`, `GET /v1/devices/{id}`, `POST /v1/devices/{id}/test`, `GET /v1/usage` |
| 4 (v0.2) | inbound SMS, webhooks, SDK |

## 7. Android communication

```text
pair:     scan QR (contains API URL + 10-min single-use pairing token)
          → POST /v1/devices/pair → long-lived device credential (stored in Android Keystore)
connect:  WebSocket /v1/devices/connect, credential in the first frame (never in the URL)
loop:     heartbeat every 30s (battery, network, signal) ⇄ server pushes `send_sms` jobs
send:     device ACKs job id → SmsManager → sent PendingIntent → report `sent`/`failed`
          → delivery PendingIntent → report `delivered`
recover:  exponential backoff reconnect; on reconnect the server replays unacknowledged
          jobs; jobs carry the message ID, so the device de-duplicates and never sends twice
wake:     FCM high-priority data message when a job is queued for an offline device
```

## 8. Security model

* Passwords: argon2id (m=19 MiB, t=2, p=1), with a cap on concurrent hashes to bound memory use.
* API keys, device credentials, session and pairing tokens: 256-bit random, only SHA-256 stored.
  API keys include a CRC32 checksum, so malformed keys are rejected without a database lookup and secret scanners can detect them.
* Project isolation: every query for project resources includes `project_id`; session
  routes resolve membership first and return `404` (not `403`) for other tenants' IDs.
* CSRF: session cookies are `HttpOnly`, `SameSite=Lax`, and `Secure` behind HTTPS.
  Unsafe methods with a cookie must have an allowed `Origin`.
* No CORS. Secret keys are server-side only.
* Rate limits: per IP on auth endpoints and per key on the developer API, with project and destination limits arriving alongside messages.
* Logs never contain secrets, tokens, passwords, OTPs or message bodies; recipients are masked.
* Audit log for key creation/revocation, device pairing/removal and project changes.
* Security headers on every response; request bodies capped at 1 MiB.

## 9. Testing strategy

* **Go unit tests**: IDs, key format and checksum, password hashing, message state machine, config.
* **Go integration tests** against real Postgres (`BRIDGE_TEST_DATABASE_URL`). Each run creates a throwaway
  database and covers signup/login/logout, session expiry, org/project isolation, API key lifecycle,
  revoked/expired keys, rate limiting, CSRF origin checks, and (later) queue processing, retries and duplicate prevention.
* **Dashboard**: type check (TS 7), Biome lint, production build.
* **Android** (milestone 2): unit tests for pairing, reconnect, de-duplication and permission states; instrumented SMS test on a real device.
* **End-to-end**: the v0.1 acceptance script in §10, run against `docker compose up`.

## 10. Milestone checklist

**Milestone 1 — Foundation** ✅ complete (2026-10-04)
- [x] Monorepo: pnpm workspace, Turborepo, Biome, TS 7, Go module
- [x] Postgres via Docker Compose, schema migration, River migrations
- [x] Config, structured logging, request IDs, error model, security headers
- [x] Auth: signup, login, logout, sessions, CSRF origin check, rate limits
- [x] Organizations, projects, API keys (create once-shown secret, list, revoke), `whoami`
- [x] Worker with periodic maintenance job
- [x] Generated OpenAPI → TypeScript types
- [x] Dashboard: auth pages, shell (sidebar, top bar, theme), overview, API keys, settings, account
- [x] Docker images + `docker compose up` self-host path
- [x] README, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, self-hosting docs, LICENSE

**Milestone 2 — Android gateway** ✅ complete (2026-10-04)
- [x] Pairing tokens + QR, device credential (Keystore-encrypted on the phone), server confirmation before trust
- [x] WebSocket protocol v1, adaptive heartbeats, presence, replacement, cross-instance delivery
- [x] Background refresh (WorkManager), boot restart, push wake-ups (UnifiedPush / FCM), SSRF-safe push client
- [x] Dashboard Devices page, developer device endpoints, device simulator
- [ ] Verified on a physical phone (deferred by the maintainer; build, lint and unit tests only so far)

**Milestone 3 — Messages** ✅ complete (2026-10-04)
- [x] `POST /v1/messages` with validation, idempotency, rate limits; list/detail with timeline; usage
- [x] River dispatch with smart device selection, send-limit pacing, push wake, reassignment, retries
- [x] Device protocol: send_sms, accepted/sent/failed/delivery reports with acknowledgements
- [x] Duplicate prevention: per-attempt de-duplication on the phone, conditional assignment on the server
- [x] Test-mode simulator, segment counting, body redaction after retention
- [x] Android SMS sending (SIM choice, multipart, delivery reports, durable outbox)
- [x] Dashboard Messages page, overview stats, device SIM/limit settings, test send
- [ ] Verified on a physical phone

**Milestone 4 (v0.2):** inbound SMS, webhooks, TypeScript SDK, examples.

- [x] Standard Webhooks signing (`whsec_` secrets, `webhook-id`/`-timestamp`/`-signature`), verified against the spec's test vector
- [x] Endpoints CRUD, event subscriptions, secret reveal/rotate (audited), test events, 10 per project
- [x] River delivery queue: ~2.8 days of backoff with jitter, per-attempt delivery log, disable after 5 days failing
- [x] SSRF-safe delivery (public addresses only unless `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS`)
- [x] Events: message.sent/delivered/failed/received, device.online/offline with a 2-minute grace period
- [x] Inbound SMS: per-device opt-in pushed to the phone (`config` frame, welcome, HTTP check-in), `sms_received` frames kept until acknowledged, server-side de-duplication and rate limit
- [x] Android: SMS_RECEIVED receiver, multipart joining, SIM slot, permission requested only when forwarding is on
- [x] Dashboard: Webhooks list/detail, Incoming filter on Messages, forwarding setting on devices
- [x] TypeScript SDK: zero-dependency fetch client, typed from OpenAPI (types inlined), ESM via tsdown, WebCrypto webhook verification, Vitest unit + integration tests
- [x] Node.js examples (send and follow, webhook receiver)
- [ ] Incoming SMS verified on a physical phone

**Milestone 5 (v0.3):** developer platform.

- [x] Request logs: async batched recorder (COPY, row-by-row fallback), metadata only, 14-day retention, dashboard Logs page
- [x] Usage history API (daily buckets in the caller's time zone, per-device breakdown, request volume and p95) and dashboard Usage page (Recharts, validated palette, table view)
- [x] Playground: session-authenticated send in test or live, segment counter mirroring the server, code samples
- [x] Event stream (SSE) over LISTEN/NOTIFY; oversized events stored and announced by ID
- [x] `bridgectl` CLI (Go, stdlib + x/term): login, send, messages, tail, devices, usage, listen/forward
- [x] Release pipeline: GoReleaser binaries (bridgectl, bridge), multi-arch images on GHCR and Docker Hub, signed APKs, provenance attestations
- [ ] First tagged release (v0.3.0) run end to end on GitHub

**Milestone 7 (v0.4):** Verify API.

- [x] `otp_verifications` and `otp_settings`; codes as HMAC-SHA256 under a per-installation key, erased when finished
- [x] Send (cooldown, hourly cap, cancels the previous code), verify by number or ID (row lock, constant-time compare, attempts), expiry in maintenance
- [x] OTP messages: `purpose`, masked `display_body` everywhere, body erased once sent
- [x] Templates with `{code}`, `{app}`, `{minutes}`; Android SMS Retriever hash; WebOTP domain line
- [x] Test keys return the code; test and live verifications are separate
- [x] SDK `bridge.otp`, `bridgectl otp`, dashboard Verify page, docs
- [x] SDK parity: event stream (reconnecting SSE iterator), request logs (`GET /v1/request-logs` for keys), usage history; `bridgectl logs`
- [x] npm publishing of `@kroszborg/bridge` from the release workflow (trusted publishing, `next` for pre-releases)
- [ ] Verified on a physical phone, including Android autofill

**Milestone 6 (v0.3):** teams, account security and operations.

- [x] Roles (owner, admin, member) enforced in the API through operation metadata; the dashboard hides what a role cannot do
- [x] Invite links (single-use, 7 days, hashed), sign-up through an invite even with sign-ups off, last-owner protection
- [x] Account: name, password change (signs out other sessions), sessions with user agent, account deletion rules
- [x] Audit log page with filters; project deletion by owners with typed confirmation
- [x] Status: process check-ins, minute samples kept 90 days, public `/status`, operator System health
- [x] Command palette, onboarding checklist, Rune pairing QR, page titles
- [x] Public website (`apps/web`, static export)
- [x] Browser pass in Chrome over every new page, in dark and light mode

**Milestone 8 (v0.5):** SMS providers and integrations.

- [x] Provider layer: MSG91 (Flow API v5, DLT OTP and message templates, configurable variables, sender ID), Twilio (From or Messaging Service, `StatusCallback`), Vonage (SMS API, `callback`), Plivo (Message API, `url`); clients never retry, errors are retryable or permanent with `<provider>_<code>` codes
- [x] `BRIDGE_SECRET_KEY` and `secretbox`: AES-256-GCM, row-bound, credentials write-only with a display hint
- [x] Routing per project (`phones`, `phones_then_providers`, `providers`, `fallback_after_seconds`); fallback on no paired phone, no phone in time, queue timeout, final phone failure (except `invalid_destination`), unresponsive phones; never for test keys or `device_id`
- [x] Provider send job: priority order, up to 6 attempts while errors are retryable, accepted = `sent`, timeline events `provider_fallback` and `provider_accepted`
- [x] Delivery-report callbacks at `/v1/provider-callbacks/{id}/{token}` (constant-time token check) for all four providers, including MSG91's numeric statuses
- [x] Provider management API (add, update, remove, check credentials), routing API, audit entries
- [x] Integrations: Supabase Send SMS hook (Standard Webhooks verification, Verify template, masked OTP messages, live or test)
- [x] Android: ambiguous send failures are not retried (duplicate prevention); RIL result codes mapped; modem cause code reported
- [x] Docs: providers, integrations (Supabase, Better Auth, Auth0, n8n/Zapier/Make, Firebase and Clerk), iPhones
- [ ] Real provider accounts tested end to end (send, delivery report, failure codes) for MSG91, Twilio, Vonage and Plivo
- [ ] MSG91 delivery-report format confirmed with a live account (`requestId` matching the Flow API's request ID)
- [ ] Supabase hook tested against hosted Supabase and the Supabase CLI
- [x] Ambiguous phone failures do not fall back to a provider (the SMS may already be out); only failures that prove nothing was sent do

**Milestone 9 (v0.6):** Verify Pro.

- [x] Verify apps per project (template, code settings, limits, stats); the default app keeps today's settings (API, migration `00010`)
- [x] Delivery failover: resend the same code through providers or another phone when it was not sent within N seconds; never after ambiguous failures
- [x] Fraud protection: allowed countries, per-IP and number-range limits, per-country cap, blocked-attempt report (API, kept 30 days), `otp.blocked` event
- [x] Widget API: publishable key, allowed origins, exact redirect URIs, test environment, Turnstile, public `/v1/widget/{key}` endpoints, HS256 token signed with the app secret, `POST /v1/otp/tokens/verify`
- [x] SDK `verifyWidgetToken` and `bridge.otp.verifyToken`; docs for apps, failover, fraud protection, widget and tokens
- [x] Dashboard: Verify apps pages (settings, secret reveal and rotation, blocked-attempt report, statistics)
- [x] Drop-in widget (`widget.js`, `<bridge-verify>`) and hosted page (`/verify/{publishableKey}`) in the dashboard
- [ ] Failover and widget verified end to end on physical phones and a real provider account

**Milestone 10 (v0.7):** Messaging tools.

- [ ] Send page: single and CSV bulk with variables, segment and cost preview
- [ ] Scheduled and repeating messages (once, daily, weekly, monthly in the project's time zone)
- [ ] Auto-replies with keyword rules and an opt-out list that sends respect
- [ ] Forwarding rules: another phone, Telegram, webhook, email over SMTP

### v0.1 acceptance (from the brief)

1. Start Bridge locally. 2. Start the Android gateway. 3. Pair the device. 4. The device shows online.
5. Create an API key. 6. `POST /v1/messages`. 7. The message is created. 8. It is queued.
9. Android receives the job. 10. Android sends via the SIM. 11. Android reports back.
12. The status updates. 13. The dashboard shows the message. 14. Logs show the full timeline.
15. The device can disconnect and reconnect. 16. Duplicate delivery is prevented. 17. Docker Compose self-hosting works.
