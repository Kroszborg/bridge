# Security model

This describes what Bridge does today (v0.1 foundation). It is updated with each milestone.

## Credentials

| Secret | Format | Stored as | Lifetime |
| --- | --- | --- | --- |
| Password | chosen by user, 10–128 characters | argon2id (m=19 MiB, t=2, p=1), PHC string | until changed |
| Session token | `bs_` + 43 base62 chars (~256 bits) | SHA-256 | `BRIDGE_SESSION_TTL` (default 30 days), sliding |
| API key | `bk_live_…` / `bk_test_…`, 40 random base62 chars + 6-char CRC32 checksum | SHA-256 | until revoked or expired |
| Device credential | `bd_` + 43 base62 chars | SHA-256 | until the device is removed |
| Pairing token | `bp_` + 43 base62 chars | SHA-256 | 10 minutes, single use |

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

Project, destination-number and device-capacity limits arrive with message sending. Rate-limited
responses return `429` with `Retry-After`. Limits are enforced in PostgreSQL behind an interface,
so a Redis backend can replace it later without changing callers.

Client IPs come from `X-Forwarded-For` only when the direct peer is in `BRIDGE_TRUSTED_PROXIES`,
and the header is read right to left so clients cannot spoof their address.

## Logging and audit

* Access logs record method, route pattern, status, duration, request ID and client IP. They never
  record headers, query strings or bodies.
* Server errors return a generic message plus the request ID; the cause is logged, never sent.
* The `audit_logs` table records sign-ups, organization and project changes, API keys, phones,
  webhooks and their secrets being revealed or rotated, invites, role changes, member removal,
  password changes and session revocations, with actor, target and IP. Owners and admins read it on
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
  anywhere. A paired phone refuses new pairings until it is disconnected.
* Device credentials, API keys and session cookies are separate: none of them works on the
  others' routes.
* Removing a device revokes its credential and closes its live connection on whichever API instance
  holds it.
* Push endpoints are supplied by devices, so the server treats them as untrusted: it only connects
  to public addresses (checked after DNS resolution), never follows redirects, and encrypts every
  WebPush message (RFC 8291) with VAPID authentication (RFC 8292). A wake-up carries no data.

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
* A verification allows 5 attempts (configurable, 1 to 10) and expires after 10 minutes (1 to 60).
  Checks lock the row and compare in constant time. Each number gets a new code at most every
  30 seconds and 5 times an hour, which bounds guessing to 25 tries per number per hour.
* The SMS text contains the code. The API, dashboard, webhooks and event stream only ever show it
  masked, and the stored text is erased once the code is used or the SMS has left the phone. Live
  keys never receive the code; test keys do, because nothing is sent.

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

## Containers

* The API image is a static binary on `distroless/static` running as a non-root user, with no shell.
* The dashboard image runs as a dedicated non-root user.
* PostgreSQL is not published to the host in the default Compose file.
