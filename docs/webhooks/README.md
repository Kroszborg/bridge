# Webhooks

Bridge tells your application when something happens: a message is delivered, a message fails, a
phone receives an SMS, a phone goes offline. It POSTs a signed JSON event to each endpoint you add,
so you do not have to poll the API.

Webhooks follow [Standard Webhooks](https://www.standardwebhooks.com). Its official libraries for
Node.js, Python, Go, PHP, Ruby, Java, Rust and C# verify Bridge's signatures without changes.

## Add an endpoint

In the dashboard, open **Webhooks → Add endpoint**. Enter an HTTPS URL, then choose all events or
only some. Bridge shows the endpoint's signing secret (`whsec_…`). Store it on the server that
receives the events, for example as `BRIDGE_WEBHOOK_SECRET`. You can reveal or rotate the secret
later from the endpoint's page; every reveal is written to the audit log.

**Send test event** delivers a `webhook.test` event to that endpoint only. It goes out whichever
events the endpoint subscribes to, so you can check your verification code before real events
arrive.

A project can have up to 10 endpoints.

## Events

| Type | When |
| --- | --- |
| `message.sent` | Android reported every segment of an outgoing message sent. |
| `message.delivered` | The carrier confirmed delivery. |
| `message.failed` | Sending or delivery failed for good (after Bridge's own retries). |
| `message.received` | A phone with forwarding turned on received an SMS. See [Incoming SMS](#incoming-sms). |
| `device.online` | A phone connected. |
| `device.offline` | A phone has been offline for 2 minutes. A brief network change is not reported. |
| `otp.verified` | A one-time password was entered correctly. `data` is the [verification](../otp/README.md). |
| `otp.failed` | A verification ran out of attempts. |
| `otp.expired` | A code lapsed unused (announced within about 2 minutes of expiring, or when it is checked). |

Messages and verifications made with a test key (`bk_test_…`) produce the same events, with
`"environment": "test"`. Verification events never include the code. You can build your whole webhook flow without a phone.

## The request

```http
POST /webhooks/bridge HTTP/1.1
Content-Type: application/json
User-Agent: Bridge-Webhooks/1 (+https://www.standardwebhooks.com)
webhook-id: evt_06ggn4989nk8ab525pqx6ah7rr
webhook-timestamp: 1791192420
webhook-signature: v1,8C8m4Jd0Ywq1bVqQ3n4rW9pX2H7yZkGmT0sLhJcUeAo=

{
  "type": "message.delivered",
  "timestamp": "2026-10-05T09:20:00Z",
  "data": {
    "id": "msg_06ggn3q8w4k2x9r7m5v1c0z3hy",
    "status": "delivered",
    "direction": "outbound",
    "environment": "live",
    "to": "+919876543210",
    "from": null,
    "body": "Your order has shipped.",
    "segments": 1,
    "device_id": "dev_06ggn3jw2t8p4f6h0k9m3n5q7r",
    "sim_slot": 1,
    "metadata": { "order_id": "ORD-2291" },
    "created_at": "2026-10-05T09:19:57Z",
    "sent_at": "2026-10-05T09:19:58Z",
    "delivered_at": "2026-10-05T09:20:00Z"
  }
}
```

`data` for `message.*` events is the same object `GET /v1/messages/{id}` returns, without the
timeline. For `device.*` events it holds the device's `id`, `name`, `status`, `battery_level`,
`is_charging`, `network_type` and `last_seen_at`. `timestamp` is when the event happened.

| Header | Meaning |
| --- | --- |
| `webhook-id` | The event ID. It is the same on every retry of that event, so de-duplicate on it. |
| `webhook-timestamp` | When this attempt was signed, in Unix seconds. |
| `webhook-signature` | `v1,` followed by a base64 HMAC-SHA256 signature. It may list several, separated by spaces. |

## Verify the signature

Reject any request that does not verify. Without this check, anyone who learns your URL can send
you fake deliveries.

```js
// npm i standardwebhooks
import { Webhook } from "standardwebhooks";

const wh = new Webhook(process.env.BRIDGE_WEBHOOK_SECRET);

app.post("/webhooks/bridge", express.raw({ type: "application/json" }), (req, res) => {
  let event;
  try {
    event = wh.verify(req.body, req.headers); // the raw body, exactly as received
  } catch {
    return res.status(400).end();
  }
  res.status(204).end();
});
```

```python
# pip install standardwebhooks
from standardwebhooks.webhooks import Webhook

wh = Webhook(os.environ["BRIDGE_WEBHOOK_SECRET"])
event = wh.verify(request.get_data(), dict(request.headers))
```

```go
import standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

wh, _ := standardwebhooks.NewWebhook(os.Getenv("BRIDGE_WEBHOOK_SECRET"))
err := wh.Verify(body, r.Header)
```

With the Bridge [TypeScript SDK](../../packages/sdk/README.md#webhooks), which needs no other
package and returns typed events:

```ts
import { verifyWebhook } from '@bridge/sdk';

const event = await verifyWebhook({ payload: rawBody, headers: req.headers, secret: process.env.BRIDGE_WEBHOOK_SECRET });
```

Without a library:

1. Build the signed content: `webhook-id + "." + webhook-timestamp + "." + raw body`.
2. Base64-decode the secret after removing `whsec_`, and compute HMAC-SHA256 of the content with it.
3. Accept the request if `"v1," + base64(hmac)` equals one of the space-separated values in
   `webhook-signature`. Compare in constant time.
4. Reject timestamps more than 5 minutes from your clock, so a captured request cannot be replayed
   later.

Verify against the raw request body. A body that was parsed and re-serialised no longer matches.

## Responding, retries and failures

Respond with any `2xx` status within 15 seconds. Do slow work after responding, for example in your
own job queue. Bridge records every other outcome as a failure: another status code, a timeout, a
refused connection or a TLS error.

A failed delivery is retried with the same `webhook-id`:

| Retry | After the previous attempt |
| --- | --- |
| 1 | 5 seconds |
| 2 | 5 minutes |
| 3 | 30 minutes |
| 4 | 2 hours |
| 5 | 5 hours |
| 6–11 | 10 hours each |

That is 12 attempts over about 2.8 days, so an endpoint can be down over a weekend without losing
events. Each wait has up to 10% random jitter added.

Every attempt appears in the endpoint's **delivery log**: the event, status code, the first 500
characters of the response, any error, and how long it took. If every delivery to an endpoint fails
for **5 days**, Bridge disables it and shows why. Fix the endpoint, then choose **Enable** to resume.
Events that happened while it was disabled are not sent.

Your handler should expect:

* **Duplicates.** A retry can arrive after your server processed the original but before Bridge saw
  the response. Skip a `webhook-id` you have already handled.
* **Any order.** Retries mean `message.delivered` can arrive before `message.sent`. Use `data.status`
  and the timestamps rather than arrival order.

## Incoming SMS

A phone can forward the SMS it receives to Bridge. Each one is stored as a message with
`"direction": "inbound"` and `"status": "received"`, and announced as a `message.received` event.
Typical uses are replies such as `STOP`, two-way conversations, and reading verification codes sent
to a number you own.

Forwarding is **off by default for every phone**, because it sends everything the SIM receives,
including bank codes and personal messages. Turn it on per phone under **Devices → ⋯ → Settings →
Forward incoming SMS**. Use a SIM dedicated to Bridge. The app then asks for permission to receive
SMS. Turning forwarding off takes effect on the phone immediately, and messages it had queued are
dropped by the server.

```json
{
  "type": "message.received",
  "timestamp": "2026-10-05T09:31:02Z",
  "data": {
    "id": "msg_06ggn6t2c8v0b4n6m8q0s2u4w6",
    "direction": "inbound",
    "status": "received",
    "environment": "live",
    "from": "AX-HDFCBK",
    "to": "",
    "body": "Your OTP for login is 482913.",
    "segments": 1,
    "device_id": "dev_06ggn3jw2t8p4f6h0k9m3n5q7r",
    "sim_slot": 2,
    "metadata": { "device_received_at": "2026-10-05T09:31:01.412Z" }
  }
}
```

* `from` is the sender as the network reports it: a number, or an alphanumeric sender ID such as
  `AX-HDFCBK`.
* `to` is empty because Bridge never reads the phone's own number. `device_id` and `sim_slot` tell
  you which phone and SIM received it.
* Parts of a long SMS arrive as one message.
* The phone keeps each SMS until the server confirms it, so nothing is lost while it is offline.
  Resends are de-duplicated.
* List incoming messages with `GET /v1/messages?direction=inbound`, or filter by sender with
  `&from=AX-HDFCBK`. Incoming messages are always `live`, so use a live API key.

Incoming bodies follow the same retention as outgoing ones: they are removed after
`BRIDGE_MESSAGE_RETENTION` (default 30 days). Webhook events and their delivery logs are deleted
after the same period.

## Develop locally

`bridgectl listen --forward-to http://localhost:3000/webhooks/bridge` forwards your project's events
to a local server as they happen, signed with a secret it prints. No public URL is needed. See the
[CLI guide](../cli/README.md#develop-webhooks-locally).

## Self-hosting: endpoints on your network

By default Bridge only delivers to public addresses, checked after DNS resolution and on every
connection. Project members cannot point a webhook at the server's internal network, such as cloud
metadata at `169.254.169.254`, a database or an admin panel. Redirects are never followed.

If your application runs next to Bridge, for example `http://my-app:3000/webhooks` on the same
Docker network, set:

```bash
BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true
```

Set it only when everyone who can add an endpoint is trusted with your internal network.
