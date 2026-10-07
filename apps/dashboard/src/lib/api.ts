import type { ErrorBody, paths } from '@bridge/api-types';
import createClient from 'openapi-fetch';

/** Browser client. Requests go to /api/*, which the dashboard proxies to the Bridge API. */
export const api = createClient<paths>({ baseUrl: '/api' });

export class BridgeApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;
  readonly details?: ErrorBody['details'];

  constructor(status: number, body: Partial<ErrorBody> | undefined, requestId?: string) {
    super(body?.message ?? `The Bridge API returned ${status}.`);
    this.name = 'BridgeApiError';
    this.status = status;
    this.code = body?.code ?? 'internal_error';
    this.requestId = body?.request_id ?? requestId;
    this.details = body?.details ?? undefined;
  }
}

type FetchResult<T> = { data?: T; error?: unknown; response: Response };

/** Returns the response body or throws a BridgeApiError. A 401 sends the user to sign in. */
export async function unwrap<T>(request: Promise<FetchResult<T>>): Promise<T> {
  let result: FetchResult<T>;
  try {
    result = await request;
  } catch {
    throw new BridgeApiError(0, {
      code: 'network_error',
      message: 'Could not reach Bridge. Check your connection and that the API is running.',
    });
  }
  const { data, error, response } = result;
  if (!response.ok) {
    const body = (error as { error?: ErrorBody } | undefined)?.error;
    if (response.status === 401 && typeof window !== 'undefined') {
      const next = encodeURIComponent(window.location.pathname);
      window.location.assign(`/login?next=${next}`);
    }
    throw new BridgeApiError(
      response.status,
      body,
      response.headers.get('x-request-id') ?? undefined,
    );
  }
  return data as T;
}
