# Bridge with Node.js

Two small scripts using [`@bridge/sdk`](../../packages/sdk/README.md): one sends an SMS and follows
it to delivery, the other receives and verifies webhooks.

```bash
pnpm install
pnpm --filter @bridge/sdk build
cd examples/node
```

## Send a message

```bash
export BRIDGE_URL=http://localhost:8080
export BRIDGE_API_KEY=bk_test_…        # a test key simulates everything; bk_live_ sends real SMS
node send.mjs +15550000001 "Hello from Bridge"
```

```text
[test] queued msg_06ggn… to +15550000001 (1 segment(s))
  11:42:07  created
  11:42:07  queued
  11:42:07  device_accepted
  11:42:08  sent
  11:42:09  delivered
delivered
```

Try `+15550000002` to see a failure.

## Receive webhooks

```bash
export BRIDGE_WEBHOOK_SECRET=whsec_…   # shown when you add the endpoint
node webhook-server.mjs
```

Add `http://<host>:4000/webhooks/bridge` as an endpoint in the dashboard, then click **Send test
event** or send a message. Bridge only delivers to public addresses by default. For a server on
your own network, set `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true` on Bridge.

```text
listening on http://localhost:4000/webhooks/bridge
webhook.test       {"endpoint_id":"whk_…","message":"This is a test event from Bridge. …"}
message.sent       msg_06ggn… to +15550000001
message.delivered  msg_06ggn… to +15550000001
message.received   from AX-HDFCBK on SIM 2: Your OTP for login is 482913.
```
