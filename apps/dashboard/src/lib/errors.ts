import { toast } from 'sonner';
import { BridgeApiError } from './api';

/** Shows an API error as a toast, with the request ID so it can be traced in the server logs. */
export function showError(err: unknown) {
  if (err instanceof BridgeApiError) {
    if (err.status === 401) return; // already redirecting to sign-in
    if (err.code === 'plan_limit_reached') {
      toast.error(err.message, {
        action: { label: 'Upgrade', onClick: () => window.location.assign('/billing') },
      });
      return;
    }
    const field = err.details?.[0];
    toast.error(field?.message ?? err.message, {
      description: err.requestId ? `Request ID ${err.requestId}` : undefined,
    });
    return;
  }
  toast.error('Something went wrong. Try again.');
}

/**
 * Per-field messages from a 422, keyed by location without the "body."
 * prefix: "body.credentials.auth_token" becomes "credentials.auth_token".
 */
export function fieldErrors(err: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (!(err instanceof BridgeApiError) || err.status !== 422) return out;
  for (const d of err.details ?? []) {
    if (!d.location) continue;
    const key = d.location.replace(/^body\./, '');
    out[key] ??= d.message ?? 'Check this value.';
  }
  return out;
}
