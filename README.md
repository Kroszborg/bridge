# Bridge

Open-source infrastructure for SMS and phone verification.

Connect Android devices or professional messaging providers through one developer API. Use
[hosted Bridge](https://bridge.kroszborg.co) or run it on your own server.

```text
Application
     |  POST /v1/messages
     v
  Bridge  --  queue, retries, delivery status, logs
     |
     v
Android phone + SIM   or   MSG91 / Twilio / Vonage / Plivo
     |
     v
    SMS
```

Your application talks to one stable API. What delivers the message underneath (your own Android
phone, or a messaging provider as a fallback or instead) can change without rewriting your code.

**Bridge 1.0** is the first stable release. Documentation: [bridge.kroszborg.co/docs](https://bridge.kroszborg.co/docs/).

## What you get

* **SMS through your own phones.** The [Android app](docs/android/README.md) pairs by QR code or by
  signing in, sends through the phone's SIM (dual-SIM aware), reports sent and delivered, and can
  forward the SMS it receives. Bridge paces each phone under Android's limit and a daily cap that
  keeps SIMs within their operator's allowance.
* **SMS providers.** [MSG91, Twilio, Vonage and Plivo](docs/providers/README.md), as a fallback when
  no phone can send or for everything, with delivery reports.
* **Delivery you can follow.** Every message has a timeline from queued to the carrier's delivery
  report, retries that never send a code twice, and [signed webhooks](docs/webhooks/README.md)
  (Standard Webhooks) for deliveries and incoming SMS, plus a live event stream.
* **Verify.** [One-time passwords](docs/otp/README.md) that Bridge generates, sends and checks,
  with Verify apps per product, failover to another route, fraud limits, Cloudflare Turnstile, and
  a drop-in widget or hosted page that returns a signed token.
* **Messaging tools.** [Broadcasts](docs/broadcasts/README.md) to up to 10,000 numbers with
  variables, [scheduled and repeating messages](docs/schedules/README.md) in any time zone, and an
  [opt-out list, keyword auto-replies and forwarding rules](docs/automation/README.md) (to another
  phone, Telegram, Slack, Discord, a signed webhook or email).
* **A dashboard for the whole team.** Messages, Send (one, bulk CSV, scheduled), Verify,
  Automation, Phones, Providers, Integrations, API keys, webhooks, request logs, a playground,
  usage charts, teams with roles and invite links, an audit log and a public status page.
* **Developer tools.** A zero-dependency [TypeScript SDK](packages/sdk/README.md)
  (`npm install @kroszborg/bridge`), the [`bridgectl` CLI](docs/cli/README.md), and an
  [MCP server](docs/mcp/README.md) for AI assistants.
* **Integrations.** [Supabase Auth](docs/integrations/supabase.md) (built-in Send SMS hook),
  [Better Auth](docs/integrations/better-auth.md), [Auth0](docs/integrations/auth0.md),
  [Firebase and Clerk](docs/integrations/firebase-clerk.md), and
  [n8n, Zapier or Make](docs/integrations/no-code.md).
* **Secure by default.** Hashed keys and tokens, argon2id passwords, encrypted provider credentials,
  SSRF-safe webhooks, rate limits, and message text erased after 30 days. See the
  [security model](docs/security/README.md).

## Get started

### Hosted Bridge

[Create an account](https://dashboard.bridge.kroszborg.co/signup). You get a workspace and a
default project on the Free plan (1 phone, 300 live SMS a month); test messages are always free and
unlimited. Pro ($5/month) and Team ($15/month) add phones, SMS, projects and members; see
[plans and billing](docs/hosted/billing.md). The API is at `https://api.bridge.kroszborg.co`.

### Self-hosted

You need Docker with Compose.

```bash
git clone https://github.com/kroszborg/bridge.git
cd bridge
docker compose up -d
```

| Service | URL |
| --- | --- |
| Dashboard | http://localhost:3000 |
| API | http://localhost:8080 |
| API reference | http://localhost:8080/docs |
| Status page | http://localhost:3000/status |

This builds the images from source. To run a published release instead, set
`BRIDGE_IMAGE_PREFIX=ghcr.io/kroszborg/` and `BRIDGE_VERSION=1.1.0` in `.env` (see
[Releases](https://github.com/kroszborg/bridge/releases)). Self-hosted servers have no plans or
limits. For a public server, read the [self-hosting guide](docs/self-hosting/README.md); configuration
lives in environment variables, see [`.env.example`](.env.example).

### Send your first message

Create an API key under **API keys**, pair a phone under **Phones** (or add an
[SMS provider](docs/providers/README.md)), then send:

```bash
export BRIDGE_URL="https://api.bridge.kroszborg.co"   # or http://localhost:8080
export BRIDGE_API_KEY="bk_test_..."

curl "$BRIDGE_URL/v1/messages" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to": "+919876543210", "message": "Your order has shipped."}'
```

```json
{ "id": "msg_01ja8z3k5wq2v7c9e4r2n0w6yb", "status": "queued", "to": "+919876543210", "segments": 1, "...": "..." }
```

`bk_test_` keys use the full API but never send a real SMS. `bk_live_` keys send through your paired
phones (or your SMS providers, if the project routes to them).

Follow the message under **Messages**, or with `GET /v1/messages/{id}`, which includes the full
delivery timeline. To be told when it is delivered, or when a phone receives an SMS, add a webhook
endpoint. See [Sending messages](docs/messages/README.md) and [Webhooks](docs/webhooks/README.md).
For login and sign-up codes, use [Verify](docs/otp/README.md): `POST /v1/otp`, then
`POST /v1/otp/verify`, or drop in the [verification widget](docs/otp/README.md#drop-in-widget) and
check its signed token.

From TypeScript, use the [SDK](packages/sdk/README.md):

```ts
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: process.env.BRIDGE_URL });
const msg = await bridge.messages.send({ to: '+919876543210', message: 'Your order has shipped.' });
```

From a terminal, use [`bridgectl`](docs/cli/README.md): `bridgectl send +919876543210 "Hello" --wait`.

To check a key without sending anything:

```bash
curl "$BRIDGE_URL/v1/whoami" -H "Authorization: Bearer $BRIDGE_API_KEY"
```

```json
{
  "project_id": "prj_01j9tq4m2xk3v8c7e5r2n0w6yb",
  "project_name": "Default",
  "organization_id": "org_01j9tq4m2xk3v8c7e5r2n0w6yb",
  "environment": "test",
  "api_key_id": "key_01j9tq4m2xk3v8c7e5r2n0w6yb",
  "api_key_name": "Local test"
}
```

## What is inside

| Path | What |
| --- | --- |
| `apps/api` | Go API, background worker and migrations in one binary (`bridge serve`, `bridge worker`, `bridge migrate`) |
| `apps/dashboard` | Next.js dashboard, including the public status page and the hosted Verify page |
| `apps/web` | Public website and documentation site |
| `apps/api/cmd/bridgectl` | Command-line tool and MCP server: send, broadcast from CSV, opt-outs, follow, tail events, forward webhooks locally, see [docs/cli](docs/cli/README.md) |
| `packages/sdk` | TypeScript SDK (MIT, zero dependencies), see [its README](packages/sdk/README.md) |
| `packages/api-types` | TypeScript types generated from the API's OpenAPI document |
| `examples` | [curl](examples/curl/README.md) and [Node.js](examples/node/README.md) examples |
| `android/gateway` | Android gateway app (Kotlin, `foss` and `gms` builds), see [docs/android](docs/android/README.md) |
| `docs` | Guides ([messages](docs/messages/README.md), [broadcasts](docs/broadcasts/README.md), [schedules](docs/schedules/README.md), [automation](docs/automation/README.md), [Verify](docs/otp/README.md), [providers](docs/providers/README.md), [integrations](docs/integrations/README.md)), [security model](docs/security/README.md), [self-hosting](docs/self-hosting/README.md), [hosted plans](docs/hosted/billing.md), [releasing](docs/releasing.md) |

The stack is deliberately boring: Go, PostgreSQL (data and job queue), Next.js. Self-hosting needs
one binary and one database. What changed in each release is in the [changelog](CHANGELOG.md).

## Responsible use

Bridge is not a marketing tool, and it does not help you bypass carrier rules, DLT registration or
provider policies. Broadcasts are for messages people expect from you, are paced to your phones'
send limits, and always honour opt-outs. Each phone sends at most its daily cap (100 SMS in any
24 hours by default), because operators limit SIMs and may block those that send far more.
Throughput is limited by your SIM and carrier, and Bridge reports those limits rather than hiding
them.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, tests and conventions. Report security
issues privately as described in [SECURITY.md](SECURITY.md).

## License

The Bridge server, dashboard and Android app are licensed under [AGPL-3.0](LICENSE). Client SDKs
are MIT-licensed so you can embed them anywhere.
