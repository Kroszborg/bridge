# @kroszborg/bridge

The TypeScript SDK for [Bridge](https://github.com/kroszborg/bridge), open-source SMS
infrastructure. Send SMS through your own Android phones, follow every message to delivery, verify
phone numbers with one-time passwords, stream events, and verify webhooks.

```bash
npm install @kroszborg/bridge
```

* No dependencies. It uses the platform's `fetch` and WebCrypto, so it runs on Node.js 20+, Bun,
  Deno, Cloudflare Workers and Vercel Edge.
* Typed from Bridge's OpenAPI document. Field names match the REST API exactly.
* Safe retries. Sends always carry an idempotency key, so a retried request never sends twice.

> Pre-releases are published under the `next` tag: `npm install @kroszborg/bridge@next`.
> Inside this repository, depend on it with `"@kroszborg/bridge": "workspace:*"`.

## Send a message

```ts
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge({
  apiKey: process.env.BRIDGE_API_KEY, // bk_test_… simulates everything, bk_live_… sends real SMS
  baseUrl: 'https://api.sms.example.com', // your Bridge server
});

const msg = await bridge.messages.send(
  { to: '+919876543210', message: 'Your order has shipped.', metadata: { order_id: 'ORD-2291' } },
  { idempotencyKey: 'order-2291-shipped' },
);
console.log(msg.id, msg.status); // msg_… queued
```

`apiKey`, `baseUrl` and `webhookSecret` default to the `BRIDGE_API_KEY`, `BRIDGE_URL` and
`BRIDGE_WEBHOOK_SECRET` environment variables.

Optional fields: `device_id` sends through one phone, `sim_slot` (1 or 2) picks a SIM, and `metadata`
holds up to 32 keys of your own data. Pass your own `idempotencyKey`, such as an order ID, to make
*your* retries safe too. A repeat with the same key returns the original message with
`replayed: true`.

## Follow it

```ts
const detail = await bridge.messages.get(msg.id); // includes the full timeline in detail.events

const final = await bridge.messages.waitFor(msg.id); // polls until delivered, failed or received
if (final.status === 'failed') console.error(final.error_code, final.error_message);

const page = await bridge.messages.list({ status: 'failed', limit: 20 });

for await (const m of bridge.messages.listAll({ direction: 'inbound', from: 'AX-HDFCBK' })) {
  console.log(m.from, m.body); // every matching message; pages are fetched as you go
}
```

Polling is fine for scripts. In production, use [webhooks](#webhooks) instead.

## One-time passwords

Bridge generates, sends and checks the code; you never store it.

```ts
await bridge.otp.send({ to: '+919876543210' });

const { valid, verification } = await bridge.otp.verify({ to: '+919876543210', code: input });
if (!valid) {
  // verification.status: 'pending' (wrong code, attempts_remaining left), 'failed', 'expired', …
}
```

With a test key nothing is sent and `send` returns the code in `code`. `send` and `verify` are not
retried automatically, because a retry could send a second code or use an attempt. See
[docs/otp](../../docs/otp/README.md).

## Devices, usage and your key

```ts
const phones = await bridge.devices.list(); // presence, battery, network, send window
await bridge.devices.test(phones[0].id, { to: '+919876543210', message: 'Test from Bridge' });

const usage = await bridge.usage(); // last 24 hours and 30 days for this key's environment
const me = await bridge.whoami(); // project and environment of the key
```

## Webhooks

Bridge signs every webhook with [Standard Webhooks](https://www.standardwebhooks.com).
`verify` checks the signature and timestamp, then returns the event typed by its `type`:

```ts
import express from 'express';
import { Bridge, WebhookVerificationError } from '@kroszborg/bridge';

const bridge = new Bridge({ webhookSecret: process.env.BRIDGE_WEBHOOK_SECRET });
const app = express();

// Verify against the raw body: a parsed and re-serialised body will not match.
app.post('/webhooks/bridge', express.raw({ type: 'application/json' }), async (req, res) => {
  try {
    const event = await bridge.webhooks.verify(req.body, req.headers);
    switch (event.type) {
      case 'message.delivered':
        await markDelivered(event.data.id);
        break;
      case 'message.received':
        await handleReply(event.data.from, event.data.body);
        break;
      case 'device.offline':
        await alertOps(`${event.data.name} went offline`);
        break;
    }
    res.sendStatus(204);
  } catch (err) {
    if (err instanceof WebhookVerificationError) return res.sendStatus(400);
    throw err;
  }
});
```

On runtimes with Fetch-style requests (Workers, Next.js route handlers, Bun, Deno):

```ts
import { verifyWebhook } from '@kroszborg/bridge';

export async function POST(request: Request) {
  const event = await verifyWebhook({
    payload: await request.text(),
    headers: request.headers,
    secret: process.env.BRIDGE_WEBHOOK_SECRET!,
  });
  // …
  return new Response(null, { status: 204 });
}
```

Retries reuse the same `webhook-id` header, so skip IDs you have already processed. Events can
arrive out of order. See the [webhooks guide](../../docs/webhooks/README.md) for every event and
the retry schedule.

## Errors

```ts
import { BridgeApiError, BridgeConnectionError } from '@kroszborg/bridge';

try {
  await bridge.messages.send({ to: '12345', message: 'hi' });
} catch (err) {
  if (err instanceof BridgeApiError) {
    err.status; // 422
    err.code; // 'validation_failed' (stable: branch on this)
    err.details; // [{ location: 'body.to', message: 'Use international E.164 format…' }]
    err.requestId; // req_… (include it when asking for help)
    err.retryAfter; // seconds, for 429 responses
  } else if (err instanceof BridgeConnectionError) {
    err.timedOut; // network failure or timeout; the request may not have reached Bridge
  }
}
```

The client retries network errors, `429` and `5xx` up to `maxRetries` times (default 2) with
exponential backoff. It follows `Retry-After` when the wait is 10 seconds or less; for longer waits
it throws so you can decide. Client errors (`4xx`) are never retried. Every request times out after
`timeoutMs` (default 30 seconds). Pass `{ signal }` as the last argument of any method to cancel it.

## Test mode

With a `bk_test_…` key nothing is sent: Bridge simulates each message's lifecycle, including
webhooks. These numbers produce specific outcomes:

| Number | Outcome |
| --- | --- |
| `+15550000002` | Fails with `invalid_destination` |
| `+15550000003` | Sent, no delivery report |
| `+15550000004` | Fails with `send_timeout` |
| `+15550000005` | Undelivered (`delivery_failed`) |
| anything else | Delivered |

`bridge.testMode` tells you which kind of key a client uses.

## Events as they happen

`bridge.events.stream()` delivers the same events as webhooks over one long-lived connection, so a
script or worker can react without a public URL. It reconnects after drops.

```ts
const stop = new AbortController();
for await (const event of bridge.events.stream({ types: ['message.failed'], signal: stop.signal })) {
  console.log(event.id, event.type, event.data); // typed by event.type
}
```

The stream is live only: events that happen while disconnected are not replayed. Where every event
matters, use [webhooks](#webhooks); they are retried for days.

## Request logs and usage history

```ts
for await (const entry of bridge.requestLogs.listAll({ status: 'error' })) {
  console.log(entry.request_id, entry.method, entry.path, entry.status, entry.error_code);
}

const history = await bridge.usageHistory({ days: 30, tz: 'Asia/Kolkata' });
for (const day of history.days) console.log(day.date, day.delivered, day.failed);
```

Request logs cover every request made with the project's keys of this key's environment, kept for
14 days by default. They never contain bodies or headers.

## Keep keys on the server

API keys can send SMS from your phones, so they must never reach a browser or mobile app. The SDK
throws if it detects a browser. Set `dangerouslyAllowBrowser: true` only for local tools that only
you use.

## Development

```bash
pnpm --filter @kroszborg/bridge build      # dist/index.js and dist/index.d.ts
pnpm --filter @kroszborg/bridge test       # unit tests
BRIDGE_SDK_TEST_URL=http://localhost:8080 BRIDGE_SDK_TEST_KEY=bk_test_… pnpm --filter @kroszborg/bridge test
```

The last command also runs the integration tests against a running server, using a test key.

MIT licensed.
