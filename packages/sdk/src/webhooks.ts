import type { Broadcast, Message, Verification, VerifyBlock } from '@bridge/api-types';
import { WebhookVerificationError } from './errors';

/** Data of `device.online` and `device.offline` events. */
export interface WebhookDevice {
  id: string;
  name: string;
  status: 'online' | 'offline' | 'disabled';
  battery_level: number | null;
  is_charging: boolean | null;
  network_type: string | null;
  last_seen_at: string | null;
}

/** Data of `message.auto_replied`: an auto-reply rule ran for an incoming SMS. */
export interface WebhookAutoReply {
  environment: 'live' | 'test';
  /** The incoming SMS that matched. */
  message: Message;
  rule_id: string;
  rule_name: string;
  /** The keyword that matched, as written in the rule. */
  keyword: string;
  /** `opt_out` added the sender to the opt-out list; `opt_in` removed them. */
  action: 'none' | 'opt_out' | 'opt_in';
  /** The reply Bridge sent, or null when the rule has no reply or the reply was skipped. */
  reply_message_id: string | null;
}

/** Data of the `webhook.test` event sent by "Send test event". */
export interface WebhookTest {
  endpoint_id: string;
  message: string;
}

interface Envelope<T extends string, D> {
  type: T;
  /** When the event happened (RFC 3339). */
  timestamp: string;
  data: D;
}

/** A verified webhook event. Narrow on `type` to get the right `data`. */
export type WebhookEvent =
  | Envelope<'message.sent' | 'message.delivered' | 'message.failed' | 'message.received', Message>
  | Envelope<'message.auto_replied', WebhookAutoReply>
  | Envelope<'device.online' | 'device.offline', WebhookDevice>
  | Envelope<'otp.verified' | 'otp.failed' | 'otp.expired', Verification>
  | Envelope<'otp.blocked', VerifyBlock>
  | Envelope<'broadcast.completed', Broadcast>
  | Envelope<'webhook.test', WebhookTest>;

export type WebhookEventType = WebhookEvent['type'];

/** Request headers in any common shape: Fetch `Headers`, Node's `IncomingHttpHeaders`, or a plain object. */
export type WebhookHeaders =
  | Headers
  | Record<string, string | string[] | undefined>
  | { get(name: string): string | null };

export interface VerifyWebhookOptions {
  /** The raw request body, exactly as received. A re-serialised body will not verify. */
  payload: string | Uint8Array | ArrayBuffer;
  headers: WebhookHeaders;
  /** The endpoint's signing secret (`whsec_…`). */
  secret: string;
  /** Largest accepted difference between the webhook timestamp and now, in seconds. Default 300. */
  toleranceSeconds?: number;
  /** For tests: the current time. */
  now?: Date;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function header(headers: WebhookHeaders, name: string): string | undefined {
  if (typeof (headers as Headers).get === 'function') {
    return (headers as Headers).get(name) ?? undefined;
  }
  const record = headers as Record<string, string | string[] | undefined>;
  const key = Object.keys(record).find((k) => k.toLowerCase() === name);
  const value = key === undefined ? undefined : record[key];
  return Array.isArray(value) ? value.join(' ') : value;
}

function toBytes(payload: string | Uint8Array | ArrayBuffer): Uint8Array {
  if (typeof payload === 'string') return encoder.encode(payload);
  return payload instanceof Uint8Array ? payload : new Uint8Array(payload);
}

function base64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

/** Computes the Standard Webhooks signature (`v1,…`) of a payload. */
export async function signWebhook(
  secret: string,
  msgId: string,
  timestampSeconds: number,
  payload: string | Uint8Array | ArrayBuffer,
): Promise<string> {
  let key: Uint8Array<ArrayBuffer>;
  try {
    key = base64ToBytes(secret.replace(/^whsec_/, ''));
  } catch {
    throw new WebhookVerificationError(
      'The webhook secret is malformed. Copy it again from the dashboard (whsec_…).',
    );
  }
  if (key.length === 0) {
    throw new WebhookVerificationError('The webhook secret is empty.');
  }
  const body = toBytes(payload);
  const prefix = encoder.encode(`${msgId}.${timestampSeconds}.`);
  const signed = new Uint8Array(prefix.length + body.length);
  signed.set(prefix);
  signed.set(body, prefix.length);
  const cryptoKey = await crypto.subtle.importKey(
    'raw',
    key,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const mac = new Uint8Array(await crypto.subtle.sign('HMAC', cryptoKey, signed));
  return `v1,${bytesToBase64(mac)}`;
}

/**
 * Verifies a webhook from Bridge and returns the parsed event. Throws
 * {@link WebhookVerificationError} when the signature or timestamp is wrong;
 * respond with 400 in that case.
 *
 * Retries reuse the same `webhook-id`, so skip IDs you have already handled.
 */
export async function verifyWebhook(options: VerifyWebhookOptions): Promise<WebhookEvent> {
  const id = header(options.headers, 'webhook-id');
  const ts = header(options.headers, 'webhook-timestamp');
  const signatures = header(options.headers, 'webhook-signature');
  if (!id || !ts || !signatures) {
    throw new WebhookVerificationError(
      'Missing webhook-id, webhook-timestamp or webhook-signature header.',
    );
  }
  const timestamp = Number(ts);
  if (!Number.isInteger(timestamp)) {
    throw new WebhookVerificationError('The webhook-timestamp header is not a Unix timestamp.');
  }
  const now = Math.floor((options.now ?? new Date()).getTime() / 1000);
  const tolerance = options.toleranceSeconds ?? 300;
  if (Math.abs(now - timestamp) > tolerance) {
    throw new WebhookVerificationError(
      'The webhook timestamp is too far from the current time. Check the server clock, or this may be a replayed request.',
    );
  }
  const expected = await signWebhook(options.secret, id, timestamp, options.payload);
  const matches = signatures
    .split(' ')
    .some((sig) => sig.startsWith('v1,') && timingSafeEqual(sig, expected));
  if (!matches) {
    throw new WebhookVerificationError(
      'No webhook signature matches. Check that you use this endpoint’s signing secret and the raw request body.',
    );
  }
  const body = options.payload;
  const text = typeof body === 'string' ? body : decoder.decode(toBytes(body));
  try {
    return JSON.parse(text) as WebhookEvent;
  } catch {
    throw new WebhookVerificationError('The webhook body is not valid JSON.');
  }
}
