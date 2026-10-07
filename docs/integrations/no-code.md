# n8n, Zapier and Make

Workflow tools talk to Bridge with plain HTTP: an HTTP request step sends an SMS or a code, and a
webhook trigger starts a workflow when Bridge reports something.

| Tool | Send (HTTP step) | Receive (trigger) |
| --- | --- | --- |
| n8n | **HTTP Request** node | **Webhook** node |
| Zapier | **Webhooks by Zapier**, POST or Custom Request action | **Webhooks by Zapier**, Catch Hook or Catch Raw Hook |
| Make | **HTTP**, Make a request | **Webhooks**, Custom webhook |

Names of steps change between versions of these tools; look for their generic HTTP and webhook
building blocks.

## Send an SMS

Configure the HTTP step like this curl command:

```bash
curl "$BRIDGE_URL/v1/messages" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to": "+919876543210", "message": "Your order has shipped."}'
```

| Setting | Value |
| --- | --- |
| Method | `POST` |
| URL | `https://api.sms.example.com/v1/messages` (your `BRIDGE_PUBLIC_URL`) |
| Header | `Authorization: Bearer bk_live_…` (store the key in the tool's credentials, not in the step) |
| Body type | JSON |
| Body | `{"to": "+919876543210", "message": "…"}`, mapping fields from earlier steps |

Numbers must be E.164 with the country code. Bridge answers `202` with the message (`id`, `status:
queued`). Add an `Idempotency-Key` header (for example the order ID) if the tool may run the step
twice: a repeat then returns the first message instead of sending again. See
[Sending messages](../messages/README.md) for every field.

## Send and check a code

For a sign-up form or a phone check inside a workflow, use [Verify](../otp/README.md), which makes,
sends and checks the code for you:

| Step | Request | Body |
| --- | --- | --- |
| Send the code | `POST /v1/otp` | `{"to": "+919876543210"}` |
| Check what the user typed | `POST /v1/otp/verify` | `{"to": "+919876543210", "code": "482913"}` |

Branch on `valid` in the second response. A wrong code is a normal `200` with `valid: false`; `404`
means no code is pending for that number. Codes sent this way are shown masked in Bridge.

Use a `bk_test_` key while building the workflow: nothing is sent, and the send response includes
the code so you can feed it to the verify step.

## Start a workflow from Bridge

1. Create the trigger in your tool and copy its URL.
2. In Bridge, open **Webhooks → Add endpoint**, paste the URL and pick events, for example
   `message.failed`, `message.received` or `otp.verified`.
3. Use **Send test event** to give the tool a sample to map fields from.

The event is JSON with `type`, `timestamp` and `data`; see [Webhooks](../webhooks/README.md#events)
for each event. Respond within 15 seconds (these tools do by default), or Bridge retries.

Bridge only delivers to public addresses. If your n8n runs on the same Docker network or LAN as
Bridge, set `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true` on the Bridge server; see
[Self-hosting endpoints](../webhooks/README.md#self-hosting-endpoints-on-your-network).

### Checking that the event came from Bridge

Bridge signs every event with [Standard Webhooks](https://www.standardwebhooks.com). Anyone who
learns your trigger URL can post to it, so pick at least one of these:

- **Verify the signature in a code step.** It needs the raw body exactly as received and the
  `webhook-id`, `webhook-timestamp` and `webhook-signature` headers. For example, in a JavaScript
  step with Node's `crypto` module (on self-hosted n8n, allow it with
  `NODE_FUNCTION_ALLOW_BUILTIN=crypto`):

  ```js
  const crypto = require('crypto');

  function verifyBridge(secret, headers, rawBody) {
    const key = Buffer.from(secret.replace(/^whsec_/, ''), 'base64');
    const signed = `${headers['webhook-id']}.${headers['webhook-timestamp']}.${rawBody}`;
    const expected = `v1,${crypto.createHmac('sha256', key).update(signed).digest('base64')}`;
    const fresh = Math.abs(Date.now() / 1000 - Number(headers['webhook-timestamp'])) < 300;
    const match = headers['webhook-signature'].split(' ').some(
      (s) => s.length === expected.length && crypto.timingSafeEqual(Buffer.from(s), Buffer.from(expected)),
    );
    return fresh && match;
  }
  ```

  Whether you can get the raw body depends on the tool and the trigger (Zapier's Catch Raw Hook
  provides it; check your tool's trigger options).
- **Re-read from the API.** Take only the ID from the event and fetch the real thing with your API
  key: `GET /v1/messages/{id}` for `message.*` events, `GET /v1/otp/{id}` for `otp.*` events. A
  forged event then cannot make the workflow act on false data.
- **Keep the URL secret.** Trigger URLs from these tools are long and random. That alone is weaker
  than the two options above, but better than a guessable URL.

Retries reuse the same `webhook-id`, so skip IDs the workflow has already handled if running twice
would matter.
