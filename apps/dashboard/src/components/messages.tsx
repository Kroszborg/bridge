'use client';

import type { Device, Message, MessageEvent } from '@bridge/api-types';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyField } from '@/components/kit/copy-button';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDateTime } from '@/lib/format';
import { useMessage } from '@/lib/queries';
import { cn } from '@/lib/utils';

const STATUS: Record<Message['status'], { kind: StatusKind; label: string; live?: boolean }> = {
  created: { kind: 'neutral', label: 'Created' },
  queued: { kind: 'neutral', label: 'Queued' },
  sending: { kind: 'info', label: 'Sending', live: true },
  sent: { kind: 'info', label: 'Sent' },
  delivered: { kind: 'success', label: 'Delivered' },
  failed: { kind: 'danger', label: 'Failed' },
  received: { kind: 'info', label: 'Received' },
};

export function MessageStatus({ status }: { status: Message['status'] }) {
  const s = STATUS[status];
  return (
    <StatusBadge kind={s.kind} live={s.live}>
      {s.label}
    </StatusBadge>
  );
}

/** Display names for SMS providers, keyed by the API's provider value. */
export const PROVIDER_NAMES: Record<string, string> = {
  msg91: 'MSG91',
  twilio: 'Twilio',
  vonage: 'Vonage',
  plivo: 'Plivo',
};

/**
 * What sent or received a message: an SMS provider, the phone's name, or the
 * simulator. `fallback` means the message is queued for a provider.
 */
export function viaLabel(provider: Message['provider'], deviceName: string | undefined): string {
  const name = PROVIDER_NAMES[provider];
  if (name) return name;
  if (provider === 'fallback') return 'Waiting for a provider';
  if (deviceName) return deviceName;
  if (provider === 'simulator') return 'Simulator';
  return '—';
}

const FALLBACK_REASONS: Record<string, string> = {
  no_paired_phone: 'No phone paired; handed to a provider',
  no_phone_available: 'No phone available; handed to a provider',
  no_phone_in_time: 'No phone took it in time; handed to a provider',
  providers_only: 'Routed to a provider',
  phone_failed: 'Phone could not send; handed to a provider',
  phone_unresponsive: 'Phone did not respond; handed to a provider',
};

function eventLabel(e: MessageEvent): string {
  const d = e.detail as Record<string, unknown>;
  switch (e.type) {
    case 'provider_fallback':
      return FALLBACK_REASONS[String(d.reason)] ?? 'Handed to a provider';
    case 'provider_accepted':
      return `Accepted by ${PROVIDER_NAMES[String(d.provider)] ?? 'the provider'}`;
    case 'created':
      return 'Created';
    case 'received':
      return `Received by ${String(d.device_name ?? 'a phone')}`;
    case 'queued':
      return 'Queued';
    case 'assigned':
      return `Assigned to ${String(d.device_name ?? 'a phone')}${Number(d.attempt) > 1 ? ` (attempt ${d.attempt})` : ''}`;
    case 'device_accepted':
      return d.simulated ? 'Accepted by the simulator' : 'Phone accepted';
    case 'sent':
      return d.segments
        ? `Sent · ${d.segments} segment${Number(d.segments) > 1 ? 's' : ''}`
        : 'Sent';
    case 'delivered':
      return 'Delivered';
    case 'delivery_failed':
      return 'Carrier reported it undelivered';
    case 'failed':
      return 'Failed';
    case 'send_failed_retrying':
      return 'Phone could not send; retrying';
    case 'assignment_timed_out':
      return 'Phone did not respond; reassigning';
    default:
      return e.type.replaceAll('_', ' ');
  }
}

function eventTone(e: MessageEvent): string {
  if (e.to_status === 'failed') return 'bg-destructive';
  if (e.to_status === 'delivered') return 'bg-success';
  if (e.type === 'send_failed_retrying' || e.type === 'assignment_timed_out') return 'bg-warning';
  if (e.type === 'provider_fallback') {
    const reason = (e.detail as Record<string, unknown>).reason;
    if (reason === 'phone_failed' || reason === 'phone_unresponsive') return 'bg-warning';
  }
  return 'bg-primary';
}

const timeFmt = new Intl.DateTimeFormat('en', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  fractionalSecondDigits: 3,
  hour12: false,
});

export function MessageTimeline({ events }: { events: MessageEvent[] }) {
  return (
    <ol className="flex flex-col">
      {events.map((e, i) => {
        const d = e.detail as Record<string, unknown>;
        const note =
          (d.error_message as string | undefined) ??
          (e.type === 'assigned' && typeof d.device_id === 'string' ? d.device_id : undefined);
        return (
          <li
            key={`${e.created_at}-${e.type}`}
            className="relative grid grid-cols-[6.5rem_1rem_1fr] items-start gap-3 pb-3 last:pb-0"
          >
            <time
              className="pt-px font-mono text-[0.7rem] tabular-nums text-faint"
              dateTime={e.created_at}
            >
              {timeFmt.format(new Date(e.created_at))}
            </time>
            <span className="relative flex justify-center pt-1">
              <span className={cn('z-10 size-2.5 rounded-full', eventTone(e))} />
              {i < events.length - 1 ? (
                <span className="absolute top-3.5 -bottom-3 w-px bg-border" aria-hidden />
              ) : null}
            </span>
            <span className="min-w-0">
              <span className="block text-sm font-medium">{eventLabel(e)}</span>
              {note ? (
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {note}
                </span>
              ) : null}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
        {label}
      </dt>
      <dd className="truncate text-sm">{children}</dd>
    </div>
  );
}

export function MessageDialog({
  projectId,
  messageId,
  devices,
  onClose,
}: {
  projectId: string;
  messageId: string | null;
  devices: Device[];
  onClose: () => void;
}) {
  const { data: m, isPending } = useMessage(projectId, messageId);
  const device = devices.find((d) => d.id === m?.device_id);
  const viaProvider = m ? m.provider === 'fallback' || m.provider in PROVIDER_NAMES : false;

  return (
    <Dialog open={messageId !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-3">
            Message {m ? <MessageStatus status={m.status} /> : null}
          </DialogTitle>
          <DialogDescription className="sr-only">
            Message details and delivery timeline
          </DialogDescription>
        </DialogHeader>
        {isPending || !m ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-32 w-full" />
          </div>
        ) : (
          <div className="flex min-w-0 flex-col gap-5">
            <CopyField value={m.id} />
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
              {m.direction === 'inbound' ? (
                <Fact label="From">
                  <span className="font-mono">{m.from}</span>
                </Fact>
              ) : (
                <Fact label="To">
                  <span className="font-mono">{m.to}</span>
                </Fact>
              )}
              <Fact label="Via">{viaLabel(m.provider, device?.name)}</Fact>
              <Fact label="SIM">
                {m.sim_slot
                  ? `SIM ${m.sim_slot}`
                  : m.direction === 'inbound' || viaProvider
                    ? '—'
                    : 'Default'}
              </Fact>
              <Fact label="Segments">
                {m.segments ?? '—'}{' '}
                {m.encoding ? `· ${m.encoding === 'gsm7' ? 'GSM-7' : 'Unicode'}` : ''}
              </Fact>
              <Fact label="Environment">{m.environment}</Fact>
              {m.direction === 'inbound' ? (
                <Fact label="Direction">Incoming</Fact>
              ) : (
                <Fact label="Attempts">{m.attempts}</Fact>
              )}
              <Fact label="Created">{formatDateTime(m.created_at)}</Fact>
            </dl>

            {m.status === 'failed' ? (
              <div className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
                <p className="font-mono font-semibold text-destructive">{m.error_code}</p>
                <p className="mt-1 text-muted-foreground">{m.error_message}</p>
              </div>
            ) : null}

            <div className="flex flex-col gap-2">
              <h3 className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
                Body
              </h3>
              {m.body == null ? (
                <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
                  The text was removed after the retention period. Status and timeline are kept.
                </p>
              ) : (
                <p className="whitespace-pre-wrap break-words rounded-lg border bg-background px-3 py-2 text-sm">
                  {m.body}
                </p>
              )}
              {m.purpose === 'otp' ? (
                <p className="text-xs text-muted-foreground">
                  A one-time password from Verify. The code is never shown, and the stored text is
                  erased once the code is used or the SMS has left the phone.
                </p>
              ) : null}
            </div>

            <div className="flex flex-col gap-3">
              <h3 className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
                Timeline
              </h3>
              <MessageTimeline events={m.events} />
              {m.status === 'sent' ? (
                <p className="text-xs text-muted-foreground">
                  {m.provider === 'msg91'
                    ? "Waiting for MSG91's delivery report. MSG91 sends it only if its delivery-report webhook is set to the callback URL shown on the Providers page."
                    : viaProvider
                      ? `Waiting for ${viaLabel(m.provider, undefined)}'s delivery report. Some carriers never send one.`
                      : "Waiting for the carrier's delivery report. Some carriers never send one."}
                </p>
              ) : null}
              {m.status === 'sending' && !viaProvider ? (
                <p className="text-xs text-muted-foreground">
                  The phone is handing the message to Android. If Android asks for permission to
                  send many messages, approve it on the phone.
                </p>
              ) : null}
            </div>

            {Object.keys(m.metadata).length > 0 ? (
              <CodeBlock language="metadata" code={JSON.stringify(m.metadata, null, 2)} />
            ) : null}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
