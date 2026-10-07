import { toast } from 'sonner';
import { BridgeApiError } from './api';

/** Shows an API error as a toast, with the request ID so it can be traced in the server logs. */
export function showError(err: unknown) {
  if (err instanceof BridgeApiError) {
    if (err.status === 401) return; // already redirecting to sign-in
    const field = err.details?.[0];
    toast.error(field?.message ?? err.message, {
      description: err.requestId ? `Request ID ${err.requestId}` : undefined,
    });
    return;
  }
  toast.error('Something went wrong. Try again.');
}
