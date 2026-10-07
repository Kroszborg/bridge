'use client';

import type { Broadcast, BroadcastStatus } from '@bridge/api-types';
import { SentIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useState } from 'react';
import { toast } from 'sonner';
import { CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { Segmented } from '@/components/kit/segmented';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan } from '@/components/layout/console-context';
import { Fact } from '@/components/messaging';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  type Environment,
  useBroadcast,
  useBroadcasts,
  useCancelBroadcast,
  useDevices,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

const number = new Intl.NumberFormat('en');

const STATUS: Record<BroadcastStatus, { kind: StatusKind; label: string }> = {
  scheduled: { kind: 'neutral', label: 'Scheduled' },
  sending: { kind: 'info', label: 'Sending' },
  completed: { kind: 'success', label: 'Completed' },
  canceled: { kind: 'neutral', label: 'Canceled' },
};

export function BroadcastStatusBadge({ status }: { status: BroadcastStatus }) {
  const s = STATUS[status];
  return <StatusBadge kind={s.kind}>{s.label}</StatusBadge>;
}

/** The broadcast's name, or the start of its template. */
function title(b: Broadcast): string {
  if (b.name) return b.name;
  const t = b.template.replace(/\s+/g, ' ').trim();
  return t.length > 60 ? `${t.slice(0, 60)}…` : t;
}

const PARTS = [
  { key: 'delivered', label: 'Delivered', className: 'bg-success' },
  { key: 'sent', label: 'Sent', className: 'bg-primary' },
  { key: 'failed', label: 'Failed', className: 'bg-destructive' },
  { key: 'canceled', label: 'Canceled', className: 'bg-faint' },
] as const;

/** Delivered, sent, failed and canceled as one stacked bar; the rest is still queued. */
export function BroadcastProgress({ b, detailed = false }: { b: Broadcast; detailed?: boolean }) {
  const c = b.counts;
  const total = Math.max(c.recipients, c.queued + c.sent + c.delivered + c.failed + c.canceled, 1);
  const done = c.sent + c.delivered + c.failed + c.canceled;
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div
        className="flex h-1.5 w-full overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={`${number.format(done)} of ${number.format(total)} processed`}
      >
        {PARTS.map((p) =>
          c[p.key] > 0 ? (
            <span
              key={p.key}
              className={cn('h-full', p.className)}
              style={{ width: `${(c[p.key] / total) * 100}%` }}
            />
          ) : null,
        )}
      </div>
      {detailed ? (
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {PARTS.map((p) => (
            <li key={p.key} className="flex items-center gap-1.5">
              <span className={cn('size-2 rounded-full', p.className)} aria-hidden />
              {p.label}{' '}
              <span className="tabular-nums text-foreground">{number.format(c[p.key])}</span>
            </li>
          ))}
          <li className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-muted ring-1 ring-border" aria-hidden />
            Queued <span className="tabular-nums text-foreground">{number.format(c.queued)}</span>
          </li>
        </ul>
      ) : (
        <span className="text-[0.7rem] tabular-nums text-muted-foreground">
          {number.format(done)} / {number.format(total)}
          {c.failed > 0 ? ` · ${number.format(c.failed)} failed` : ''}
        </span>
      )}
    </div>
  );
}

export function BroadcastList({
  projectId,
  environment,
  onEnvironmentChange,
  onOpen,
}: {
  projectId: string;
  environment: Environment;
  onEnvironmentChange: (e: Environment) => void;
  onOpen: (id: string) => void;
}) {
  const [status, setStatus] = useState<BroadcastStatus | 'all'>('all');
  const list = useBroadcasts(projectId, environment, status === 'all' ? undefined : status);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <section className="flex min-w-0 flex-col overflow-hidden rounded-xl border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3.5">
        <div>
          <h2 className="font-display text-sm font-semibold">Broadcasts</h2>
          <p className="mt-0.5 text-xs/relaxed text-muted-foreground">
            Newest first. Open one for its counts or to cancel it.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={status} onValueChange={(v) => setStatus(v as BroadcastStatus | 'all')}>
            <SelectTrigger className="w-36" aria-label="Status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All statuses</SelectItem>
              {(Object.keys(STATUS) as BroadcastStatus[]).map((s) => (
                <SelectItem key={s} value={s}>
                  {STATUS[s].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Segmented
            label="Environment"
            value={environment}
            onChange={onEnvironmentChange}
            options={[
              { value: 'test', label: 'Test' },
              { value: 'live', label: 'Live' },
            ]}
          />
        </div>
      </div>
      {list.isPending ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : list.isError ? (
        <EmptyState
          title="Could not load broadcasts"
          description={list.error.message}
          action={
            <Button variant="outline" onClick={() => list.refetch()}>
              Try again
            </Button>
          }
        />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={SentIcon} strokeWidth={1.8} />}
          title={status === 'all' ? `No ${environment} broadcasts yet` : 'No broadcasts match'}
          description={
            status === 'all'
              ? 'Broadcasts you send from this page or with POST /v1/broadcasts appear here.'
              : undefined
          }
        />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Broadcast</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-48">Progress</TableHead>
                <TableHead className="text-right">Recipients</TableHead>
                <TableHead>Scheduled</TableHead>
                <TableHead className="pr-5 text-right">Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((b) => (
                <TableRow
                  key={b.id}
                  className="cursor-pointer"
                  onClick={() => onOpen(b.id)}
                  onKeyDown={(e) => e.key === 'Enter' && onOpen(b.id)}
                  tabIndex={0}
                >
                  <TableCell className="max-w-[18rem] pl-5">
                    <span className="block truncate font-medium">{title(b)}</span>
                    {b.name ? (
                      <span className="block truncate text-xs text-muted-foreground">
                        {b.template}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    <BroadcastStatusBadge status={b.status} />
                  </TableCell>
                  <TableCell className="w-48">
                    <BroadcastProgress b={b} />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {number.format(b.counts.recipients)}
                  </TableCell>
                  <TableCell
                    className="text-muted-foreground"
                    title={b.scheduled_at ? formatDateTime(b.scheduled_at) : undefined}
                  >
                    {b.scheduled_at ? formatDateTime(b.scheduled_at) : '—'}
                  </TableCell>
                  <TableCell
                    className="pr-5 text-right text-muted-foreground"
                    title={formatDateTime(b.created_at)}
                  >
                    {formatRelative(b.created_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {list.hasNextPage ? (
            <div className="flex justify-center border-t p-3">
              <Button
                variant="ghost"
                onClick={() => list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                {list.isFetchingNextPage ? 'Loading…' : 'Load older broadcasts'}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

export function BroadcastDialog({
  projectId,
  broadcastId,
  onClose,
}: {
  projectId: string;
  broadcastId: string | null;
  onClose: () => void;
}) {
  const { data: b, isPending, isError, error } = useBroadcast(projectId, broadcastId);
  const devices = useDevices(projectId, 60_000);
  const cancel = useCancelBroadcast(projectId);
  const canAdmin = useCan('admin');
  const [confirming, setConfirming] = useState(false);
  const canCancel =
    b &&
    (b.status === 'scheduled' || b.status === 'sending') &&
    (b.environment === 'test' || canAdmin);
  const device = devices.data?.find((d) => d.id === b?.device_id);

  async function confirmCancel() {
    if (!b) return;
    try {
      await cancel.mutateAsync(b.id);
      toast.success('Broadcast canceled');
      setConfirming(false);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <Dialog
      open={broadcastId !== null}
      onOpenChange={(o) => {
        if (o) return;
        setConfirming(false);
        onClose();
      }}
    >
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-3 pr-8">
            <span className="min-w-0 truncate">{b ? title(b) : 'Broadcast'}</span>
            {b ? <BroadcastStatusBadge status={b.status} /> : null}
          </DialogTitle>
          <DialogDescription className="sr-only">Broadcast counts and template</DialogDescription>
        </DialogHeader>
        {isError ? (
          <p className="text-sm text-destructive">{error.message}</p>
        ) : isPending || !b ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-32 w-full" />
          </div>
        ) : (
          <div className="flex min-w-0 flex-col gap-5">
            <CopyField value={b.id} />
            <BroadcastProgress b={b} detailed />
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
              <Fact label="Recipients">{number.format(b.counts.recipients)}</Fact>
              <Fact label="Segments">{number.format(b.total_segments)}</Fact>
              <Fact label="Skipped">{number.format(b.counts.skipped)}</Fact>
              <Fact label="Repeated">{number.format(b.counts.duplicates)}</Fact>
              <Fact label="Environment">{b.environment === 'live' ? 'Live' : 'Test'}</Fact>
              <Fact label="Phone">
                {b.device_id ? (device?.name ?? b.device_id) : 'Bridge picks'}
              </Fact>
              <Fact label="Created">{formatDateTime(b.created_at)}</Fact>
              {b.scheduled_at ? (
                <Fact label="Scheduled">{formatDateTime(b.scheduled_at)}</Fact>
              ) : (
                <Fact label="Started">{formatDateTime(b.started_at)}</Fact>
              )}
              {b.completed_at ? (
                <Fact label="Completed">{formatDateTime(b.completed_at)}</Fact>
              ) : null}
              {b.canceled_at ? <Fact label="Canceled">{formatDateTime(b.canceled_at)}</Fact> : null}
            </dl>
            <p className="text-xs/relaxed text-muted-foreground">
              Skipped counts numbers that opted out and rows the pipeline refused. Repeated numbers
              were sent once.
              {b.status === 'completed'
                ? ' Every message was sent or failed; delivery reports may still arrive.'
                : ''}
            </p>

            <div className="flex flex-col gap-2">
              <h3 className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
                Template
              </h3>
              <p className="whitespace-pre-wrap break-words rounded-lg border bg-background px-3 py-2 text-sm">
                {b.template}
              </p>
            </div>

            <p className="text-xs/relaxed text-muted-foreground">
              The messages are listed under{' '}
              <Link
                href={`/projects/${projectId}/messages`}
                className="font-medium text-primary hover:underline"
              >
                Messages
              </Link>{' '}
              in {b.environment === 'live' ? 'Live' : 'Test'}. Each one carries{' '}
              <code className="font-mono">metadata.broadcast_id</code>, so webhook events can be
              matched to this broadcast.
            </p>
          </div>
        )}
        {b && canCancel ? (
          <DialogFooter className="border-t pt-4">
            {confirming ? (
              <>
                <span className="mr-auto self-center text-xs text-muted-foreground">
                  Messages not yet sent are dropped.
                </span>
                <Button variant="ghost" onClick={() => setConfirming(false)}>
                  Keep sending
                </Button>
                <Button variant="destructive" onClick={confirmCancel} disabled={cancel.isPending}>
                  {cancel.isPending ? 'Canceling…' : 'Cancel broadcast'}
                </Button>
              </>
            ) : (
              <Button variant="destructive" onClick={() => setConfirming(true)}>
                Cancel broadcast
              </Button>
            )}
          </DialogFooter>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
