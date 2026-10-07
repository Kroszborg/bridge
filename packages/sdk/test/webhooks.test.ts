import { describe, expect, it } from 'vitest';
import { Bridge, signWebhook, verifyWebhook, WebhookVerificationError } from '../src/index';

// The reference vector from the Standard Webhooks test suite.
const vector = {
  secret: 'whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw',
  id: 'msg_p5jXN8AQM9LWM0D4loKWxJek',
  timestamp: 1614265330,
  payload: '{"test": 2432232314}',
  signature: 'v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=',
};

const event = JSON.stringify({
  type: 'message.received',
  timestamp: '2026-10-05T09:31:02Z',
  data: { id: 'msg_1', direction: 'inbound', from: 'AX-HDFCBK', body: 'Your OTP is 482913.' },
});

async function signed(body: string, secret = vector.secret, at = new Date()) {
  const ts = Math.floor(at.getTime() / 1000);
  return {
    'webhook-id': 'evt_1',
    'webhook-timestamp': String(ts),
    'webhook-signature': await signWebhook(secret, 'evt_1', ts, body),
  };
}

describe('signWebhook', () => {
  it('matches the Standard Webhooks reference vector', async () => {
    expect(await signWebhook(vector.secret, vector.id, vector.timestamp, vector.payload)).toBe(
      vector.signature,
    );
  });
});

describe('verifyWebhook', () => {
  it('accepts the reference vector', async () => {
    const parsed = await verifyWebhook({
      payload: vector.payload,
      headers: {
        'webhook-id': vector.id,
        'webhook-timestamp': String(vector.timestamp),
        'webhook-signature': vector.signature,
      },
      secret: vector.secret,
      now: new Date(vector.timestamp * 1000),
    });
    expect(parsed).toEqual({ test: 2432232314 });
  });

  it('returns a typed event for every header and body shape', async () => {
    const h = await signed(event);
    const bytes = new TextEncoder().encode(event);
    for (const [payload, headers] of [
      [event, h],
      [bytes, new Headers(h)],
      [bytes.buffer, { ...h, 'WEBHOOK-ID': undefined }],
    ] as const) {
      const e = await verifyWebhook({ payload, headers, secret: vector.secret });
      expect(e.type).toBe('message.received');
      if (e.type === 'message.received') expect(e.data.from).toBe('AX-HDFCBK');
    }
  });

  it('types the messaging tools events', async () => {
    const autoReplied = JSON.stringify({
      type: 'message.auto_replied',
      timestamp: '2026-10-05T09:31:02Z',
      data: {
        environment: 'live',
        message: { id: 'msg_1', direction: 'inbound', from: '+919876543210', body: 'STOP' },
        rule_id: 'arr_1',
        rule_name: 'Unsubscribe',
        keyword: 'STOP',
        action: 'opt_out',
        reply_message_id: 'msg_2',
      },
    });
    const a = await verifyWebhook({
      payload: autoReplied,
      headers: await signed(autoReplied),
      secret: vector.secret,
    });
    expect(a.type === 'message.auto_replied' && a.data.action).toBe('opt_out');

    const completed = JSON.stringify({
      type: 'broadcast.completed',
      timestamp: '2026-10-05T09:31:02Z',
      data: { id: 'brd_1', status: 'completed', counts: { recipients: 2, delivered: 2 } },
    });
    const b = await verifyWebhook({
      payload: completed,
      headers: await signed(completed),
      secret: vector.secret,
    });
    expect(b.type === 'broadcast.completed' && b.data.counts.delivered).toBe(2);
  });

  it('accepts any signature in a space-separated list (secret rotation)', async () => {
    const h = await signed(event);
    h['webhook-signature'] = `v1,bm90LWl0 ${h['webhook-signature']}`;
    await expect(
      verifyWebhook({ payload: event, headers: h, secret: vector.secret }),
    ).resolves.toBeTruthy();
  });

  it.each([
    [
      'a tampered body',
      async () => ({ payload: event.replace('482913', '000000'), headers: await signed(event) }),
    ],
    [
      'another secret',
      async () => ({ payload: event, headers: await signed(event, 'whsec_c2VjcmV0LXR3bw==') }),
    ],
    [
      'an old timestamp',
      async () => ({
        payload: event,
        headers: await signed(event, vector.secret, new Date(Date.now() - 600_000)),
      }),
    ],
    ['missing headers', async () => ({ payload: event, headers: {} })],
  ])('rejects %s', async (_, make) => {
    const { payload, headers } = await make();
    await expect(verifyWebhook({ payload, headers, secret: vector.secret })).rejects.toBeInstanceOf(
      WebhookVerificationError,
    );
  });

  it('is available on the client with its configured secret', async () => {
    const bridge = new Bridge({ apiKey: 'bk_test_x', webhookSecret: vector.secret });
    const e = await bridge.webhooks.verify(event, await signed(event));
    expect(e.type).toBe('message.received');
    await expect(new Bridge({ apiKey: 'bk_test_x' }).webhooks.verify(event, {})).rejects.toThrow(
      /webhook secret/,
    );
  });
});
