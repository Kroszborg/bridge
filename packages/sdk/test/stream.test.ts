import { describe, expect, it, vi } from 'vitest';
import { Bridge, BridgeApiError, sseFrames } from '../src/index';

/** A response body that delivers `chunks` one by one, optionally never ending. */
function body(chunks: string[], { hang = false } = {}): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  let i = 0;
  return new ReadableStream({
    pull(controller) {
      if (i < chunks.length) {
        controller.enqueue(enc.encode(chunks[i++]));
      } else if (!hang) {
        controller.close();
      }
    },
  });
}

const sse = (chunks: string[], opts?: { hang?: boolean }) =>
  new Response(body(chunks, opts), {
    status: 200,
    headers: { 'content-type': 'text/event-stream' },
  });

const event = (id: string, type: string, data: unknown) =>
  `id: ${id}\nevent: ${type}\ndata: ${JSON.stringify({ type, timestamp: '2026-10-07T09:00:00Z', data })}\n\n`;

function client(responses: (() => Response)[]) {
  const requests: Request[] = [];
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push(new Request(input, init));
    const r = responses[Math.min(requests.length - 1, responses.length - 1)];
    if (!r) throw new Error('no response');
    return r();
  });
  return {
    bridge: new Bridge({ apiKey: 'bk_test_x', baseUrl: 'https://api.example.com', fetch }),
    requests,
  };
}

async function collect<T>(it: AsyncIterable<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const v of it) out.push(v);
  return out;
}

describe('sseFrames', () => {
  it('parses frames split across chunks, skipping comments', async () => {
    const frames = await collect(
      sseFrames(
        body([
          'retry: 2500\n: connected\n\n',
          'id: evt_1\nevent: message.sent\nda',
          'ta: {"a":1}\r',
          '\n\n: ping\n\ndata: line one\ndata: line two\n\n',
        ]),
      ),
    );
    expect(frames).toEqual([
      { data: '', retry: 2500 },
      { id: 'evt_1', event: 'message.sent', data: '{"a":1}' },
      { data: 'line one\nline two' },
    ]);
  });
});

describe('events.stream', () => {
  it('yields events with their IDs and filters by type', async () => {
    const { bridge, requests } = client([
      () => sse([': connected\n\n', event('evt_1', 'message.delivered', { id: 'msg_1' })]),
    ]);
    const events = await collect(
      bridge.events.stream({ types: ['message.delivered'], reconnect: false }),
    );
    expect(events).toEqual([
      {
        id: 'evt_1',
        type: 'message.delivered',
        timestamp: '2026-10-07T09:00:00Z',
        data: { id: 'msg_1' },
      },
    ]);
    const url = new URL(requests[0]?.url ?? '');
    expect(url.pathname).toBe('/v1/events/stream');
    expect(url.searchParams.get('types')).toBe('message.delivered');
    expect(requests[0]?.headers.get('authorization')).toBe('Bearer bk_test_x');
  });

  it('reconnects after the connection drops, until aborted', async () => {
    const stop = new AbortController();
    const connects = vi.fn();
    const { bridge, requests } = client([
      () => sse(['retry: 1\n\n', event('evt_1', 'device.online', { id: 'dev_1' })]),
      () =>
        sse(['retry: 1\n\n', event('evt_2', 'device.offline', { id: 'dev_1' })], { hang: true }),
    ]);
    const seen: string[] = [];
    for await (const e of bridge.events.stream({ signal: stop.signal, onConnect: connects })) {
      seen.push(e.id);
      if (seen.length === 2) stop.abort();
    }
    expect(seen).toEqual(['evt_1', 'evt_2']);
    expect(connects).toHaveBeenCalledTimes(2);
    expect(requests).toHaveLength(2);
  });

  it('does not retry a rejected key', async () => {
    const { bridge, requests } = client([
      () =>
        new Response(JSON.stringify({ error: { code: 'invalid_api_key', message: 'nope' } }), {
          status: 401,
          headers: { 'content-type': 'application/json' },
        }),
    ]);
    await expect(collect(bridge.events.stream())).rejects.toBeInstanceOf(BridgeApiError);
    expect(requests).toHaveLength(1);
  });
});

describe('usage history and request logs', () => {
  it('passes filters as query parameters', async () => {
    const json = (b: unknown) =>
      new Response(JSON.stringify(b), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    const { bridge, requests } = client([
      () => json({ days: [], devices: [] }),
      () => json({ data: [{ id: 'log_2' }], has_more: true }),
      () => json({ data: [{ id: 'log_1' }], has_more: false }),
    ]);
    await bridge.usageHistory({ days: 7, tz: 'Asia/Kolkata' });
    const u = new URL(requests[0]?.url ?? '');
    expect(u.pathname).toBe('/v1/usage/history');
    expect(u.searchParams.get('tz')).toBe('Asia/Kolkata');

    const ids: string[] = [];
    for await (const l of bridge.requestLogs.listAll({ status: 'error' })) ids.push(l.id);
    expect(ids).toEqual(['log_2', 'log_1']);
    const second = new URL(requests[2]?.url ?? '');
    expect(second.pathname).toBe('/v1/request-logs');
    expect(second.searchParams.get('starting_after')).toBe('log_2');
    expect(second.searchParams.get('status')).toBe('error');
  });
});
