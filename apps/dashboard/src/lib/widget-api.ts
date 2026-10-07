import type { WidgetConfig, WidgetSendResult, WidgetVerifyResult } from '@bridge/api-types';

/**
 * The public widget API, called by the hosted verification page through the
 * dashboard's same-origin /api proxy. No credentials: the publishable key in
 * the path identifies the app.
 */

export class WidgetError extends Error {
  readonly status: number;
  readonly code: string;
  /** Seconds, from Retry-After, when the server sent one. */
  readonly retryAfter: number | null;

  constructor(status: number, code: string, message: string, retryAfter: number | null) {
    super(message);
    this.name = 'WidgetError';
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

async function call<T>(key: string, path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`/api/v1/widget/${encodeURIComponent(key)}${path}`, {
      ...init,
      credentials: 'omit',
      cache: 'no-store',
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    });
  } catch {
    throw new WidgetError(0, 'network_error', 'Could not reach the server.', null);
  }
  const body = (await res.json().catch(() => null)) as
    | (T & { error?: { code?: string; message?: string } })
    | null;
  if (!res.ok) {
    const retry = Number(res.headers.get('retry-after'));
    throw new WidgetError(
      res.status,
      body?.error?.code ?? 'internal_error',
      body?.error?.message ?? `The server returned ${res.status}.`,
      Number.isFinite(retry) && retry > 0 ? retry : null,
    );
  }
  return body as T;
}

export const widgetApi = {
  config: (key: string) => call<WidgetConfig>(key, ''),
  checkRedirect: (key: string, redirectUri: string) =>
    call<{ ok: boolean }>(key, `/redirect-check?redirect_uri=${encodeURIComponent(redirectUri)}`),
  send: (key: string, to: string, turnstileToken?: string) =>
    call<WidgetSendResult>(key, '/send', {
      method: 'POST',
      body: JSON.stringify({ to, turnstile_token: turnstileToken || undefined }),
    }),
  verify: (key: string, verificationId: string, code: string) =>
    call<WidgetVerifyResult>(key, '/verify', {
      method: 'POST',
      body: JSON.stringify({ verification_id: verificationId, code }),
    }),
};

/** "30 seconds", "2 minutes", "1 hour". */
export function waitText(seconds: number): string {
  if (seconds < 60) return `${seconds} second${seconds === 1 ? '' : 's'}`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'}`;
  const hours = Math.ceil(minutes / 60);
  return `${hours} hour${hours === 1 ? '' : 's'}`;
}

/** What to tell the person verifying their number, for an error from send or verify. */
export function errorText(err: unknown, step: 'send' | 'verify'): string {
  if (!(err instanceof WidgetError)) return 'Something went wrong. Try again.';
  const wait = err.retryAfter ? ` Try again in ${waitText(err.retryAfter)}.` : ' Try again later.';
  switch (true) {
    case err.status === 0:
      return 'Could not reach the verification service. Check your connection and try again.';
    case err.code === 'otp_blocked' && err.status === 403:
      return /captcha/i.test(err.message)
        ? 'The security check did not pass. Try again.'
        : 'Codes cannot be sent to numbers in this country. Use a different number.';
    case err.code === 'otp_blocked':
      return `Too many codes were requested.${wait}`;
    case err.status === 429:
      return step === 'send'
        ? `Please wait before asking for another code.${wait}`
        : `Too many tries.${wait}`;
    case err.status === 503:
      return 'The security check is not available right now. Try again in a moment.';
    case err.status === 404 && step === 'verify':
      return 'This code is no longer valid. Send a new one.';
    case err.status === 404:
      return 'This verification link is not valid.';
    case err.status === 409:
      return 'Verification is not available for this app right now.';
    case err.status === 400 || err.status === 422:
      return step === 'send'
        ? 'Check the phone number and try again.'
        : 'Check the code and try again.';
    default:
      return 'Something went wrong. Try again.';
  }
}
