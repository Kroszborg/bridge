# Bridge

Open-source infrastructure for SMS and phone verification.

Connect Android devices or professional messaging providers through one developer API.

```text
Application
     │  POST /v1/messages
     ▼
  Bridge  ──  queue · retries · delivery status · logs
     │
     ▼
Android phone + SIM   (later: MSG91, Twilio, …)
     │
     ▼
    SMS
```

Your application talks to one stable API. What delivers the message underneath (your own Android
phone today, a messaging provider later) can change without rewriting your code.

> **Status: pre-release (v0.4.0-rc.1).** Working today: accounts, projects, API keys, the
> dashboard, Docker self-hosting, the Android gateway, sending SMS through paired phones with
> delivery tracking, forwarding of incoming SMS, signed webhooks with retries, the TypeScript SDK,
> the `bridgectl` CLI, usage charts, request logs, a playground, teams with roles and invite links,
> an audit log, a public status page, and the Verify API for one-time passwords. It has not yet been verified on enough real phones to call it stable. See the [Era 0 plan](docs/architecture/era-0-plan.md) for exactly what works today.
> Do not run it in production yet.

## Run it

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
`BRIDGE_IMAGE_PREFIX=ghcr.io/kroszborg/` and `BRIDGE_VERSION` in `.env` (see
[Releases](https://github.com/kroszborg/bridge/releases)).

Create an account in the dashboard. You get a workspace and a default project. Create an API key
under **API keys**, pair a phone under **Devices**, then send:

```bash
curl http://localhost:8080/v1/messages \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to": "+919876543210", "message": "Your order has shipped."}'
```

```json
{ "id": "msg_01ja8z3k5wq2v7c9e4r2n0w6yb", "status": "queued", "to": "+919876543210", "segments": 1, "...": "..." }
```

Follow it under **Messages**, or with `GET /v1/messages/{id}`, which includes the full delivery timeline.
To be told when it is delivered, or when a phone receives an SMS, add a webhook endpoint. See
[Sending messages](docs/messages/README.md) and [Webhooks](docs/webhooks/README.md). For login and
sign-up codes, use [Verify](docs/otp/README.md): `POST /v1/otp`, then `POST /v1/otp/verify`.

From a terminal, use [`bridgectl`](docs/cli/README.md): `bridgectl send +919876543210 "Hello" --wait`.
From TypeScript, use the [SDK](packages/sdk/README.md):

```ts
const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: 'http://localhost:8080' });
const msg = await bridge.messages.send({ to: '+919876543210', message: 'Your order has shipped.' });
```

To check a key without sending anything:

```bash
export BRIDGE_API_KEY="bk_test_…"
curl http://localhost:8080/v1/whoami -H "Authorization: Bearer $BRIDGE_API_KEY"
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

`bk_test_` keys use the full API but never send a real SMS. `bk_live_` keys send through your paired devices.

Configuration lives in environment variables; see [`.env.example`](.env.example) and the
[self-hosting guide](docs/self-hosting/README.md).

## What is inside

| Path | What |
| --- | --- |
| `apps/api` | Go API, background worker and migrations in one binary (`bridge serve`, `bridge worker`, `bridge migrate`) |
| `apps/dashboard` | Next.js dashboard, including the public status page |
| `apps/web` | Public website (static Next.js export) |
| `apps/api/cmd/bridgectl` | Command-line tool: send, follow, tail events, forward webhooks locally, see [docs/cli](docs/cli/README.md) |
| `packages/sdk` | TypeScript SDK (MIT, zero dependencies), see [its README](packages/sdk/README.md) |
| `packages/api-types` | TypeScript types generated from the API's OpenAPI document |
| `examples` | [curl](examples/curl/README.md) and [Node.js](examples/node/README.md) examples |
| `android/gateway` | Android gateway app (Kotlin, `foss` and `gms` builds), see [docs/android](docs/android/README.md) |
| `docs` | Architecture, security model, self-hosting, [releasing](docs/releasing.md) |

The stack is deliberately boring: Go, PostgreSQL (data and job queue), Next.js. Self-hosting needs
one binary and one database.

## Roadmap

| Version | Scope |
| --- | --- |
| 0.1 | Android gateway: pair a phone, send SMS through its SIM, delivery status, message timeline |
| 0.2 | Inbound SMS, webhooks, TypeScript SDK |
| 0.3 | Developer platform: playground, CLI, usage, request logs, teams, audit log, status page (in progress) |
| 0.4 | Verify API: `bridge.otp.send()` / `bridge.otp.verify()` with a zero-cost test mode (in progress, see [docs/otp](docs/otp/README.md)) |
| 0.5+ | Provider layer (MSG91, Twilio, …) and routing, driven by what users ask for |

Bridge is not a bulk-SMS or marketing tool, and it does not help you bypass carrier rules, DLT
registration or provider policies. Throughput is limited by your SIM and carrier, and Bridge
reports those limits rather than hiding them.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, tests and conventions. Report security
issues privately as described in [SECURITY.md](SECURITY.md).

## License

The Bridge server, dashboard and Android app are licensed under [AGPL-3.0](LICENSE). Client SDKs
are MIT-licensed so you can embed them anywhere.
