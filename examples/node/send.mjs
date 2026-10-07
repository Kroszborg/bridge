// Sends one SMS and prints its timeline once it is delivered or fails.
//
//   BRIDGE_URL=http://localhost:8080 BRIDGE_API_KEY=bk_test_… node send.mjs +919876543210 "Hello from Bridge"
//
// With a bk_test_ key nothing is sent: Bridge simulates the whole lifecycle.

import { Bridge, BridgeApiError } from '@kroszborg/bridge';

const [to = '+15550000001', text = 'Hello from Bridge'] = process.argv.slice(2);
const bridge = new Bridge();

try {
  const sent = await bridge.messages.send({
    to,
    message: text,
    metadata: { source: 'examples/node' },
  });
  console.log(
    `${bridge.testMode ? '[test] ' : ''}queued ${sent.id} to ${sent.to} (${sent.segments} segment(s))`,
  );

  const done = await bridge.messages.waitFor(sent.id, { timeoutMs: 120_000 });
  for (const e of done.events) {
    console.log(`  ${new Date(e.created_at).toLocaleTimeString()}  ${e.type}`);
  }
  if (done.status === 'failed') {
    console.error(`failed: ${done.error_code}: ${done.error_message}`);
    process.exitCode = 1;
  } else {
    console.log(done.status);
  }
} catch (err) {
  if (err instanceof BridgeApiError) {
    console.error(
      `${err.code}: ${err.message}${err.requestId ? ` (request ${err.requestId})` : ''}`,
    );
    for (const d of err.details) console.error(`  ${d.location}: ${d.message}`);
    process.exitCode = 1;
  } else {
    throw err;
  }
}
