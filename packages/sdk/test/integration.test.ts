import { describe, expect, it } from 'vitest';
import { Bridge, BridgeApiError } from '../src/index';

// Runs against a real server with a test key (nothing is sent), for example:
//   BRIDGE_SDK_TEST_URL=http://localhost:8080 BRIDGE_SDK_TEST_KEY=bk_test_… pnpm --filter @bridge/sdk test
const url = process.env.BRIDGE_SDK_TEST_URL;
const key = process.env.BRIDGE_SDK_TEST_KEY;

describe.skipIf(!url || !key)('against a Bridge server', () => {
  const bridge = new Bridge({ apiKey: key ?? 'bk_test_unset', baseUrl: url });

  it('identifies the key', async () => {
    const me = await bridge.whoami();
    expect(me.environment).toBe('test');
  });

  it('sends a simulated message and follows it to delivery', async () => {
    const idem = `sdk-it-${crypto.randomUUID()}`;
    const sent = await bridge.messages.send(
      { to: '+15550000001', message: 'SDK integration test', metadata: { suite: 'sdk' } },
      { idempotencyKey: idem },
    );
    expect(sent).toMatchObject({ status: 'queued', environment: 'test', replayed: false });

    const again = await bridge.messages.send(
      { to: '+15550000001', message: 'SDK integration test', metadata: { suite: 'sdk' } },
      { idempotencyKey: idem },
    );
    expect(again).toMatchObject({ id: sent.id, replayed: true });

    const done = await bridge.messages.waitFor(sent.id, { timeoutMs: 20_000, intervalMs: 250 });
    expect(done.status).toBe('delivered');
    expect(done.events.map((e) => e.type)).toEqual([
      'created',
      'queued',
      'device_accepted',
      'sent',
      'delivered',
    ]);

    const found: string[] = [];
    for await (const m of bridge.messages.listAll({ to: '+15550000001', limit: 2 })) {
      found.push(m.id);
      if (found.length >= 3) break;
    }
    expect(found).toContain(sent.id);
  });

  it('reports failures from the test numbers', async () => {
    const sent = await bridge.messages.send({ to: '+15550000002', message: 'invalid' });
    const done = await bridge.messages.waitFor(sent.id, { timeoutMs: 20_000, intervalMs: 250 });
    expect(done).toMatchObject({ status: 'failed', error_code: 'invalid_destination' });
  });

  it('throws typed validation errors', async () => {
    const err = await bridge.messages.send({ to: '12345', message: 'x' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(BridgeApiError);
    expect(err).toMatchObject({ status: 422, code: 'validation_failed' });
    expect((err as BridgeApiError).details[0]?.location).toBe('body.to');
    expect((err as BridgeApiError).requestId).toMatch(/^req_/);
  });

  it('reads usage and devices', async () => {
    const usage = await bridge.usage();
    expect(usage.environment).toBe('test');
    expect(Array.isArray(await bridge.devices.list())).toBe(true);
  });
});
