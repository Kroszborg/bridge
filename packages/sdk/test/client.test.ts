import { afterEach, describe, expect, it, vi } from 'vitest';
import { Bridge, BridgeApiError, BridgeConnectionError } from '../src/index';

type Handler = (req: Request) => Response | Promise<Response>;

/** A client whose fetch is answered by `handlers` in order, recording every request. */
function mockClient(handlers: Handler[], options: ConstructorParameters<typeof Bridge>[0] = {}) {
  const requests: Request[] = [];
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = new Request(input, init);
    requests.push(req);
    const h = handlers[Math.min(requests.length - 1, handlers.length - 1)];
    if (!h) throw new Error('no handler');
    return h(req);
  });
  const bridge = new Bridge({
    apiKey: 'bk_test_abc',
    baseUrl: 'https://api.example.com/',
    fetch,
    ...options,
  });
  return { bridge, requests, fetch };
}

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });

const message = { id: 'msg_1', status: 'queued', to: '+919876543210', direction: 'outbound' };

afterEach(() => {
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

describe('configuration', () => {
  it('needs a Bridge API key', () => {
    vi.stubEnv('BRIDGE_API_KEY', '');
    expect(() => new Bridge()).toThrow(/Missing API key/);
    expect(() => new Bridge({ apiKey: 'sk_live_nope' })).toThrow(/bk_live_/);
  });

  it('reads the environment', () => {
    vi.stubEnv('BRIDGE_API_KEY', 'bk_live_fromenv');
    vi.stubEnv('BRIDGE_URL', 'https://bridge.internal/');
    const bridge = new Bridge();
    expect(bridge.baseUrl).toBe('https://bridge.internal');
    expect(bridge.testMode).toBe(false);
  });

  it('refuses to run in a browser by default', () => {
    vi.stubGlobal('window', {});
    vi.stubGlobal('document', {});
    try {
      expect(() => new Bridge({ apiKey: 'bk_test_x' })).toThrow(/browser/);
      expect(
        () => new Bridge({ apiKey: 'bk_test_x', dangerouslyAllowBrowser: true }),
      ).not.toThrow();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe('messages', () => {
  it('sends with auth, an idempotency key and the REST field names', async () => {
    const { bridge, requests } = mockClient([() => json(202, message)]);
    const sent = await bridge.messages.send(
      { to: '+919876543210', message: 'hi', sim_slot: 2, metadata: { order: 'A1' } },
      { idempotencyKey: 'order-A1' },
    );
    const req = requests[0];
    expect(req?.method).toBe('POST');
    expect(req?.url).toBe('https://api.example.com/v1/messages');
    expect(req?.headers.get('authorization')).toBe('Bearer bk_test_abc');
    expect(req?.headers.get('idempotency-key')).toBe('order-A1');
    expect(req?.headers.get('user-agent')).toMatch(/^bridge-sdk-ts\//);
    expect(await req?.json()).toEqual({
      to: '+919876543210',
      message: 'hi',
      sim_slot: 2,
      metadata: { order: 'A1' },
    });
    expect(sent).toMatchObject({ id: 'msg_1', replayed: false });
  });

  it('reports replays and generates an idempotency key when none is given', async () => {
    const { bridge, requests } = mockClient([
      () => json(200, message, { 'idempotent-replayed': 'true' }),
    ]);
    const sent = await bridge.messages.send({ to: '+919876543210', message: 'hi' });
    expect(sent.replayed).toBe(true);
    expect(requests[0]?.headers.get('idempotency-key')).toMatch(/^sdk_[0-9a-f-]{36}$/);
  });

  it('retries a send after a 5xx with the same idempotency key', async () => {
    const { bridge, requests } = mockClient([
      () => json(503, { error: { code: 'service_unavailable', message: 'down' } }),
      () => json(202, message),
    ]);
    await bridge.messages.send({ to: '+919876543210', message: 'hi' });
    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers.get('idempotency-key')).toBe(
      requests[1]?.headers.get('idempotency-key'),
    );
  });

  it('turns API errors into BridgeApiError with code, details and request ID', async () => {
    const { bridge } = mockClient([
      () =>
        json(422, {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            request_id: 'req_1',
            details: [{ location: 'body.to', message: 'Use E.164 format.' }],
          },
        }),
    ]);
    const err = await bridge.messages.send({ to: '123', message: 'hi' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(BridgeApiError);
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', requestId: 'req_1' });
    expect((err as BridgeApiError).details[0]?.location).toBe('body.to');
  });

  it('honours a short Retry-After and gives up on a long one', async () => {
    const limited = (secs: string) => () =>
      json(429, { error: { code: 'rate_limited', message: 'slow down' } }, { 'retry-after': secs });
    const short = mockClient([limited('1'), () => json(200, { data: [], has_more: false })]);
    await expect(short.bridge.messages.list()).resolves.toEqual({ data: [], has_more: false });
    expect(short.requests).toHaveLength(2);

    const long = mockClient([limited('1800')]);
    const err = await long.bridge.messages.list().catch((e: unknown) => e);
    expect(err).toMatchObject({ code: 'rate_limited', retryAfter: 1800 });
    expect(long.requests).toHaveLength(1);
  });

  it('does not retry client errors', async () => {
    const { bridge, requests } = mockClient([
      () => json(404, { error: { code: 'not_found', message: 'nope' } }),
    ]);
    await expect(bridge.messages.get('msg_x')).rejects.toMatchObject({ code: 'not_found' });
    expect(requests).toHaveLength(1);
  });

  it('wraps network failures after retrying', async () => {
    const { bridge, requests } = mockClient([() => Promise.reject(new TypeError('fetch failed'))], {
      maxRetries: 1,
    });
    const err = await bridge.whoami().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(BridgeConnectionError);
    expect((err as Error).message).toMatch(/Could not reach Bridge at https:\/\/api.example.com/);
    expect(requests).toHaveLength(2);
  });

  it('times out', async () => {
    const { bridge } = mockClient(
      [
        (req) =>
          new Promise((_, reject) =>
            req.signal.addEventListener('abort', () => reject(req.signal.reason)),
          ),
      ],
      { timeoutMs: 50, maxRetries: 0 },
    );
    await expect(bridge.whoami()).rejects.toMatchObject({
      name: 'BridgeConnectionError',
      timedOut: true,
    });
  });

  it('sends filters as query parameters and paginates with listAll', async () => {
    const page = (ids: string[], more: boolean) => () =>
      json(200, { data: ids.map((id) => ({ id })), has_more: more });
    const { bridge, requests } = mockClient([page(['m3', 'm2'], true), page(['m1'], false)]);
    const ids: string[] = [];
    for await (const m of bridge.messages.listAll({ direction: 'inbound', from: 'AX-HDFCBK' }))
      ids.push(m.id);
    expect(ids).toEqual(['m3', 'm2', 'm1']);
    const first = new URL(requests[0]?.url ?? '');
    expect(Object.fromEntries(first.searchParams)).toEqual({
      limit: '100',
      direction: 'inbound',
      from: 'AX-HDFCBK',
    });
    expect(new URL(requests[1]?.url ?? '').searchParams.get('starting_after')).toBe('m2');
  });

  it('waits for a final status', async () => {
    const { bridge, requests } = mockClient([
      () => json(200, { ...message, status: 'sending', events: [] }),
      () => json(200, { ...message, status: 'delivered', events: [] }),
    ]);
    const done = await bridge.messages.waitFor('msg_1', { intervalMs: 5 });
    expect(done.status).toBe('delivered');
    expect(requests).toHaveLength(2);
  });
});

describe('devices and account', () => {
  it('unwraps the device list and calls the right paths', async () => {
    const { bridge, requests } = mockClient([
      () => json(200, { data: [{ id: 'dev_1', name: 'Pixel 7' }] }),
      () => json(202, message),
      () => json(200, { project_id: 'prj_1', environment: 'test' }),
    ]);
    expect(await bridge.devices.list()).toEqual([{ id: 'dev_1', name: 'Pixel 7' }]);
    await bridge.devices.test('dev_1', { to: '+919876543210', message: 'test' });
    await bridge.whoami();
    expect(requests.map((r) => `${r.method} ${new URL(r.url).pathname}`)).toEqual([
      'GET /v1/devices',
      'POST /v1/devices/dev_1/test',
      'GET /v1/whoami',
    ]);
  });
});

describe('otp', () => {
  const verification = { id: 'otp_1', status: 'pending', to: '+919876543210', code: '482913' };

  it('sends a code without retrying, so users never get two', async () => {
    const { bridge, requests } = mockClient([
      () => json(500, { error: { code: 'internal_error', message: 'boom' } }),
    ]);
    await expect(bridge.otp.send({ to: '+919876543210' })).rejects.toBeInstanceOf(BridgeApiError);
    expect(requests).toHaveLength(1);
    expect(requests[0]?.method).toBe('POST');
    expect(new URL(requests[0]?.url ?? '').pathname).toBe('/v1/otp');
    expect(requests[0]?.headers.get('idempotency-key')).toBeNull();
  });

  it('sends, verifies and reads', async () => {
    const { bridge, requests } = mockClient([
      () => json(201, verification),
      () => json(200, { valid: true, verification: { ...verification, status: 'verified' } }),
      () => json(200, { ...verification, status: 'verified' }),
    ]);
    const sent = await bridge.otp.send({ to: '+919876543210', android_app_hash: 'FA+9qCX9VSu' });
    expect(sent.code).toBe('482913');
    expect(await requests[0]?.json()).toEqual({
      to: '+919876543210',
      android_app_hash: 'FA+9qCX9VSu',
    });

    const res = await bridge.otp.verify({ id: sent.id, code: '482913' });
    expect(res.valid).toBe(true);
    expect(new URL(requests[1]?.url ?? '').pathname).toBe('/v1/otp/verify');

    const got = await bridge.otp.get('otp_1');
    expect(got.status).toBe('verified');
    expect(new URL(requests[2]?.url ?? '').pathname).toBe('/v1/otp/otp_1');
  });

  it('passes the Verify app and the end user IP through', async () => {
    const { bridge, requests } = mockClient([
      () => json(201, { ...verification, app_id: 'vap_1' }),
      () => json(200, { valid: false, verification }),
    ]);
    await bridge.otp.send({ to: '+919876543210', app: 'checkout', client_ip: '203.0.113.7' });
    expect(await requests[0]?.json()).toEqual({
      to: '+919876543210',
      app: 'checkout',
      client_ip: '203.0.113.7',
    });
    await bridge.otp.verify({ to: '+919876543210', app: 'checkout', code: '000000' });
    expect(await requests[1]?.json()).toEqual({
      to: '+919876543210',
      app: 'checkout',
      code: '000000',
    });
  });

  it('surfaces fraud protection blocks with their code and Retry-After', async () => {
    const { bridge, requests } = mockClient([
      () =>
        json(
          429,
          { error: { code: 'otp_blocked', message: 'Too many codes from this IP.' } },
          { 'retry-after': '1800' },
        ),
    ]);
    const err = await bridge.otp
      .send({ to: '+919876543210', client_ip: '203.0.113.7' })
      .catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 429, code: 'otp_blocked', retryAfter: 1800 });
    expect(requests).toHaveLength(1);
  });

  it('surfaces the resend cooldown as a rate-limit error', async () => {
    const { bridge } = mockClient([
      () =>
        json(429, { error: { code: 'rate_limited', message: 'wait' } }, { 'retry-after': '25' }),
    ]);
    const err = await bridge.otp.send({ to: '+919876543210' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(BridgeApiError);
    expect((err as BridgeApiError).code).toBe('rate_limited');
    expect((err as BridgeApiError).retryAfter).toBe(25);
  });
});

describe('broadcasts', () => {
  const broadcast = { id: 'brd_1', status: 'sending', counts: { recipients: 2 } };
  const params = {
    name: 'October newsletter',
    template: 'Hi {name}',
    recipients: [
      { to: '+919876543210', vars: { name: 'Asha' } },
      { to: '+919812345678', vars: { name: 'Ravi' } },
    ],
  };

  it('creates without retrying, so a broadcast is never created twice', async () => {
    const { bridge, requests } = mockClient([
      () => json(503, { error: { code: 'service_unavailable', message: 'down' } }),
    ]);
    await expect(bridge.broadcasts.create(params)).rejects.toBeInstanceOf(BridgeApiError);
    expect(requests).toHaveLength(1);
    expect(requests[0]?.method).toBe('POST');
    expect(new URL(requests[0]?.url ?? '').pathname).toBe('/v1/broadcasts');
    expect(await requests[0]?.json()).toEqual(params);
  });

  it('previews with dry_run', async () => {
    const preview = { dry_run: true, recipients: 2, skipped_opted_out: 0, duplicates: 0 };
    const { bridge, requests } = mockClient([() => json(200, preview)]);
    expect(await bridge.broadcasts.preview(params)).toEqual(preview);
    expect(await requests[0]?.json()).toEqual({ ...params, dry_run: true });
  });

  it('gets, lists, pages and cancels', async () => {
    const { bridge, requests } = mockClient([
      () => json(201, broadcast),
      () => json(200, broadcast),
      () => json(200, { data: [{ id: 'brd_2' }], has_more: true }),
      () => json(200, { data: [{ id: 'brd_1' }], has_more: false }),
      () => json(200, { ...broadcast, status: 'canceled' }),
    ]);
    await bridge.broadcasts.create(params);
    await bridge.broadcasts.get('brd_1');
    const ids: string[] = [];
    for await (const b of bridge.broadcasts.listAll({ status: 'sending' })) ids.push(b.id);
    expect(ids).toEqual(['brd_2', 'brd_1']);
    const canceled = await bridge.broadcasts.cancel('brd_1');
    expect(canceled.status).toBe('canceled');
    expect(requests.map((r) => `${r.method} ${new URL(r.url).pathname}`)).toEqual([
      'POST /v1/broadcasts',
      'GET /v1/broadcasts/brd_1',
      'GET /v1/broadcasts',
      'GET /v1/broadcasts',
      'POST /v1/broadcasts/brd_1/cancel',
    ]);
    expect(Object.fromEntries(new URL(requests[2]?.url ?? '').searchParams)).toEqual({
      limit: '100',
      status: 'sending',
    });
    expect(new URL(requests[3]?.url ?? '').searchParams.get('starting_after')).toBe('brd_2');
  });

  it('waits until the broadcast completes or is canceled', async () => {
    const { bridge, requests } = mockClient([
      () => json(200, broadcast),
      () => json(200, { ...broadcast, status: 'completed' }),
    ]);
    const done = await bridge.broadcasts.waitFor('brd_1', { intervalMs: 5 });
    expect(done.status).toBe('completed');
    expect(requests).toHaveLength(2);

    const { bridge: other } = mockClient([() => json(200, { ...broadcast, status: 'canceled' })]);
    expect((await other.broadcasts.waitFor('brd_1')).status).toBe('canceled');
  });

  it('gives up waiting after the timeout', async () => {
    const { bridge } = mockClient([() => json(200, broadcast)]);
    const err = await bridge.broadcasts
      .waitFor('brd_1', { timeoutMs: 0, intervalMs: 5 })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(BridgeConnectionError);
    expect(err).toMatchObject({ timedOut: true });
  });
});

describe('schedules', () => {
  const schedule = { id: 'sch_1', status: 'active', paused: false };

  it('calls every endpoint with the right method, path and body', async () => {
    const { bridge, requests } = mockClient([
      () => json(201, schedule),
      () => json(200, schedule),
      () => json(200, { data: [schedule], has_more: false }),
      () => json(200, { ...schedule, name: 'Standup' }),
      () => json(200, { ...schedule, status: 'paused', paused: true }),
      () => json(200, schedule),
      () => json(200, schedule),
      () => new Response(null, { status: 204 }),
    ]);
    const timing = {
      kind: 'weekly' as const,
      days: ['mon' as const, 'fri' as const],
      at: '09:45',
      time_zone: 'Asia/Kolkata',
    };
    await bridge.schedules.create({ to: '+919876543210', message: 'Standup', schedule: timing });
    await bridge.schedules.get('sch_1');
    const ids: string[] = [];
    for await (const s of bridge.schedules.listAll()) ids.push(s.id);
    expect(ids).toEqual(['sch_1']);
    await bridge.schedules.update('sch_1', { name: 'Standup', ends_at: '' });
    expect((await bridge.schedules.pause('sch_1')).paused).toBe(true);
    await bridge.schedules.resume('sch_1');
    await bridge.schedules.run('sch_1');
    await expect(bridge.schedules.delete('sch_1')).resolves.toBeUndefined();

    expect(requests.map((r) => `${r.method} ${new URL(r.url).pathname}`)).toEqual([
      'POST /v1/schedules',
      'GET /v1/schedules/sch_1',
      'GET /v1/schedules',
      'PATCH /v1/schedules/sch_1',
      'POST /v1/schedules/sch_1/pause',
      'POST /v1/schedules/sch_1/resume',
      'POST /v1/schedules/sch_1/run',
      'DELETE /v1/schedules/sch_1',
    ]);
    expect(await requests[0]?.json()).toEqual({
      to: '+919876543210',
      message: 'Standup',
      schedule: timing,
    });
    expect(await requests[3]?.json()).toEqual({ name: 'Standup', ends_at: '' });
  });

  it('does not retry run now, so a message is never sent twice', async () => {
    const { bridge, requests } = mockClient([
      () => json(502, { error: { code: 'internal_error', message: 'bad gateway' } }),
    ]);
    await expect(bridge.schedules.run('sch_1')).rejects.toBeInstanceOf(BridgeApiError);
    expect(requests).toHaveLength(1);
  });
});

describe('opt-outs', () => {
  const entry = { id: 'uns_1', number: '+919876543210', source: 'api', keyword: null };

  it('adds, lists and removes, encoding + in paths', async () => {
    const { bridge, requests } = mockClient([
      () => json(201, entry),
      () => json(200, { data: [entry], has_more: false }),
      () => new Response(null, { status: 204 }),
    ]);
    expect(await bridge.optOuts.add('+919876543210')).toEqual(entry);
    expect(await requests[0]?.json()).toEqual({ number: '+919876543210' });
    const all: string[] = [];
    for await (const o of bridge.optOuts.listAll({ source: 'keyword' })) all.push(o.number);
    expect(all).toEqual(['+919876543210']);
    await bridge.optOuts.remove('+919876543210');
    expect(requests.map((r) => `${r.method} ${r.url}`)).toEqual([
      'POST https://api.example.com/v1/opt-outs',
      'GET https://api.example.com/v1/opt-outs?limit=100&source=keyword',
      'DELETE https://api.example.com/v1/opt-outs/%2B919876543210',
    ]);
  });

  it('checks a number: 404 means it may be messaged', async () => {
    const { bridge, requests } = mockClient([
      () => json(200, entry),
      () => json(404, { error: { code: 'not_found', message: 'Opt-out for +919812345678' } }),
    ]);
    expect(await bridge.optOuts.isOptedOut('+919876543210')).toBe(true);
    expect(await bridge.optOuts.isOptedOut('+919812345678')).toBe(false);
    expect(new URL(requests[0]?.url ?? '').pathname).toBe('/v1/opt-outs/%2B919876543210');
  });

  it('still throws other errors from isOptedOut', async () => {
    const { bridge } = mockClient([
      () => json(401, { error: { code: 'invalid_api_key', message: 'bad key' } }),
    ]);
    await expect(bridge.optOuts.isOptedOut('+919876543210')).rejects.toMatchObject({
      code: 'invalid_api_key',
    });
  });

  it('surfaces opted_out on sends', async () => {
    const { bridge } = mockClient([
      () => json(409, { error: { code: 'opted_out', message: '+919876543210 opted out' } }),
    ]);
    const err = await bridge.messages
      .send({ to: '+919876543210', message: 'hi' })
      .catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 409, code: 'opted_out' });
  });
});
