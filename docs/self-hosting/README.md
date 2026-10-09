# Self-hosting Bridge

Bridge runs as one Go binary (API, worker and migrations) plus PostgreSQL, with an optional
Next.js dashboard. Docker Compose wires them together.

## Quick start

```bash
git clone https://github.com/kroszborg/bridge.git
cd bridge
docker compose up -d
docker compose ps        # migrate exits 0; api, worker, dashboard become healthy
```

That builds the images on your machine. To run a published release instead, put this in `.env`
and use `docker compose pull && docker compose up -d`:

```bash
BRIDGE_IMAGE_PREFIX=ghcr.io/kroszborg/   # or kroszborg/ for Docker Hub
BRIDGE_VERSION=1.0.0
```

Release images are multi-architecture (amd64 and arm64) and carry signed build provenance:
`gh attestation verify oci://ghcr.io/kroszborg/bridge-api:1.0.0 --repo kroszborg/bridge`.

Open http://localhost:3000, create your account, and create an API key.

## What runs

| Service | Image | Purpose |
| --- | --- | --- |
| `postgres` | `postgres:18.6-alpine` | Data and the job queue (River). Not exposed on the host. |
| `migrate` | `bridge-api` | Runs `bridge migrate` once, then exits. |
| `api` | `bridge-api` | `bridge serve`: REST API on port 8080. |
| `worker` | `bridge-api` | `bridge worker`: background jobs (retries, maintenance). |
| `dashboard` | `bridge-dashboard` | Next.js console on port 3000. Proxies `/api/*` to `api`. |

On a very small server you can drop the `worker` service and run the API with
`command: ["serve", "--worker"]` instead.

## Configuration

Copy [`.env.example`](../../.env.example) to `.env` next to `docker-compose.yml`. Compose reads it
automatically. The settings that matter for a public deployment:

| Variable | Set it to |
| --- | --- |
| `POSTGRES_PASSWORD` | A long random value (URL-safe characters). |
| `BRIDGE_PUBLIC_URL` | The HTTPS URL of the API, e.g. `https://api.sms.example.com`. Android devices and your apps use it. |
| `BRIDGE_DASHBOARD_URL` | The HTTPS URL of the dashboard, e.g. `https://sms.example.com`. |
| `BRIDGE_ALLOW_SIGNUP` | `false` once you have created your own account. Invite teammates from **Team**; invite links work with sign-up off. |
| `BRIDGE_OPERATOR_EMAILS` | Your email (comma-separate several). Operators see **System health** and **Insights**. When unset, the first account is the operator; set it explicitly on any server others can sign up to. |
| `BRIDGE_SECRET_KEY` | 32 random bytes as base64 or hex: `openssl rand -base64 32`. Needed to store [SMS provider](../providers/README.md) credentials, [integration](../integrations/README.md) secrets, Verify apps' token signing and Turnstile secrets, and Telegram bot tokens for [forwarding](../automation/README.md#telegram). |
| `BRIDGE_SMTP_*` | Optional. An SMTP server for password reset links, email verification codes and forwarding incoming SMS by email. See [Email](#email). |
| `BRIDGE_ACCOUNT_VERIFY_API_KEY` | Optional. An API key of one of your projects; users then verify a phone number with a code Bridge sends through that project's Verify. See [Account verification](#account-verification). |
| `BRIDGE_SITE_URL` | Optional. Your public website, e.g. `https://sms.example.com`. Sign-up then links to its `/terms` and `/privacy`. |
| `BRIDGE_TURNSTILE_SITE_KEY`, `BRIDGE_TURNSTILE_SECRET_KEY` | Optional. Cloudflare Turnstile on sign-up, password reset and repeated failed sign-ins. See [Bot protection](#bot-protection). |
| `BRIDGE_BLOCK_DISPOSABLE_EMAIL` | Optional. `true` refuses sign-ups from disposable email providers. Defaults to `BRIDGE_CLOUD`. |

When `BRIDGE_DASHBOARD_URL` uses `https`, session cookies are automatically marked `Secure`.

`BRIDGE_SECRET_KEY` encrypts provider credentials, integration signing secrets, Verify apps'
token signing and Turnstile secrets, and forwarding rules' Telegram bot tokens (AES-256-GCM).
Bridge starts without it, but refuses to save them until it is set, and the
[Verify widget](../otp/README.md#drop-in-widget) cannot issue tokens. Back it up with your database
backups: if it is lost or changed, stored credentials can no longer be read and must be entered
again. The API and worker containers both need the same value.

`BRIDGE_PUBLIC_URL` must be reachable from the internet, over HTTPS, if you use SMS providers or
integrations: providers post delivery reports to `<BRIDGE_PUBLIC_URL>/v1/provider-callbacks/…`,
and the Supabase Send SMS hook URL is built from it. With a local URL, providers still send but
messages stay `sent` because no delivery report arrives. It is also the `iss` claim of Verify
widget tokens, so changing it invalidates tokens checked against the old value.

The Verify widget's script (`<BRIDGE_DASHBOARD_URL>/widget.js`) and hosted page
(`<BRIDGE_DASHBOARD_URL>/verify/…`) are served by the dashboard, so it must be reachable by your
users when you use them.

Request logs are kept for `BRIDGE_REQUEST_LOG_RETENTION` (default `336h`, 14 days) and message
bodies for `BRIDGE_MESSAGE_RETENTION` (default `720h`). Forwarding delivery logs follow the
message retention too.

The worker runs scheduled messages: it checks for due [schedules](../schedules/README.md) once a
minute. While no worker is running, nothing scheduled is sent; when it comes back, each overdue
schedule sends once and continues, without replaying missed runs.

Webhooks are delivered only to public addresses. If your application runs on the same host or
Docker network as Bridge, set `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true`; see
[Webhooks](../webhooks/README.md#self-hosting-endpoints-on-your-network).

Android phones connect to `BRIDGE_PUBLIC_URL` (REST and the `/v1/device/connect` WebSocket). If you
put Bridge behind a reverse proxy, make sure it forwards WebSocket upgrades and does not buffer
`/v1/events/stream` (Server-Sent Events); Caddy handles both by default, and Bridge sends
`X-Accel-Buffering: no` for nginx. Optional push settings for waking phones are described in
[docs/android/README.md](../android/README.md).

### Email

Bridge sends three kinds of email through your SMTP server: password reset links, verification
codes for users' email addresses (see [Account verification](#account-verification)), and incoming
SMS for [forwarding rules](../automation/README.md#forwarding-rules) with email destinations.
Without these settings, the sign-in page hides "Forgot password?", users cannot verify or change
their email address, email destinations are unavailable, and everything else works.

| Variable | Default | Notes |
| --- | --- | --- |
| `BRIDGE_SMTP_HOST` | | The mail server. Setting it turns email on. |
| `BRIDGE_SMTP_PORT` | `587` | |
| `BRIDGE_SMTP_TLS` | `starttls` | `starttls` (usually port 587), `tls` (TLS from the first byte, usually 465), or `none` for a relay on a trusted network. |
| `BRIDGE_SMTP_USERNAME` | | Leave empty for a server that needs no login. |
| `BRIDGE_SMTP_PASSWORD` | | |
| `BRIDGE_SMTP_FROM` | | Required with a host: the sender, such as `Bridge <sms@example.com>`. |

Bridge refuses to start when `BRIDGE_SMTP_HOST` is set but the port, TLS mode or From address is
invalid. With `starttls`, sending fails if the server does not offer STARTTLS; certificates are
always verified, and credentials are only sent over TLS (or to `localhost` with `none`). Set the
same values on the API (which checks email destinations) and the worker (which sends them); the
Compose file passes them to both. Most mail providers need the From address to belong to the
account you log in with.

### Account verification

Users can prove their email address and phone number from **Account** in the dashboard. Both are
optional and independent; `GET /v1/auth/config` reports which one the server offers
(`email_verification`, `phone_verification`) and the dashboard hides the other.

**Email.** With [SMTP](#email) configured, sign-up emails a 6-digit code, and a banner reminds the
user until they enter it. They can ask for a new code once a minute, at most 5 an hour; a code
expires after 15 minutes or 5 wrong attempts. Changing the email address works the same way: the
user confirms their password, Bridge emails a code to the new address and a notice to the current
one, and the address changes (already verified) once the code is entered. Password reset links sent
to the old address stop working; sessions stay signed in.

**Phone.** Bridge verifies phone numbers with its own [Verify](../otp/README.md), through one of
your projects, so account verification doubles as an end-to-end check of your Verify setup:

1. In a project of yours, pair a phone (or connect an SMS provider) and create an API key.
2. Set `BRIDGE_ACCOUNT_VERIFY_API_KEY` to it on the API and restart.

| Variable | Default | Notes |
| --- | --- | --- |
| `BRIDGE_ACCOUNT_VERIFY_API_KEY` | | A `bk_live_…` or `bk_test_…` key. Bridge refuses to start if it is malformed. |
| `BRIDGE_ACCOUNT_VERIFY_APP` | the default app | The Verify app (ID or slug) whose message template, code length and limits the codes use. |

With a **live** key, the code is a real SMS sent through that project's paired phones or SMS
providers, and counts toward its usage like any other code. With a **test** key nothing is sent:
the dashboard shows the code ("Test key: the code is …"), which is handy to try the flow, but
proves nothing about the number, so use a live key in production. The codes appear in that
project's Verify activity with the metadata `{"purpose": "account_phone", "user_id": "usr_…"}`.

The key is looked up each time a code is sent or checked: when it is revoked, expired or deleted,
or `BRIDGE_ACCOUNT_VERIFY_APP` names no app of its project, phone verification answers
`503 phone_verification_unavailable` and the API logs the reason. A verified number belongs to one
account (`409 phone_in_use`); users can change or remove theirs. Codes follow the Verify app's
rules (expiry, attempts, one code per number every 30 seconds and 5 an hour, fraud protection),
and each account can request at most 5 codes an hour.

### Bot protection

If anyone can sign up to your server, put Cloudflare Turnstile in front of the account forms. It is
free, and usually invisible: most visitors never see a challenge.

1. In the Cloudflare dashboard, open **Turnstile**, add a widget, and add your dashboard's hostname
   (the host of `BRIDGE_DASHBOARD_URL`). Widget mode **Managed** works well.
2. Set `BRIDGE_TURNSTILE_SITE_KEY` and `BRIDGE_TURNSTILE_SECRET_KEY` on the API and restart it.
   Bridge refuses to start with only one of them.

`GET /v1/auth/config` then returns `turnstile_site_key`, and the dashboard renders the widget:

| Form | When Turnstile is checked | If Cloudflare cannot be reached |
| --- | --- | --- |
| Sign-up | Every request | Refused: `503 captcha_unavailable` |
| Password reset request | Every request | Refused: `503 captcha_unavailable` |
| Sign-in | After 3 failed attempts for the address, or 5 from the client's IP, in 15 minutes | Allowed (logged); the rate limits still apply |

A request without a token answers `400 captcha_required`, and one Cloudflare rejects (expired,
reused, from another hostname, or made for another form) `400 captcha_failed`. The API calls
Cloudflare's siteverify with the secret and the client's IP, with a 5-second timeout. The
dashboard's Content-Security-Policy allows `https://challenges.cloudflare.com` only on the
sign-up, sign-in, password-reset and hosted Verify pages.

Turnstile means Cloudflare processes each visitor's IP address and browser signals for the
challenge; mention it in your privacy policy if you publish one. Without the two keys there is no
CAPTCHA anywhere and nothing is sent to Cloudflare.

Two checks need no setup: the sign-up and reset forms carry a hidden field that people never see,
and requests that fill it are dropped (sign-up answers `400 invalid_request`, reset its usual
`202`). And with `BRIDGE_BLOCK_DISPOSABLE_EMAIL=true` (the default with `BRIDGE_CLOUD`), sign-ups
and email changes to about 275 well-known disposable providers (mailinator.com, yopmail.com,
10minutemail.com, …) answer `422 email_not_allowed`. Invite sign-ups are exempt, and privacy relays
that forward to a real inbox (Firefox Relay, DuckDuckGo, SimpleLogin, iCloud Hide My Email) are not
on the list.

## Production checklist

- [ ] TLS in front of both the API and the dashboard (Caddy, nginx, Traefik or a cloud load balancer).
- [ ] `POSTGRES_PASSWORD` changed from the default.
- [ ] `BRIDGE_SECRET_KEY` set and backed up, if you use SMS providers, integrations, the Verify widget
      or Telegram forwarding.
- [ ] `BRIDGE_SMTP_*` set, if you want password reset, email verification or forwarding by email.
- [ ] `BRIDGE_ACCOUNT_VERIFY_API_KEY` set to a live key, if users should verify phone numbers.
- [ ] `BRIDGE_ALLOW_SIGNUP=false` after creating your account, or, if others may sign up,
      `BRIDGE_TURNSTILE_*` set (see [Bot protection](#bot-protection)).
- [ ] Your reverse proxy's address is covered by `BRIDGE_TRUSTED_PROXIES` (private networks are
      trusted by default), so rate limits see real client IPs.
- [ ] PostgreSQL backups. The `pgdata` volume holds everything, including queued jobs.
- [ ] Logs collected from the containers (JSON in production).

### Example: Caddy

For a complete single-server setup with Caddy included, see [Deploying to AWS Lightsail](lightsail.md).

```caddyfile
sms.example.com {
  reverse_proxy localhost:3000
}

api.sms.example.com {
  reverse_proxy localhost:8080
}
```

## Upgrading

```bash
git pull
docker compose build
docker compose up -d     # migrate runs first; api and worker wait for it
```

Migrations are forward-only and safe to run concurrently (they take a Postgres advisory lock).
Read the [changelog](../../CHANGELOG.md) for breaking changes before upgrading.

## Health checks

* `GET /healthz` returns 200 when the process is up.
* `GET /readyz` returns 200 when the database is reachable.
* Inside the distroless image, `bridge healthcheck` probes `/healthz` (used by Compose).

## Status page and System health

Every Bridge installation has a public status page at `<BRIDGE_DASHBOARD_URL>/status`, backed by
`GET /v1/status` (no sign-in, cached for 15 seconds). It shows the current state and 90 days of
uptime for the API, the database, message processing, webhook delivery and, for information only,
phones. A phone going offline is its owner's to fix, so it never marks Bridge as down.

How it is measured:

* API and worker processes check in every 15 seconds. When no worker has checked in for a minute,
  message processing and webhook delivery are an outage.
* Jobs waiting more than 60 seconds to start is degraded; more than 5 minutes is an outage.
* The worker records every component once a minute and keeps 90 days of samples. A day is an
  outage below 95% healthy samples and degraded below 99%.

Operators (see `BRIDGE_OPERATOR_EMAILS`) also get **System health** in the dashboard: running
processes, job queues by state, messages waiting for a phone, database size and connections, and
retention settings. The page refreshes every 10 seconds.

### Insights

**Insights** (`/system/insights` in the dashboard, `GET /v1/system/insights?days=30` in the API)
shows operators how the installation is used over the last 7, 30 or 90 days (the API takes 7 to
180):

* Accounts, workspaces, projects, active API keys and phones online, with new sign-ups per day and
  the newest 15 accounts (email, when they joined, whether email and phone are verified).
* Live SMS per day by status, the delivery rate (delivered out of delivered plus failed), test
  messages, messages by route (`android`, a provider, or `simulator` for test mode) and the most
  common failure codes.
* Verify codes started and verified, workspaces that sent messages in the last 7 and 30 days, and
  the ten workspaces sending the most live SMS.
* With billing on (`BRIDGE_CLOUD`), workspaces per plan, an estimated monthly recurring revenue from
  active subscriptions, and the most recent subscription changes.

It is computed from Bridge's own database when the page loads (and every minute while it is open);
nothing is sent to an analytics service, and it never shows message text or recipients' numbers.
Days are UTC.

Who can see it: only operators. Everyone else, signed in or not, gets the same answer as for a page
or API route that does not exist (404; signed-out requests and API keys get 401, as for any
dashboard route). When `BRIDGE_OPERATOR_EMAILS` is unset the first account to sign up is the
operator, which suits a private install; on a hosted or public server set it explicitly, e.g.
`BRIDGE_OPERATOR_EMAILS=you@example.com`.

For alerting, point an external monitor at `GET /readyz` and `GET /v1/status`; the latter's
`status` field is `operational`, `degraded` or `outage`.

## Public website

`apps/web` is the project website: a static export with no server. Build it with
`pnpm --filter @bridge/web build` and serve `apps/web/out` from any static host. Set these at
build time:

| Variable | Purpose |
| --- | --- |
| `NEXT_PUBLIC_DASHBOARD_URL` | Where "Open the dashboard" and the status link point. Default `http://localhost:3000`. |
| `NEXT_PUBLIC_REPO_URL` | The source repository. Default `https://github.com/kroszborg/bridge`. |

## Running without Docker

```bash
cd apps/api
go build -o bridge ./cmd/bridge
export BRIDGE_DATABASE_URL="postgres://…"
./bridge migrate
./bridge serve --worker
```

The dashboard is a standard Next.js app: `pnpm --filter @bridge/dashboard build && pnpm --filter
@bridge/dashboard start`, with `BRIDGE_API_URL` pointing at the API.
