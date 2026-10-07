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
