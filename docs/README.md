# Bridge documentation

Bridge is open-source infrastructure for SMS and phone verification. Your application calls one
API; Bridge sends the message through an Android phone you own, or through an SMS provider (MSG91,
Twilio, Vonage or Plivo), and follows it to the carrier's delivery report.

```text
Your application -- POST /v1/messages --> Bridge --> Android phone + SIM --> SMS
                                            |
                                            +--> or MSG91 / Twilio / Vonage / Plivo
```

Every message gets a timeline (queued, sending, sent, delivered or failed), retries, and signed
webhooks for delivery reports and incoming SMS. On top of that sit
[Verify](otp/README.md) for one-time passwords, [broadcasts](broadcasts/README.md),
[scheduled messages](schedules/README.md), and [opt-outs, auto-replies and forwarding](automation/README.md).

This documentation covers Bridge 1.0, the first stable release, both [hosted](https://bridge.kroszborg.co)
and self-hosted. The [changelog](../CHANGELOG.md) lists what each release contains.

## Hosted or self-hosted

Both run the same code and expose the same API.

| | Hosted Bridge | Self-hosted |
| --- | --- | --- |
| Where it runs | Our servers, at `https://api.bridge.kroszborg.co` | Your server, with Docker Compose |
| Set up | [Create an account](https://dashboard.bridge.kroszborg.co/signup) | [Self-hosting guide](self-hosting/README.md): `docker compose up -d` |
| Plans | Free, Pro ($5/month) and Team ($15/month), see [plans and billing](hosted/billing.md) | No plans or limits |
| Your data | Kept by us, see the [privacy policy](https://bridge.kroszborg.co/privacy/) | Never leaves your server |

## Quick start

1. **Get an API key.** In the dashboard, open **API keys** and create one. A `bk_test_` key uses the
   full API but never sends a real SMS, so you can build against it first; a `bk_live_` key sends.
2. **Pair a phone.** Install the [Android app](android/README.md), open **Phones → Pair device** in
   the dashboard and scan the code with the app, or sign in to your account in the app and tap
   **Pair this phone**. No phone? Add an [SMS provider](providers/README.md) instead.
3. **Send a message.**

```bash
export BRIDGE_URL="https://api.bridge.kroszborg.co"   # or your own server, e.g. http://localhost:8080
export BRIDGE_API_KEY="bk_test_..."

curl "$BRIDGE_URL/v1/messages" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to": "+919876543210", "message": "Your order has shipped."}'
```

The response is `202 Accepted` with the message in status `queued`. Follow it in the dashboard
under **Messages**, or with `GET /v1/messages/{id}`.

From TypeScript, use the [SDK](../packages/sdk/README.md) (`npm install @kroszborg/bridge`):

```ts
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge({
  apiKey: process.env.BRIDGE_API_KEY,
  baseUrl: process.env.BRIDGE_URL,
});

const msg = await bridge.messages.send({ to: '+919876543210', message: 'Your order has shipped.' });
const final = await bridge.messages.waitFor(msg.id); // delivered, or failed with a reason
```

From a terminal, use the [CLI](cli/README.md): `bridgectl send +919876543210 "Hello" --wait`.

## Where to go next

| To | Read |
| --- | --- |
| Send SMS and follow delivery | [Sending messages](messages/README.md) |
| Turn an Android phone into a gateway | [Android app](android/README.md) |
| Add phone verification to sign-up or login | [Verify: one-time passwords](otp/README.md) |
| Be told about deliveries and incoming SMS | [Webhooks](webhooks/README.md) |
| Plug Bridge into Supabase, Auth0, Clerk, n8n and others | [Integrations](integrations/README.md) |
| Send without a phone, or fall back when one is offline | [SMS providers](providers/README.md) |
| Message many people, or at set times | [Broadcasts](broadcasts/README.md), [schedules](schedules/README.md) |
| Let an AI assistant send and check messages | [MCP server](mcp/README.md) |
| Run Bridge on your own server | [Self-hosting](self-hosting/README.md) |
| Choose a hosted plan | [Plans and billing](hosted/billing.md) |
| Understand how keys, devices and data are protected | [Security model](security/README.md) |
