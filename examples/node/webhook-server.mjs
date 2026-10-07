// Receives Bridge webhooks, verifies them and logs each event.
//
//   BRIDGE_WEBHOOK_SECRET=whsec_… PORT=4000 node webhook-server.mjs
//
// Add http://<this host>:4000/webhooks/bridge as an endpoint in the dashboard (Webhooks → Add
// endpoint). For a server on your own network, set BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true on
// Bridge, or expose this one with a tunnel.

import http from 'node:http';
import { verifyWebhook, WebhookVerificationError } from '@bridge/sdk';

const secret = process.env.BRIDGE_WEBHOOK_SECRET;
if (!secret) {
  console.error('Set BRIDGE_WEBHOOK_SECRET to the endpoint’s signing secret (whsec_…).');
  process.exit(2);
}
const port = Number(process.env.PORT ?? 4000);

// Retries reuse the webhook-id. A real app would keep these in its database.
const seen = new Set();

function describe(event) {
  const d = event.data;
  switch (event.type) {
    case 'message.sent':
    case 'message.delivered':
      return `${d.id} to ${d.to}`;
    case 'message.failed':
      return `${d.id} to ${d.to}: ${d.error_code}`;
    case 'message.received':
      return `from ${d.from} on SIM ${d.sim_slot ?? '?'}: ${d.body}`;
    case 'device.online':
    case 'device.offline':
      return `${d.name} (${d.id})`;
    default:
      return JSON.stringify(d);
  }
}

http
  .createServer(async (req, res) => {
    if (req.method !== 'POST' || req.url !== '/webhooks/bridge') {
      res.writeHead(404).end();
      return;
    }
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    try {
      const event = await verifyWebhook({
        payload: Buffer.concat(chunks),
        headers: req.headers,
        secret,
      });
      const id = req.headers['webhook-id'];
      if (seen.has(id)) {
        console.log(`duplicate ${id}, skipped`);
      } else {
        seen.add(id);
        console.log(`${event.type.padEnd(18)} ${describe(event)}`);
      }
      res.writeHead(204).end();
    } catch (err) {
      if (err instanceof WebhookVerificationError) {
        console.warn(`rejected: ${err.message}`);
        res.writeHead(400).end();
        return;
      }
      console.error(err);
      res.writeHead(500).end();
    }
  })
  .listen(port, () => console.log(`listening on http://localhost:${port}/webhooks/bridge`));
