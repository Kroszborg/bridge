import type { TokenVerification } from '@bridge/api-types';

/** Stable error codes returned by the Bridge API. New codes may be added. */
export type BridgeErrorCode =
  | 'unauthenticated'
  | 'invalid_api_key'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'validation_failed'
  | 'invalid_request'
  | 'rate_limited'
  | 'otp_blocked'
  | 'opted_out'
  | 'service_unavailable'
  | 'internal_error'
  | (string & {});

/** One invalid field in a `validation_failed` error. */
export interface BridgeErrorDetail {
  /** Where the problem is, e.g. `body.to`. */
  location?: string;
  message?: string;
  value?: unknown;
}

/** Base class of every error the SDK throws. */
export class BridgeError extends Error {
  override name = 'BridgeError';
}

/**
 * The API answered with an error. Use `code` in your code and quote
 * `requestId` when asking for help: it identifies the request in server logs.
 */
export class BridgeApiError extends BridgeError {
  override name = 'BridgeApiError';
  readonly status: number;
  readonly code: BridgeErrorCode;
  readonly requestId: string | undefined;
  readonly details: BridgeErrorDetail[];
  /** Seconds to wait before retrying, from the Retry-After header. */
  readonly retryAfter: number | undefined;

  constructor(init: {
    status: number;
    code: BridgeErrorCode;
    message: string;
    requestId?: string | undefined;
    details?: BridgeErrorDetail[] | undefined;
    retryAfter?: number | undefined;
  }) {
    super(init.message);
    this.status = init.status;
    this.code = init.code;
    this.requestId = init.requestId;
    this.details = init.details ?? [];
    this.retryAfter = init.retryAfter;
  }
}

/** The request never got an answer: network failure, DNS error or timeout. */
export class BridgeConnectionError extends BridgeError {
  override name = 'BridgeConnectionError';
  readonly timedOut: boolean;

  constructor(message: string, options: { cause?: unknown; timedOut?: boolean } = {}) {
    super(message, { cause: options.cause });
    this.timedOut = options.timedOut ?? false;
  }
}

/** A webhook request failed signature or timestamp verification. Reject it. */
export class WebhookVerificationError extends BridgeError {
  override name = 'WebhookVerificationError';
}

/**
 * Why a Verify widget token is not valid. The same values as `reason` in the response of
 * `POST /v1/otp/tokens/verify`. Checking a token locally can produce every reason except
 * `verification_mismatch`, which needs Bridge's records; there, `unknown_app` means the token's
 * `aud` is not the app ID you passed.
 */
export type BridgeTokenErrorReason = NonNullable<TokenVerification['reason']>;

/** A Verify widget token failed verification. Do not treat the phone number as verified. */
export class BridgeTokenError extends BridgeError {
  override name = 'BridgeTokenError';
  readonly reason: BridgeTokenErrorReason;

  constructor(reason: BridgeTokenErrorReason, message: string) {
    super(message);
    this.reason = reason;
  }
}
