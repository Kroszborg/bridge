# Bridge with curl

For TypeScript, see the [SDK](../../packages/sdk/README.md) and the [Node.js examples](../node/README.md).

Every Bridge feature is a plain HTTPS request. These examples assume a local install
(`docker compose up -d`) and an API key from the dashboard.

```bash
export BRIDGE_URL=http://localhost:8080
export BRIDGE_API_KEY="bk_test_…"   # test keys never send real SMS
```

## Check your key

```bash
curl "$BRIDGE_URL/v1/whoami" -H "Authorization: Bearer $BRIDGE_API_KEY"
```

## Errors

Every error has the same shape. Use `code` in your code and `request_id` when asking for help.

```bash
curl -i "$BRIDGE_URL/v1/whoami" -H "Authorization: Bearer bk_live_wrong"
```

```json
{
  "error": {
    "code": "invalid_api_key",
    "message": "The API key is malformed. Copy it again from the dashboard; keys start with bk_live_ or bk_test_.",
    "request_id": "req_06ggep2ev3hg7g197jp1338sy4"
  }
}
```

| HTTP | `code` | Meaning |
| --- | --- | --- |
| 401 | `unauthenticated` | No credentials were sent |
| 401 | `invalid_api_key` | The key is malformed, unknown, revoked or expired |
| 404 | `not_found` | The resource does not exist or belongs to another project |
| 422 | `validation_failed` | See `details` for each invalid field |
| 429 | `rate_limited` | Wait for the `Retry-After` header |

## Send an SMS

```bash
curl "$BRIDGE_URL/v1/messages" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-1042-shipped" \
  -d '{"to": "+919876543210", "message": "Your order has shipped."}'
```

## Follow it

```bash
curl "$BRIDGE_URL/v1/messages/msg_…" -H "Authorization: Bearer $BRIDGE_API_KEY"
curl "$BRIDGE_URL/v1/messages?status=failed&limit=10" -H "Authorization: Bearer $BRIDGE_API_KEY"
```

## Incoming SMS

Phones with forwarding turned on store what they receive. Incoming messages are always live:

```bash
curl "$BRIDGE_URL/v1/messages?direction=inbound&limit=10" -H "Authorization: Bearer $BRIDGE_LIVE_KEY"
curl "$BRIDGE_URL/v1/messages?direction=inbound&from=AX-HDFCBK" -H "Authorization: Bearer $BRIDGE_LIVE_KEY"
```

To be told about deliveries and incoming SMS as they happen, add a webhook endpoint in the
dashboard. See [Webhooks](../../docs/webhooks/README.md).

## Test numbers

With a test key (`bk_test_…`), `+15550000002` fails, `+15550000005` is reported undelivered, and any
other number is delivered. Nothing is sent. See [Sending messages](../../docs/messages/README.md).
