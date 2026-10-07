import type {
  Device,
  Message,
  MessageDetail,
  MessageList,
  operations,
  RequestLog,
  RequestLogList,
  Usage,
  UsageHistory,
  Verification,
  VerifyResult,
  WhoAmI,
} from '@bridge/api-types';
import { BridgeApiError, BridgeConnectionError, type BridgeErrorDetail } from './errors';
import {
  type VerifyWebhookOptions,
  verifyWebhook,
  type WebhookEvent,
  type WebhookEventType,
} from './webhooks';

export const VERSION = '0.4.0-rc.1';

const DEFAULT_BASE_URL = 'http://localhost:8080';

export interface BridgeOptions {
  /** A project API key (`bk_live_…` or `bk_test_…`). Defaults to the BRIDGE_API_KEY environment variable. */
  apiKey?: string;
  /** Your Bridge API URL. Defaults to BRIDGE_URL, then http://localhost:8080. */
  baseUrl?: string;
  /** Signing secret used by `bridge.webhooks.verify`. Defaults to BRIDGE_WEBHOOK_SECRET. */
  webhookSecret?: string;
  /** Per-request timeout in milliseconds. Default 30 000. */
  timeoutMs?: number;
  /** Retries for network errors, 429 and 5xx responses. Default 2. */
  maxRetries?: number;
  /** A custom fetch, e.g. for proxies or tests. Defaults to the global fetch. */
  fetch?: typeof fetch;
  /**
   * API keys must stay on servers. The SDK refuses to run in a browser unless
   * you set this, for example in a local tool that only you use.
   */
  dangerouslyAllowBrowser?: boolean;
}

export interface RequestOptions {
  /** Cancels the request. */
  signal?: AbortSignal;
  /** Overrides the client's timeout for this request. */
  timeoutMs?: number;
}

export interface SendOptions extends RequestOptions {
  /**
   * Retrying with the same key returns the original message instead of sending
   * twice. The SDK generates one when you leave it out, so its own retries are
   * always safe; pass your own (such as an order ID) to make your retries safe too.
   */
  idempotencyKey?: string;
}

/** Parameters of `bridge.messages.send`. Field names match the REST API. */
export type SendMessageParams =
  operations['sendMessage']['requestBody']['content']['application/json'];

/** Filters of `bridge.messages.list`. */
export type ListMessagesParams = NonNullable<operations['listMessages']['parameters']['query']>;

/** Parameters of `bridge.devices.test`. */
export type TestDeviceParams =
  operations['testDevice']['requestBody']['content']['application/json'];

/** Parameters of `bridge.otp.send`. */
export type SendOtpParams =
  operations['sendVerification']['requestBody']['content']['application/json'];

/** Parameters of `bridge.otp.verify`: the code, and either the verification `id` or the number `to`. */
export type VerifyOtpParams =
  operations['checkVerification']['requestBody']['content']['application/json'];

/** Options of `bridge.usageHistory`. */
export type UsageHistoryParams = NonNullable<operations['getUsageHistory']['parameters']['query']>;

/** Filters of `bridge.requestLogs.list`. */
export type ListRequestLogsParams = NonNullable<
  operations['listRequestLogs']['parameters']['query']
>;

/** An event from `bridge.events.stream`: the same envelope a webhook receives, plus its ID. */
export type StreamEvent = WebhookEvent & { id: string };

export interface StreamOptions {
  /** Only these event types. Default: every event. */
  types?: WebhookEventType[];
  /** Stops the stream; the iterator then ends without an error. */
  signal?: AbortSignal;
  /** Reconnect after the connection drops. Default true. */
  reconnect?: boolean;
  /** Called each time the stream (re)connects. */
  onConnect?: () => void;
}

/** A sent message, with whether it was a replay of an earlier request with the same idempotency key. */
export type SendResult = Message & {
  /** True when an earlier request with this idempotency key already created the message. */
  replayed: boolean;
};

type ErrorEnvelope = {
  error?: { code?: string; message?: string; request_id?: string; details?: BridgeErrorDetail[] };
};

function env(name: string): string | undefined {
  const g = globalThis as { process?: { env?: Record<string, string | undefined> } };
  return g.process?.env?.[name];
}

const sleep = (ms: number, signal?: AbortSignal): Promise<void> =>
  new Promise((resolve, reject) => {
    const t = setTimeout(resolve, ms);
    signal?.addEventListener(
      'abort',
      () => {
        clearTimeout(t);
        reject(signal.reason);
      },
      { once: true },
    );
  });

interface Call {
  method: 'GET' | 'POST';
  path: string;
  query?: Record<string, string | number | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
  /** Whether repeating the request is safe. */
  idempotent: boolean;
  options?: RequestOptions | undefined;
}

/**
 * The Bridge API client.
 *
 * ```ts
 * const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: 'https://api.example.com' });
 * const msg = await bridge.messages.send({ to: '+919876543210', message: 'Your order has shipped.' });
 * ```
 */
export class Bridge {
  readonly baseUrl: string;
  readonly messages: Messages;
  readonly devices: Devices;
  readonly otp: Otp;
  readonly requestLogs: RequestLogs;
  readonly events: Events;
  readonly webhooks: Webhooks;
  private readonly apiKey: string;
  private readonly timeoutMs: number;
  private readonly maxRetries: number;
  private readonly fetchImpl: typeof fetch;

  constructor(options: BridgeOptions = {}) {
    const g = globalThis as { window?: unknown; document?: unknown };
    if (g.window !== undefined && g.document !== undefined && !options.dangerouslyAllowBrowser) {
      throw new Error(
        'The Bridge SDK is running in a browser, which would expose your API key to anyone using the page. ' +
          'Call Bridge from your server instead, or set dangerouslyAllowBrowser if you understand the risk.',
      );
    }
    const apiKey = options.apiKey ?? env('BRIDGE_API_KEY');
    if (!apiKey) {
      throw new Error(
        'Missing API key. Pass { apiKey } or set BRIDGE_API_KEY. Create one in the dashboard under API keys.',
      );
    }
    if (!/^bk_(live|test)_/.test(apiKey)) {
      throw new Error(
        'This does not look like a Bridge API key. Keys start with bk_live_ or bk_test_.',
      );
    }
    this.apiKey = apiKey;
    this.baseUrl = (options.baseUrl ?? env('BRIDGE_URL') ?? DEFAULT_BASE_URL).replace(/\/+$/, '');
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.maxRetries = options.maxRetries ?? 2;
    const f = options.fetch ?? globalThis.fetch;
    if (typeof f !== 'function') {
      throw new Error('No fetch implementation found. Use Node.js 20 or newer, or pass { fetch }.');
    }
    this.fetchImpl = f.bind(globalThis);
    this.messages = new Messages(this);
    this.devices = new Devices(this);
    this.otp = new Otp(this);
    this.requestLogs = new RequestLogs(this);
    this.events = new Events(this);
    this.webhooks = new Webhooks(options.webhookSecret ?? env('BRIDGE_WEBHOOK_SECRET'));
  }

  /** Whether this client uses a test key, which simulates messages instead of sending them. */
  get testMode(): boolean {
    return this.apiKey.startsWith('bk_test_');
  }

  /** The key's project and environment. A cheap way to check the key works. */
  whoami(options?: RequestOptions): Promise<WhoAmI> {
    return this.request<WhoAmI>({
      method: 'GET',
      path: '/v1/whoami',
      idempotent: true,
      options,
    }).then((r) => r.body);
  }

  /** Message counts for the key's environment over the last 24 hours and 30 days. */
  usage(options?: RequestOptions): Promise<Usage> {
    return this.request<Usage>({
      method: 'GET',
      path: '/v1/usage',
      idempotent: true,
      options,
    }).then((r) => r.body);
  }

  /**
   * Daily counts for the key's environment: outgoing by status, incoming, API
   * requests and errors, p95 latency, and a per-phone breakdown.
   */
  usageHistory(params: UsageHistoryParams = {}, options?: RequestOptions): Promise<UsageHistory> {
    return this.request<UsageHistory>({
      method: 'GET',
      path: '/v1/usage/history',
      query: params,
      idempotent: true,
      options,
    }).then((r) => r.body);
  }

  /** @internal Opens a Server-Sent Events response. */
  async openStream(
    path: string,
    query: Record<string, string>,
    signal: AbortSignal,
  ): Promise<Response> {
    const url = new URL(this.baseUrl + path);
    for (const [k, v] of Object.entries(query)) if (v) url.searchParams.set(k, v);
    let res: Response;
    try {
      res = await this.fetchImpl(url, {
        headers: {
          accept: 'text/event-stream',
          authorization: `Bearer ${this.apiKey}`,
          'user-agent': `bridge-sdk-ts/${VERSION}`,
        },
        signal,
      });
    } catch (err) {
      if (signal.aborted) throw signal.reason;
      throw new BridgeConnectionError(
        `Could not reach Bridge at ${this.baseUrl}: ${err instanceof Error ? err.message : String(err)}`,
        { cause: err, timedOut: false },
      );
    }
    if (!res.ok) throw await toApiError(res);
    if (!res.body)
      throw new BridgeConnectionError('The event stream returned no body.', { timedOut: false });
    return res;
  }

  /** @internal */
  async request<T>(call: Call): Promise<{ body: T; headers: Headers }> {
    const url = new URL(this.baseUrl + call.path);
    for (const [k, v] of Object.entries(call.query ?? {})) {
      if (v !== undefined && v !== '') url.searchParams.set(k, String(v));
    }
    const headers: Record<string, string> = {
      accept: 'application/json',
      authorization: `Bearer ${this.apiKey}`,
      'user-agent': `bridge-sdk-ts/${VERSION}`,
      ...call.headers,
    };
    if (call.body !== undefined) headers['content-type'] = 'application/json';
    const timeoutMs = call.options?.timeoutMs ?? this.timeoutMs;

    for (let attempt = 0; ; attempt++) {
      const retriesLeft = call.idempotent ? this.maxRetries - attempt : 0;
      const signals = [AbortSignal.timeout(timeoutMs)];
      if (call.options?.signal) signals.push(call.options.signal);
      let res: Response;
      try {
        res = await this.fetchImpl(url, {
          method: call.method,
          headers,
          body: call.body === undefined ? undefined : JSON.stringify(call.body),
          signal: AbortSignal.any(signals),
        });
      } catch (err) {
        if (call.options?.signal?.aborted) throw call.options.signal.reason;
        const timedOut = err instanceof DOMException && err.name === 'TimeoutError';
        if (retriesLeft > 0) {
          await sleep(backoff(attempt), call.options?.signal);
          continue;
        }
        throw new BridgeConnectionError(
          timedOut
            ? `Bridge did not answer within ${timeoutMs} ms (${call.method} ${call.path}).`
            : `Could not reach Bridge at ${this.baseUrl}: ${err instanceof Error ? err.message : String(err)}`,
          { cause: err, timedOut },
        );
      }

      if (res.ok) {
        const text = await res.text();
        return { body: (text ? JSON.parse(text) : undefined) as T, headers: res.headers };
      }

      const error = await toApiError(res);
      const retryable = res.status === 429 || res.status >= 500;
      // Long waits (a full rate-limit window) are better left to the caller.
      const wait = error.retryAfter !== undefined ? error.retryAfter * 1000 : backoff(attempt);
      if (retryable && retriesLeft > 0 && wait <= 10_000) {
        await sleep(wait, call.options?.signal);
        continue;
      }
      throw error;
    }
  }
}

function backoff(attempt: number): number {
  const base = 500 * 2 ** attempt;
  return base + Math.random() * base * 0.25;
}

async function toApiError(res: Response): Promise<BridgeApiError> {
  let envelope: ErrorEnvelope = {};
  try {
    envelope = (await res.json()) as ErrorEnvelope;
  } catch {
    // Not JSON, e.g. a reverse proxy's error page.
  }
  const retryAfter = Number(res.headers.get('retry-after'));
  return new BridgeApiError({
    status: res.status,
    code: envelope.error?.code ?? (res.status >= 500 ? 'internal_error' : 'invalid_request'),
    message: envelope.error?.message ?? `Bridge responded ${res.status} ${res.statusText}.`,
    requestId: envelope.error?.request_id ?? res.headers.get('x-request-id') ?? undefined,
    details: envelope.error?.details,
    retryAfter: Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined,
  });
}

/** `bridge.messages` */
export class Messages {
  constructor(private readonly client: Bridge) {}

  /**
   * Queues an SMS and returns it with status `queued`. Bridge picks an online
   * phone (or the `device_id` you name) and records every status change.
   * With a test key the whole lifecycle is simulated and nothing is sent.
   */
  async send(params: SendMessageParams, options: SendOptions = {}): Promise<SendResult> {
    const key = options.idempotencyKey ?? `sdk_${crypto.randomUUID()}`;
    const { body, headers } = await this.client.request<Message>({
      method: 'POST',
      path: '/v1/messages',
      body: params,
      headers: { 'idempotency-key': key },
      // Safe to retry: the idempotency key makes a repeat return the same message.
      idempotent: true,
      options,
    });
    return { ...body, replayed: headers.get('idempotent-replayed') === 'true' };
  }

  /** A message with its full status timeline. */
  async get(messageId: string, options?: RequestOptions): Promise<MessageDetail> {
    const { body } = await this.client.request<MessageDetail>({
      method: 'GET',
      path: `/v1/messages/${encodeURIComponent(messageId)}`,
      idempotent: true,
      options,
    });
    return body;
  }

  /** One page of messages, newest first. Pass the last ID as `starting_after` for the next page. */
  async list(params: ListMessagesParams = {}, options?: RequestOptions): Promise<MessageList> {
    const { body } = await this.client.request<MessageList>({
      method: 'GET',
      path: '/v1/messages',
      query: params,
      idempotent: true,
      options,
    });
    return body;
  }

  /**
   * Every message matching the filters, newest first, fetching pages as you iterate.
   *
   * ```ts
   * for await (const m of bridge.messages.listAll({ status: 'failed' })) console.log(m.id);
   * ```
   */
  async *listAll(
    params: Omit<ListMessagesParams, 'starting_after'> = {},
    options?: RequestOptions,
  ): AsyncGenerator<Message, void, undefined> {
    let cursor: string | undefined;
    for (;;) {
      const page = await this.list({ limit: 100, ...params, starting_after: cursor }, options);
      yield* page.data;
      const last = page.data.at(-1);
      if (!page.has_more || !last) return;
      cursor = last.id;
    }
  }

  /**
   * Polls a message until it reaches one of the given statuses (by default a
   * final one: delivered, failed or received) and returns it.
   */
  async waitFor(
    messageId: string,
    options: RequestOptions & {
      statuses?: Message['status'][];
      /** Give up after this many milliseconds. Default 60 000. */
      timeoutMs?: number;
      /** Default 1 000. */
      intervalMs?: number;
    } = {},
  ): Promise<MessageDetail> {
    const statuses = options.statuses ?? ['delivered', 'failed', 'received'];
    const deadline = Date.now() + (options.timeoutMs ?? 60_000);
    for (;;) {
      const m = await this.get(messageId, { signal: options.signal });
      if (statuses.includes(m.status)) return m;
      if (Date.now() >= deadline) {
        throw new BridgeConnectionError(
          `Message ${messageId} is still ${m.status}; gave up waiting for ${statuses.join(' or ')}.`,
          {
            timedOut: true,
          },
        );
      }
      await sleep(options.intervalMs ?? 1_000, options.signal);
    }
  }
}

/** `bridge.devices` */
export class Devices {
  constructor(private readonly client: Bridge) {}

  /** The project's paired phones, with presence and battery. */
  async list(options?: RequestOptions): Promise<Device[]> {
    const { body } = await this.client.request<{ data: Device[] }>({
      method: 'GET',
      path: '/v1/devices',
      idempotent: true,
      options,
    });
    return body.data;
  }

  async get(deviceId: string, options?: RequestOptions): Promise<Device> {
    const { body } = await this.client.request<Device>({
      method: 'GET',
      path: `/v1/devices/${encodeURIComponent(deviceId)}`,
      idempotent: true,
      options,
    });
    return body;
  }

  /** Sends a test SMS through this phone only. */
  async test(
    deviceId: string,
    params: TestDeviceParams,
    options?: RequestOptions,
  ): Promise<Message> {
    const { body } = await this.client.request<Message>({
      method: 'POST',
      path: `/v1/devices/${encodeURIComponent(deviceId)}/test`,
      body: params,
      idempotent: false,
      options,
    });
    return body;
  }
}

/** `bridge.requestLogs`: the API requests made with the project's keys of this key's environment. */
export class RequestLogs {
  constructor(private readonly client: Bridge) {}

  /** One page, newest first. Pass the last ID as `starting_after` for the next page. */
  async list(
    params: ListRequestLogsParams = {},
    options?: RequestOptions,
  ): Promise<RequestLogList> {
    const { body } = await this.client.request<RequestLogList>({
      method: 'GET',
      path: '/v1/request-logs',
      query: params,
      idempotent: true,
      options,
    });
    return body;
  }

  /** Every entry matching the filters, newest first, fetching pages as you iterate. */
  async *listAll(
    params: Omit<ListRequestLogsParams, 'starting_after'> = {},
    options?: RequestOptions,
  ): AsyncGenerator<RequestLog, void, undefined> {
    let cursor: string | undefined;
    for (;;) {
      const page = await this.list({ limit: 100, ...params, starting_after: cursor }, options);
      yield* page.data;
      const last = page.data.at(-1);
      if (!page.has_more || !last) return;
      cursor = last.id;
    }
  }
}

/** `bridge.events`: the project's events as they happen. */
export class Events {
  constructor(private readonly client: Bridge) {}

  /**
   * Streams events (the same ones webhooks receive) for this key's
   * environment, reconnecting after drops. The stream is live only: events
   * that happen while disconnected are not replayed, so use webhooks where
   * every event matters.
   *
   * ```ts
   * const stop = new AbortController();
   * for await (const event of bridge.events.stream({ types: ['message.delivered'], signal: stop.signal })) {
   *   console.log(event.type, event.data.id);
   * }
   * ```
   */
  async *stream(options: StreamOptions = {}): AsyncGenerator<StreamEvent, void, undefined> {
    const signal = options.signal ?? new AbortController().signal;
    const query = { types: (options.types ?? []).join(',') };
    let retryMs = 3_000;
    for (let attempt = 0; ; attempt++) {
      if (signal.aborted) return;
      let res: Response;
      try {
        res = await this.client.openStream('/v1/events/stream', query, signal);
      } catch (err) {
        if (signal.aborted) return;
        // Bad keys and bad filters will not fix themselves.
        const permanent = err instanceof BridgeApiError && err.status < 500 && err.status !== 429;
        if (permanent || options.reconnect === false) throw err;
        await sleepUnlessAborted(Math.min(retryMs * 2 ** Math.min(attempt, 4), 60_000), signal);
        continue;
      }
      attempt = 0;
      options.onConnect?.();
      try {
        for await (const frame of sseFrames(res.body as ReadableStream<Uint8Array>, signal)) {
          if (frame.retry !== undefined) retryMs = frame.retry;
          if (!frame.data) continue;
          const envelope = JSON.parse(frame.data) as WebhookEvent;
          yield { ...envelope, id: frame.id ?? '' } as StreamEvent;
          if (signal.aborted) return;
        }
      } catch (err) {
        if (signal.aborted) return;
        if (options.reconnect === false) throw err;
      }
      if (options.reconnect === false) return;
      await sleepUnlessAborted(retryMs, signal);
    }
  }
}

async function sleepUnlessAborted(ms: number, signal: AbortSignal): Promise<void> {
  try {
    await sleep(ms, signal);
  } catch {
    // Aborted: the caller checks the signal.
  }
}

interface SseFrame {
  id?: string;
  event?: string;
  data: string;
  retry?: number;
}

/** Parses a Server-Sent Events body into frames. Comments (": ping") are skipped. */
export async function* sseFrames(
  body: ReadableStream<Uint8Array>,
  signal?: AbortSignal,
): AsyncGenerator<SseFrame> {
  const reader = body.getReader();
  // An idle stream would otherwise wait for the next ping after an abort.
  const cancel = () => void reader.cancel().catch(() => undefined);
  signal?.addEventListener('abort', cancel, { once: true });
  const decoder = new TextDecoder();
  let buffer = '';
  let frame: SseFrame = { data: '' };
  let hasData = false;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true });
      // Split on \n only: a \r\n broken across two chunks must not end a frame early.
      const lines = buffer.split('\n');
      buffer = lines.pop() ?? '';
      for (const raw of lines) {
        const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw;
        if (line === '') {
          if (hasData || frame.retry !== undefined) yield frame;
          frame = { data: '' };
          hasData = false;
          continue;
        }
        if (line.startsWith(':')) continue;
        const colon = line.indexOf(':');
        const field = colon === -1 ? line : line.slice(0, colon);
        let val = colon === -1 ? '' : line.slice(colon + 1);
        if (val.startsWith(' ')) val = val.slice(1);
        if (field === 'data') {
          frame.data = hasData ? `${frame.data}\n${val}` : val;
          hasData = true;
        } else if (field === 'id') frame.id = val;
        else if (field === 'event') frame.event = val;
        else if (field === 'retry' && /^\d+$/.test(val)) frame.retry = Number(val);
      }
    }
  } finally {
    signal?.removeEventListener('abort', cancel);
    reader.releaseLock();
  }
}

/** `bridge.otp`: one-time passwords that Bridge generates, sends and checks. */
export class Otp {
  constructor(private readonly client: Bridge) {}

  /**
   * Sends a new code to a number and returns the verification. A newer code
   * cancels the previous one. With a test key nothing is sent and the code is
   * returned in `code`, so tests can finish the flow without a phone.
   *
   * Not retried automatically: a retry could send the user a second code.
   */
  async send(params: SendOtpParams, options?: RequestOptions): Promise<Verification> {
    const { body } = await this.client.request<Verification>({
      method: 'POST',
      path: '/v1/otp',
      body: params,
      idempotent: false,
      options,
    });
    return body;
  }

  /**
   * Checks a code. `valid` is true only when it is right; a wrong code uses
   * one attempt, and `verification.status` says what happened (`failed` when
   * no attempts are left, `expired`, or `canceled` by a newer code).
   *
   * ```ts
   * const { valid } = await bridge.otp.verify({ to: '+919876543210', code: '482913' });
   * ```
   */
  async verify(params: VerifyOtpParams, options?: RequestOptions): Promise<VerifyResult> {
    const { body } = await this.client.request<VerifyResult>({
      method: 'POST',
      path: '/v1/otp/verify',
      body: params,
      // A repeat would use up another attempt.
      idempotent: false,
      options,
    });
    return body;
  }

  /** A verification and the delivery status of its SMS. */
  async get(otpId: string, options?: RequestOptions): Promise<Verification> {
    const { body } = await this.client.request<Verification>({
      method: 'GET',
      path: `/v1/otp/${encodeURIComponent(otpId)}`,
      idempotent: true,
      options,
    });
    return body;
  }
}

/** `bridge.webhooks` */
export class Webhooks {
  constructor(private readonly secret: string | undefined) {}

  /**
   * Verifies a webhook request with the client's `webhookSecret` (or
   * BRIDGE_WEBHOOK_SECRET) and returns the event. See {@link verifyWebhook}.
   */
  verify(
    payload: VerifyWebhookOptions['payload'],
    headers: VerifyWebhookOptions['headers'],
    options: { secret?: string; toleranceSeconds?: number } = {},
  ): Promise<WebhookEvent> {
    const secret = options.secret ?? this.secret;
    if (!secret) {
      return Promise.reject(
        new Error(
          'No webhook secret. Pass { webhookSecret } to new Bridge() or set BRIDGE_WEBHOOK_SECRET.',
        ),
      );
    }
    return verifyWebhook({ payload, headers, secret, toleranceSeconds: options.toleranceSeconds });
  }
}
