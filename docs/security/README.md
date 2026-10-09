# Security model

This page describes how Bridge 1.0 protects accounts, keys, phones and the data it stores, for both
hosted Bridge and self-hosted servers (they run the same code). To report a vulnerability, see
[SECURITY.md](../../SECURITY.md).

## Threat model and trust boundaries

Bridge sits between your applications, your team's browsers, Android phones that hold SIMs, SMS
providers, and the services it forwards to. Each of those talks to the API with its own kind of
credential, and none of them is trusted beyond what that credential allows.

```text
  Your servers ---- API key (bk_live_ / bk_test_) ----+
  Browsers ---- session cookie, via the dashboard ----+
  Android phones ---- device credential (bd_) --------+--> Bridge API --> PostgreSQL
  Provider delivery reports ---- secret URL token ----+        |
  Dodo Payments (hosted) ---- signed webhook ---------+        |
                                                               v
              outbound: webhooks, push, forwarding (untrusted URLs), SMS providers
```

| Boundary | What Bridge assumes | How it is enforced |
| --- | --- | --- |
| API keys | A key may leak. It must only reach its own project and environment. | Keys are scoped to one project and to `live` or `test`, stored only as SHA-256 hashes, rate limited, and can expire or be revoked. Test keys never send a real SMS. |
| Dashboard sessions | Browsers visit hostile sites. | `HttpOnly`, `SameSite=Lax` cookie, origin check on every state-changing request, roles checked on every route. |
| Android phones | A phone can be lost, rooted or paired to the wrong server. | Per-phone credential, revocable at once; the app confirms the server before pairing; the server only accepts reports for that phone's own messages. |
| Outbound URLs | Webhook, push and forwarding URLs are chosen by users or phones. | Public addresses only (checked after DNS resolution), no redirects, timeouts. |
| Inbound callbacks | Anyone can call a public URL. | Provider delivery reports need a secret token in the URL. The Supabase Send SMS hook and Dodo billing webhooks are verified with Standard Webhooks signatures and a 5-minute timestamp window. |
| Operators | Whoever runs the server and database can read the data. | Message text is erased after 30 days, request logs after 14; provider credentials and other third-party secrets are encrypted with a key kept outside the database. |

Out of scope: an attacker who controls the server, the database together with
`BRIDGE_SECRET_KEY`, or an unlocked paired phone. Carrier and SMS provider behaviour is outside
Bridge's control.

## Credentials

| Secret | Format | Stored as | Lifetime |
| --- | --- | --- | --- |
| Password | chosen by user, 10–128 characters | argon2id (m=19 MiB, t=2, p=1), PHC string | until changed |
| Session token | `bs_` + 43 base62 chars (~256 bits) | SHA-256 | `BRIDGE_SESSION_TTL` (default 30 days), sliding |
| API key | `bk_live_…` / `bk_test_…`, 40 random base62 chars + 6-char CRC32 checksum | SHA-256 | until revoked or expired |
| Device credential | `bd_` + 43 base62 chars | SHA-256 | until the device is removed |
| Pairing token | `bp_` + 43 base62 chars | SHA-256 | 10 minutes, single use |
| Password reset link | `br_` + 32 base62 chars | SHA-256 | 1 hour, single use |
| Email verification / change code | 6 digits | SHA-256 bound to the code's ID | 15 minutes or 5 wrong attempts, single use; a new code replaces it |
| Phone verification code | Verify code of the operator's project | HMAC-SHA-256 (the Verify service's own storage) | the Verify app's expiry and attempts |
| Team invite link | `bi_` + 32 base62 chars | SHA-256 | 7 days, single use |
| Verify publishable key | `bpk_` + 32 chars | plaintext (it is public) | for the app's life |
| Verify app signing secret | `bvs_` + 48 base62 chars | AES-256-GCM under `BRIDGE_SECRET_KEY`, bound to the app ID | until rotated |
| Turnstile secret | from Cloudflare | AES-256-GCM under `BRIDGE_SECRET_KEY`, bound to the app ID | until changed |
| Forwarding rule signing secret | `whsec_` + 32 random bytes | plaintext (signing needs it) | for the rule's life |
| Telegram bot token (forwarding) | from @BotFather | AES-256-GCM under `BRIDGE_SECRET_KEY`, bound to the destination ID | until changed or removed |
| SMTP password | set by the operator | environment only (`BRIDGE_SMTP_PASSWORD`), never in the database | until changed |
| Verify widget token | HS256 JWT | not stored | 10 minutes |

* Random values come from `crypto/rand` with rejection sampling (no modulo bias).
* High-entropy secrets use a fast hash; only human-chosen passwords need a slow one.
* API keys carry a checksum so malformed keys are rejected without a database query, and the
  `bk_` prefix lets secret scanners recognise leaked keys.
* Bridge shows an API key once, at creation. Lists show only a short display prefix.
* Password hashing runs at most 4 hashes at a time, so a burst of logins cannot exhaust memory.
* Logins for unknown emails spend the same time as real ones, so response times do not reveal
  which accounts exist.

## Sessions and CSRF

* The session cookie `bridge_session` is `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure`
  whenever the dashboard is served over HTTPS.
* The browser only ever talks to the dashboard origin. The dashboard proxies `/api/*` to the API
  server-side, so the cookie is first-party and the API needs no CORS.
* State-changing requests that carry the session cookie must come from the dashboard origin
  (`Origin` check, plus `Sec-Fetch-Site` when `Origin` is absent).
* Session routes and API-key routes are disjoint: a session cookie never authenticates a developer
  endpoint, and an API key never authenticates a dashboard endpoint.
* The Android app signs in with the same session endpoints. It sends no `Origin` header, which the
  check allows: browsers mark cross-site requests with `Origin` or `Sec-Fetch-Site`, and only a
  browser can attach someone else's cookie.

## Password reset

* Reset links are sent only when the server has SMTP configured (`BRIDGE_SMTP_*`); otherwise the
  endpoint answers `503` and the sign-in page hides "Forgot password?".
* `POST /v1/auth/password-reset` always answers `202`, whether or not an account uses the address,
  and the email is sent in the background so the response time is the same either way.
* Requests are limited to 10 per IP and 3 per email address an hour; confirmations to 20 per IP an
  hour.
* A link carries a `br_` token stored only as a SHA-256 hash. It works once and expires after an
  hour. Using it sets the new password, expires every other open reset link for the account, and
  signs out **every** session, then starts a new one for the browser that reset it.

## Account verification

* Email codes are 6 random digits (`crypto/rand`), sent only to the address being proven and stored
  as a SHA-256 hash bound to the code's ID. Checks compare in constant time; a code dies after 15
  minutes, 5 wrong attempts, use, or a newer code. Sends are limited to one a minute and 5 an hour
  per account, checks to 30 an hour.
* A code proves only the address it was sent to: if the account's address changed in between, the
  verification code no longer works.
* Changing the email address needs the current password (so the endpoint cannot be used to look up
  accounts), sends the code to the new address and a notice to the old one, and changes the address
  only when the code is entered. It expires open password reset links and codes; sessions stay.
* Phone codes are sent and checked by the [Verify](../otp/README.md) service through the project of
  `BRIDGE_ACCOUNT_VERIFY_API_KEY`, with its protections: hashed codes, attempts, expiry, per-number
  limits, fraud checks and redaction of the SMS once used. Bridge only checks a code that was issued
  for the same account (metadata `user_id`), so one user cannot use or burn another's code, and
  limits each account to 5 codes an hour. A verified number can belong to one account only.
* The key is resolved on every use; revoking it stops phone verification at once.

## Tenant isolation

* Every project-scoped query includes the project ID, and access is resolved through organization
  membership first.
* Resources in other tenants return **404**, never 403, so IDs cannot be probed.
* Integration tests assert isolation for every project and organization route.

## Roles and invites

* Every organization member is an **owner**, **admin** or **member**. Members can read everything
  in the organization. Admins also manage API keys, phones, webhooks, projects and invites. Only
  owners delete projects, make other owners, or change an owner's role.
* The API enforces roles on every route; the dashboard only hides what you cannot do. Roles are
  checked after membership, so non-members still get **404**.
* An organization always keeps at least one owner: the last owner cannot leave, be removed or be
  demoted. An account that is the only owner of an organization with other members cannot be
  deleted until ownership is handed over.
* Invite links carry a single-use `bi_` token stored only as a SHA-256 hash, and expire after
  7 days. They can be revoked, and accepting one is atomic, so a link admits at most one person.
  Previews are rate limited per IP, and each organization can create 50 invites a day.
* Changing your password signs out every other session. Each session records the browser's user
  agent and IP so you can recognise and revoke it.

## Abuse controls

| Limit | Default |
| --- | --- |
| Sign-ups per IP | 10 per hour |
| Login attempts per IP | 20 per minute |
| Login attempts per email | 10 per 15 minutes |
| Requests per API key | 300 per minute |
| Requests per device credential | 120 per minute |
| Pairing attempts per IP | 20 per hour |
| Pairing codes per user | 30 per hour |
| Verify widget, per IP: settings / sends / checks / redirect checks | 120 / 10 / 30 / 60 per minute |
| Password reset requests | 10 per IP and 3 per email per hour |
| Password changes / re-authentication per user | 10 per hour / 10 per 15 minutes |
| Email verification and change codes per user | 1 per minute and 5 per hour each; 30 checks per hour |
| Phone verification codes per user | 5 per hour (plus Verify's per-number limits); 30 checks per hour |
| Messages accepted per project | 1,000 per hour (broadcasts are admitted as a whole instead) |
| Messages to one number, per project | 20 per hour |
| Incoming SMS per phone | 1,000 per hour |
| Sends per phone | its send limit (30 per 30 minutes by default) and its daily cap (`daily_send_limit`, 100 in any 24 hours by default) |

Rate-limited responses return `429` with `Retry-After`. Limits are enforced in PostgreSQL behind an
interface, so a Redis backend can replace it later without changing callers. The per-phone limits
are not request limits: dispatch simply does not give a phone more than it may send, so SIMs stay
within Android's approval threshold and their operator's daily allowance. On hosted Bridge, plans
also limit phones, live SMS a month, projects and members (see [plans](../hosted/billing.md)).

Client IPs come from `X-Forwarded-For` only when the direct peer is in `BRIDGE_TRUSTED_PROXIES`,
and the header is read right to left so clients cannot spoof their address.

## Logging and audit

* Access logs record method, route pattern, status, duration, request ID and client IP. They never
  record headers, query strings or bodies.
* Server errors return a generic message plus the request ID; the cause is logged, never sent.
* The `audit_logs` table records sign-ups, organization and project changes, API keys, phones,
  webhooks and their secrets being revealed or rotated, invites, role changes, member removal,
  password changes, session revocations, email verification and email changes (old and new
  address), phone numbers verified or removed, SMS providers, routing, integrations, Verify apps
  (created, updated, deleted, signing secret revealed or rotated), broadcasts created or canceled
  and schedule changes from the dashboard, opt-outs added or removed in the dashboard, and
  auto-reply and forwarding rules (created, updated, deleted, signing secret revealed), with actor,
  target and IP. Owners and admins read it on
  the Audit log page. Metadata never contains secrets.

## Status data

The public status page (`GET /v1/status`) shows only component states, uptime percentages and the
installation-wide count of phones online. It never shows organizations, projects, messages, hosts
or errors. **System health** (`GET /v1/system`) adds hostnames, versions, queue sizes and database
details, and is limited to operators: the emails in `BRIDGE_OPERATOR_EMAILS`, or the first account
when that is unset.

## Transport and headers

* API responses set `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer` and `Cross-Origin-Opener-Policy: same-origin`, plus HSTS when the
  public URL is HTTPS.
* The dashboard sends a Content-Security-Policy that allows only its own origin, and blocks framing.
* Request bodies are capped at 1 MiB.
* Terminate TLS in front of Bridge in production (Caddy, nginx, a cloud load balancer). See the
  [self-hosting guide](../self-hosting/README.md).

## Devices

* A paired phone authenticates with a device credential (`bd_…`, ~256 bits). The server stores its
  SHA-256; the phone encrypts it with an AES-256-GCM key held in the Android Keystore. App backups
  are disabled, so a copied data directory cannot impersonate the phone.
* Pairing codes are single-use, expire after 10 minutes, and are claimed inside a locking
  transaction, so one code can never create two devices.
* The app confirms the server's host before pairing, because a `bridge://pair` link can come from
  anywhere. A phone that is already paired shows which pairing a new code would replace, and keeps
  the old one until the server accepts the new code.
* Pairing from the app after signing in creates the same single-use code on the user's behalf, so
  it needs the admin or owner role, like pairing from the dashboard.
* Device credentials, API keys and session cookies are separate: none of them works on the
  others' routes.
* Removing a device revokes its credential and closes its live connection on whichever API instance
  holds it.
* Android retries a send only when it is sure nothing reached the network (radio off, no service,
  modem not ready, an explicit retry). Ambiguous failures such as `generic_failure` can arrive after
  the carrier accepted the SMS, so they are final and are never retried automatically, to avoid
  sending a code twice.
* Push endpoints are supplied by devices, so the server treats them as untrusted: it only connects
  to public addresses (checked after DNS resolution), never follows redirects, and encrypts every
  WebPush message (RFC 8291) with VAPID authentication (RFC 8292). A wake-up carries no data.
* The server only accepts status reports from the phone a message is assigned to; reports about
  other messages are ignored.

### The Android app

| Permission | Used for |
| --- | --- |
| Send SMS | Required: sending the messages the server queues for this phone. |
| Receive SMS | Optional, asked for only when forwarding of incoming SMS is turned on for the phone. |
| Phone | Optional: listing SIM slots on dual-SIM phones. The phone number is never read. |
| Camera | Only to scan the pairing QR code. |
| Notifications | The foreground-service notification that keeps the gateway running. |

* **What leaves the phone.** Delivery reports for the messages it sends; battery level, charging
  state, network type, carrier name and SIM slots for the dashboard; and incoming SMS, but only
  while the server has forwarding turned on for that phone (off by default). While it is off,
  incoming SMS are ignored: nothing is stored or sent. The app never reads the SMS inbox, contacts
  or the IMEI, and contains no analytics or ads. Only the `gms` build includes Firebase Cloud
  Messaging, for wake-ups; the `foss` build uses UnifiedPush.
* **Credential storage.** The device credential is encrypted with an AES-256-GCM key that never
  leaves the Android Keystore. Signing in stores the session cookie encrypted with a second
  Keystore key; the password is never stored. Signing out deletes the session on the server and the
  cookie and key on the phone. App backups and device-to-device transfers are disabled.
* **Use a SIM dedicated to Bridge** if you turn on forwarding, because incoming SMS include
  verification codes and personal messages.

## Webhooks

* Each endpoint has its own signing secret (`whsec_`, 32 random bytes). It is stored in plaintext
  because signing needs it. Database access therefore implies the ability to forge webhooks, so
  protect database backups. Reveals and rotations are audit-logged, and list responses never
  include the secret.
* Requests are signed with Standard Webhooks HMAC-SHA256 over the event ID, timestamp and body, so
  receivers can reject forged and replayed requests.
* Endpoint URLs are supplied by project members, so the server treats them as untrusted, like push
  endpoints. It connects only to public addresses (checked after DNS resolution on every connection),
  never follows redirects, and times out after 15 seconds. `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS`
  lifts the address check for trusted installs.
* Webhook events contain message bodies, so they are deleted after the message retention period.

## One-time passwords

* Codes come from `crypto/rand`. Only an HMAC-SHA256 of each code is stored, keyed by a 256-bit
  key generated once per installation (in `server_keys`) and bound to the verification ID. The hash
  is erased when the verification finishes.
* A verification allows 5 attempts (configurable per Verify app, 1 to 10) and expires after
  10 minutes (1 to 60). Checks lock the row and compare in constant time. Each number gets a new
  code at most every 30 seconds and 5 times an hour per app, which bounds guessing to 25 tries per
  number per hour and app.
* The SMS text contains the code. The API, dashboard, webhooks and event stream only ever show it
  masked. The stored text is erased once the verification finishes and the SMS has left the phone,
  and at the latest 1 hour after sending (it is kept until then so failover can resend it). Live
  keys never receive the code; test keys do, because nothing is sent.
* **Failover never duplicates a code on purpose.** A live code is resent through another route at
  most once, and only when its SMS is provably not on its way: still queued (the original is
  atomically marked failed first, so a phone that takes it at the same moment wins and nothing is
  resent) or failed with an unambiguous error. It is never resent after an ambiguous failure
  (Android's `generic_failure` or unknown result codes, a provider timeout or `5xx`), because the
  carrier may already have accepted the SMS, and never once a phone is sending or has sent it. The
  failover job carries only IDs, never the code.
* **Fraud protection** checks, per app: allowed countries, codes per end-user IP per hour (10 by
  default), codes per number range (the number without its last 3 digits, 20 per hour by default),
  an optional per-country hourly cap, and Turnstile for widget sends. If Cloudflare cannot be
  reached, widget sends are refused (`503`). If the rate limiter's database is unavailable, codes
  are allowed and a warning is logged, so an outage does not stop logins.
* Blocked attempts store the number, its country, the end user's IP address (when known) and the
  reason, and are deleted after 30 days. They are visible to project members and sent as
  `otp.blocked` events, so treat webhook receivers of that event as handling personal data.

## Verify widget and tokens

* **Publishable key versus secrets.** A Verify app's publishable key (`bpk_…`) only identifies the
  app in the browser: it can request and check codes for that app, subject to every limit below,
  and nothing else. It cannot read verifications, send other SMS or verify tokens. API keys and the app's
  signing secret (`bvs_…`) stay on servers.
* **Origins.** The public widget endpoints (`/v1/widget/{key}/…`) answer CORS only for the app's
  allowed origins (`https`, or `http` on `localhost`, exact match after normalisation) and the
  dashboard's own origin, which serves the hosted page. A browser request from any other origin is
  refused with `403`. CORS does not stop scripts, which is why the limits below exist.
* **Redirect URIs** for the hosted page must be registered in advance and match character for
  character (`https` only, `http` on `localhost`, no fragment), so the token cannot be sent to an
  attacker's URL. Use `state` to bind the callback to the session that started it.
* **Abuse limits.** Each IP address may load the widget settings 120 times, send 10 codes, check
  30 codes and check 60 redirect URIs per minute. Sends also go through the app's fraud protection
  with the caller's IP address, and through Cloudflare Turnstile when the app has it.
* **Tokens.** A successful check returns a JWT signed with HS256 under the app's signing secret,
  with `iss` (the API URL), `aud` (the app ID), `sub` (the phone number), `vid` (the verification
  ID), `env`, `iat`, `exp` (10 minutes later) and a random `jti`. The secret is generated with
  `crypto/rand`, stored encrypted, revealed only to owners and admins (audited), and rotation
  invalidates earlier tokens at once. Verifiers must accept only `HS256` and check the signature,
  `aud`, `iss`, `exp` and `env`, and should accept each token once. `POST /v1/otp/tokens/verify`
  does this server-side and also confirms that the verification exists and was verified for that
  number.
* **Test widgets return the code.** A widget in the `test` environment sends no SMS and shows the
  code to whoever asked for it, so the flow can be built without a phone. Its tokens carry
  `env: test`, and they prove nothing about who controls the number: production must require
  `env: live`. The API check refuses a test token with a live key (`environment_mismatch`).

## SMS providers and integrations

* Provider credentials (auth tokens, API secrets, MSG91 auth keys) and integration signing secrets
  are encrypted with AES-256-GCM under `BRIDGE_SECRET_KEY` (32 bytes, set by the operator). Each
  value has a random 12-byte nonce and is bound to its row's ID as additional data, so a value
  copied to another row does not decrypt. Without the key, Bridge refuses to store them. Losing or
  changing the key makes stored credentials unreadable; they must be entered again.
* Credentials are write-only. The API and dashboard never return them, only a hint (the Twilio
  account SID, the Vonage API key, the Plivo Auth ID, or the last 4 characters of an MSG91 auth
  key). Audit entries record that credentials changed, never their values.
* Each provider account's delivery-report URL contains a random 32-character token. Bridge looks up
  the account by ID and compares the token in constant time; a wrong token gets `404`. Anyone with
  the URL can mark that account's messages delivered or failed, so treat it as a secret.
* Only owners and admins can add, change, check or remove providers, change routing, or manage
  integrations.
* The Supabase Send SMS hook verifies Supabase's Standard Webhooks signature with the stored secret
  and rejects timestamps more than 5 minutes off. Requests are refused (`503`) until a secret is
  stored. At most 64 KB of a request body is read.
* Codes delivered for an integration (the Supabase hook) are one-time-password messages: masked in
  the API, dashboard and webhooks, with the stored text and template variables erased once the SMS
  is finished, like Verify's own codes.
* Test keys and test integrations never reach a provider, so tests cannot spend money.

## Request logs and the event stream

* Request logs store metadata only: method, path, status, error code, duration, key ID, client IP
  and user agent. Bodies, headers and query strings, which may hold phone numbers, message text or
  keys, are never stored. Entries are deleted after `BRIDGE_REQUEST_LOG_RETENTION` (default 14 days).
* The event stream requires an API key and only carries the key's project and environment.
* Events reach other API instances through PostgreSQL `NOTIFY`. Payloads under 8 KB, including
  message bodies, travel inside the notification and are not stored. Larger events are stored like
  webhook events and follow the message retention period.

## Incoming SMS

* Forwarding is off by default and turned on per device. Turning it on or off is audit-logged.
* The phone sends incoming SMS only while the server has forwarding on, and the server drops any
  that arrive after it was turned off.
* Incoming messages are rate limited per device (1,000 per hour). Above that the phone keeps them
  and retries later.

## Opt-outs, broadcasts and schedules

* Ordinary messages to a number on the project's opt-out list are refused before anything is
  queued, whichever way they are sent (API, SDK, CLI, MCP, broadcasts, schedules, forwarding to a
  phone). One-time passwords and auto-replies are exempt: people must still be able to sign in,
  and the person who texted `STOP` gets the confirmation. The list is per project and shared by
  live and test keys.
* Changing the list or the rules from the dashboard needs an owner or admin. Live broadcasts and
  live schedules from the dashboard also need an owner or admin; members can use test mode.
* Broadcasts skip the per-message hourly limits but are admitted as a whole: at most 10,000
  recipients each and 20 per project per hour, paced to the project's phone capacity.
* Broadcast template variables are stored only until each recipient's message is created, then
  cleared; the message text follows the message retention period. The template itself, the
  recipient numbers and a schedule's message stay until the broadcast's project or the schedule is
  deleted.
* **CSV exports** (the opt-out list) are protected against formula injection: a cell that starts
  with `=`, `+`, `-`, `@`, a tab or a carriage return is prefixed with `'` so spreadsheet apps show
  it as text instead of running it. E.164 numbers (`+` and digits only) are left as they are.

## Forwarding and auto-replies

* **Destinations are untrusted, like webhook endpoints.** Forwarding webhooks connect only to
  public addresses (checked after DNS resolution on every connection), never follow redirects,
  time out after 15 seconds, and keep at most 300 characters of a response in the delivery log. `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS` lifts the address check, as
  for webhooks. Telegram requests go only to Telegram's API.
* **Signing.** Each forwarding rule has its own Standard Webhooks secret. It is stored in plaintext
  because signing needs it, so protect database backups. It is shown once at creation; reveals are
  audit-logged and list responses never include it. Retries reuse the delivery ID as `webhook-id`
  so receivers can de-duplicate.
* **Telegram bot tokens** are write-only: encrypted with AES-256-GCM under `BRIDGE_SECRET_KEY`,
  bound to their destination's ID, and never returned by the API (only `bot_token_set`). Without
  the key they cannot be stored. Bridge removes the token from any error message it records,
  because Telegram's URLs contain it.
* **SMTP.** `starttls` (the default) refuses to continue if the server does not offer STARTTLS;
  `tls` uses TLS from the first byte. Both require TLS 1.2 or newer and verify the server's
  certificate against `BRIDGE_SMTP_HOST`. Credentials are only sent over TLS, or to `localhost`
  with `none`. Header values are stripped of line breaks, so a sender ID cannot inject email
  headers, and messages carry `Auto-Submitted: auto-generated`.
* **Loop protection.** An auto-reply rule answers a number at most once every 10 minutes, and rules
  never act on alphanumeric sender IDs, short codes (fewer than 7 digits) or numbers without a
  country code. An incoming SMS whose text matches a message Bridge forwarded in the last hour is
  ignored by auto-replies and forwarding, so two of your phones cannot forward to each other
  forever. Each incoming SMS is answered once and forwarded once per destination, even when its
  processing is retried.
* Forwarding copies message text to other services, where Bridge's retention no longer applies.
  Forwarding delivery logs are deleted after the message retention period, and a forward that has
  not gone out by the time the text is removed is not sent.

## Data retention

The worker's maintenance job removes data on a schedule. The periods below are the defaults, which
hosted Bridge uses; self-hosted servers can change the first two.

| Data | Kept for |
| --- | --- |
| Message text (and template variables) | 30 days (`BRIDGE_MESSAGE_RETENTION`), then erased. Numbers, statuses and timelines stay for your history. |
| Webhook events, stored event-stream payloads, forwarding delivery logs | The message retention period |
| Request logs (metadata only) | 14 days (`BRIDGE_REQUEST_LOG_RETENTION`) |
| Verify code SMS text | Until the verification finishes and the SMS has left the phone, at most 1 hour |
| Blocked verification attempts | 30 days |
| Status samples | 90 days |
| Accounts, projects, phones, settings | Until you delete them. Deleting your account removes workspaces where you are the only member. |

## Hosted Bridge

Hosted Bridge (`bridge.kroszborg.co`) runs this repository's code with `BRIDGE_CLOUD=true`, on AWS
Lightsail in the Mumbai region, as described in [Deploying to AWS Lightsail](../self-hosting/lightsail.md).

* **TLS everywhere.** Caddy is the only service listening publicly (ports 80 and 443). It obtains
  and renews certificates automatically and redirects HTTP to HTTPS. The API, dashboard, website
  and PostgreSQL publish no ports of their own.
* **Database.** PostgreSQL is reachable only on the Compose network, never from the internet.
  Automatic Lightsail snapshots of the instance are the backup. `BRIDGE_SECRET_KEY` lives in the
  server's environment file, never in the database, so a database dump alone does not reveal
  provider credentials or bot tokens.
* **Payments.** Dodo Payments is the merchant of record. Card details go to Dodo's checkout and
  never reach Bridge; Bridge stores only the plan, subscription status, and Dodo's customer and
  subscription IDs.
* **Billing webhooks.** `POST /v1/billing/dodo/webhook` reads at most 1 MiB, verifies the Standard
  Webhooks signature over the raw body with `BRIDGE_DODO_WEBHOOK_SECRET` (constant-time compare,
  5-minute timestamp window) and answers `401` if it does not match. Each delivery is claimed
  atomically by its `webhook-id`, so a retry is applied at most once.
* **Authoritative state.** A webhook is only a signal. Bridge reads the subscription's current
  state back from Dodo's API and stores that, so a forged, late or out-of-order event cannot grant
  a plan or undo a newer change. A subscription is tied to a workspace by checkout metadata, then
  by its stored subscription or customer ID; one that matches no workspace is refused, and the
  return-from-checkout sync refuses a subscription that belongs to another workspace.
* **Who can pay.** Only owners can start a checkout, change plan, open the customer portal or
  withdraw a cancellation.
* **Limits are checked server-side**, never only in the dashboard. Live SMS are counted in the
  transaction that creates each message, so concurrent sends cannot overshoot the allowance;
  phones, projects and members are checked in the transaction that adds them.

## Containers

* The API image is a static binary on `distroless/static` running as a non-root user, with no shell.
* The dashboard image runs as a dedicated non-root user.
* PostgreSQL is not published to the host in the default Compose file.
* `docker-compose.prod.yml` removes the API, dashboard and website ports and puts Caddy in front,
  so only 80 and 443 are open.
