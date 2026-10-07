import { BridgeTokenError, type BridgeTokenErrorReason } from './errors';

/** Options of {@link verifyWidgetToken}. */
export interface VerifyWidgetTokenOptions {
  /**
   * The Verify app's token signing secret (`bvs_…`), from the app's page in the dashboard. Keep it
   * on your server.
   */
  secret: string;
  /** The Verify app's ID (`vap_…`). Tokens issued for any other app are rejected. */
  appId: string;
  /**
   * Your Bridge API URL (`BRIDGE_PUBLIC_URL`), such as `https://api.sms.example.com`. When set, the
   * token's `iss` must match it. A trailing slash is ignored.
   */
  issuer?: string;
  /**
   * The environment the token must come from. Default `live`: a token from a test-environment
   * widget, which sends no SMS, proves nothing about a real phone. Pass `test` only in development.
   */
  environment?: 'live' | 'test';
  /** Allowed clock difference with the Bridge server, in seconds, for `exp` and `iat`. Default 30. */
  clockToleranceSeconds?: number;
  /** For tests: the current time. */
  now?: Date;
}

/** What a valid widget token proves. */
export interface WidgetTokenClaims {
  /** The verified phone number, in E.164 format (`sub`). */
  phone: string;
  /** The verification that confirmed it, `otp_…` (`vid`). */
  verificationId: string;
  /** The Verify app the token was issued for, `vap_…` (`aud`). */
  appId: string;
  /** The widget's environment (`env`). */
  environment: 'live' | 'test';
  /** The Bridge API URL that issued the token (`iss`). */
  issuer: string;
  /** When the number was verified (`iat`). */
  issuedAt: Date;
  /** When the token stops being valid, 10 minutes after `issuedAt` (`exp`). */
  expiresAt: Date;
  /** The token's unique ID (`jti`). Store it to accept each token only once. */
  tokenId: string;
}

const encoder = new TextEncoder();
const strictDecoder = new TextDecoder('utf-8', { fatal: true });
const base64url = /^[A-Za-z0-9_-]+$/;
const maxTokenLength = 4096;

function fail(reason: BridgeTokenErrorReason, message: string): never {
  throw new BridgeTokenError(reason, message);
}

function decodeSegment(segment: string): Uint8Array<ArrayBuffer> {
  if (!base64url.test(segment) || segment.length % 4 === 1) {
    fail('malformed', 'The token is not a well-formed JWT: a segment is not base64url.');
  }
  const b64 = segment.replace(/-/g, '+').replace(/_/g, '/');
  const bin = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function decodeJson(segment: string, what: string): Record<string, unknown> {
  let value: unknown;
  try {
    value = JSON.parse(strictDecoder.decode(decodeSegment(segment)));
  } catch (err) {
    if (err instanceof BridgeTokenError) throw err;
    fail('malformed', `The token is not a well-formed JWT: its ${what} is not JSON.`);
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    fail('malformed', `The token is not a well-formed JWT: its ${what} is not a JSON object.`);
  }
  return value as Record<string, unknown>;
}

function constantTimeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= (a[i] ?? 0) ^ (b[i] ?? 0);
  return diff === 0;
}

function stringClaim(claims: Record<string, unknown>, name: string): string {
  const v = claims[name];
  if (typeof v !== 'string' || v === '') {
    fail('malformed', `The token has no ${name} claim.`);
  }
  return v;
}

function numericClaim(claims: Record<string, unknown>, name: string): number {
  const v = claims[name];
  if (typeof v !== 'number' || !Number.isFinite(v)) {
    fail('malformed', `The token has no numeric ${name} claim.`);
  }
  return v;
}

/**
 * Verifies a token from the Bridge Verify widget or hosted page on your own server, without calling
 * Bridge, and returns what it proves. It checks, in order: the JWT format, `alg` (HS256 only), the
 * HMAC-SHA256 signature under the app's secret (compared in constant time), `aud` (the app ID),
 * `iss` (when `issuer` is set), `iat` and `exp`, and `env` (`live` unless you pass `environment`).
 *
 * Throws {@link BridgeTokenError} when the token is not valid; its `reason` uses the same values
 * as `POST /v1/otp/tokens/verify`. Uses WebCrypto, so it runs on Node.js 20+, Bun, Deno and edge
 * runtimes.
 *
 * A token stays valid for 10 minutes. Accept each one once, for example by storing `tokenId` or
 * `verificationId`, and use `bridge.otp.verifyToken` when you need Bridge to confirm that the app
 * and verification still exist.
 *
 * ```ts
 * const { phone } = await verifyWidgetToken(token, {
 *   secret: process.env.BRIDGE_VERIFY_SECRET!,
 *   appId: 'vap_01ja8z3k5wq2v7c9e4r2n0w6yb',
 *   issuer: 'https://api.sms.example.com',
 * });
 * ```
 */
export async function verifyWidgetToken(
  token: string,
  options: VerifyWidgetTokenOptions,
): Promise<WidgetTokenClaims> {
  if (!options.secret) {
    throw new Error(
      "Missing the Verify app's secret. Copy it (bvs_…) from the app's page in the Bridge dashboard.",
    );
  }
  if (!options.appId) {
    throw new Error(
      "Missing the Verify app's ID (vap_…). Find it on the app's page in the dashboard.",
    );
  }
  const tolerance = options.clockToleranceSeconds ?? 30;
  if (!Number.isFinite(tolerance) || tolerance < 0) {
    throw new Error('clockToleranceSeconds must be zero or a positive number of seconds.');
  }
  const expectedEnv = options.environment ?? 'live';

  if (typeof token !== 'string') fail('malformed', 'The token is not a string.');
  const jwt = token.trim();
  const parts = jwt.split('.');
  if (parts.length !== 3 || jwt.length > maxTokenLength) {
    fail('malformed', 'The token is not a well-formed JWT: it needs three dot-separated parts.');
  }
  const [headerSegment = '', payloadSegment = '', signatureSegment = ''] = parts;

  const header = decodeJson(headerSegment, 'header');
  if (header.alg !== 'HS256') {
    fail(
      'malformed',
      `The token uses the algorithm ${JSON.stringify(header.alg ?? null)}. Bridge tokens use HS256 only.`,
    );
  }
  const signature = decodeSegment(signatureSegment);

  const key = await crypto.subtle.importKey(
    'raw',
    encoder.encode(options.secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const expected = new Uint8Array(
    await crypto.subtle.sign('HMAC', key, encoder.encode(`${headerSegment}.${payloadSegment}`)),
  );
  if (!constantTimeEqual(signature, expected)) {
    fail(
      'bad_signature',
      "The token's signature does not match. Check that you use this app's current secret; rotating it invalidates earlier tokens.",
    );
  }

  const claims = decodeJson(payloadSegment, 'payload');
  const appId = stringClaim(claims, 'aud');
  if (appId !== options.appId) {
    fail('unknown_app', `The token was issued for the Verify app ${appId}, not ${options.appId}.`);
  }
  const issuer = stringClaim(claims, 'iss');
  if (options.issuer !== undefined && issuer !== options.issuer.replace(/\/+$/, '')) {
    fail('wrong_issuer', `The token was issued by ${issuer}, not ${options.issuer}.`);
  }
  const issuedAt = numericClaim(claims, 'iat');
  const expiresAt = numericClaim(claims, 'exp');
  const now = (options.now ?? new Date()).getTime() / 1000;
  if (now - tolerance >= expiresAt) {
    fail(
      'expired',
      'The token has expired. Tokens are valid for 10 minutes; verify the number again.',
    );
  }
  if (issuedAt > now + tolerance) {
    fail('expired', "The token is not valid yet. Check this server's clock.");
  }
  const environment = claims.env;
  if (environment !== expectedEnv) {
    fail(
      'environment_mismatch',
      `The token comes from a ${typeof environment === 'string' ? environment : 'unknown'} environment widget, but ${expectedEnv} was expected.`,
    );
  }

  return {
    phone: stringClaim(claims, 'sub'),
    verificationId: stringClaim(claims, 'vid'),
    appId,
    environment: expectedEnv,
    issuer,
    issuedAt: new Date(issuedAt * 1000),
    expiresAt: new Date(expiresAt * 1000),
    tokenId: stringClaim(claims, 'jti'),
  };
}
