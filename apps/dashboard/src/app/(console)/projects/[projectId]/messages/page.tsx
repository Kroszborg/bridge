'use client';

import type { Message } from '@bridge/api-types';
import {
  ArrowDownLeft01Icon,
  ArrowUpRight01Icon,
  Message01Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { type FormEvent, useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { useConsole, useProjectId } from '@/components/layout/console-context';
import { MessageDialog, MessageStatus, viaLabel } from '@/components/messages';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { formatDateTime, formatRelative } from '@/lib/format';
import { type MessageFilters, useDevices, useMessages } from '@/lib/queries';
import { cn } from '@/lib/utils';

const STATUSES: { value: Message['status'] | undefined; label: string }[] = [
  { value: undefined, label: 'All' },
  { value: 'queued', label: 'Queued' },
  { value: 'sending', label: 'Sending' },
  { value: 'sent', label: 'Sent' },
  { value: 'delivered', label: 'Delivered' },
  { value: 'failed', label: 'Failed' },
  { value: 'received', label: 'Received' },
];

const DIRECTIONS: { value: Message['direction'] | undefined; label: string }[] = [
  { value: undefined, label: 'All' },
  { value: 'outbound', label: 'Outgoing' },
  { value: 'inbound', label: 'Incoming' },
];

/** The other party: recipient for outgoing messages, sender for incoming ones. */
function Party({ m }: { m: Message }) {
  const inbound = m.direction === 'inbound';
  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-xs">
      <HugeiconsIcon
        icon={inbound ? ArrowDownLeft01Icon : ArrowUpRight01Icon}
        strokeWidth={2}
        className={cn('size-3.5 shrink-0', inbound ? 'text-primary' : 'text-faint')}
        aria-label={inbound ? 'Incoming' : 'Outgoing'}
      />
      {inbound ? m.from : m.to}
    </span>
  );
}

function Pill({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'rounded-full border px-3 py-1 text-xs/relaxed font-medium transition-colors',
        active
          ? 'border-primary bg-primary text-primary-foreground'
          : 'bg-card text-muted-foreground hover:bg-muted',
      )}
    >
      {children}
    </button>
  );
}

export default function MessagesPage() {
  const projectId = useProjectId() ?? '';
  const { apiUrl } = useConsole();
  const [filters, setFilters] = useState<MessageFilters>({ environment: 'live' });
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState<string | null>(null);
  const messages = useMessages(projectId, filters);
  const devices = useDevices(projectId);
  const deviceName = (id: string | null) => devices.data?.find((d) => d.id === id)?.name;
  const rows = messages.data?.pages.flatMap((p) => p.data) ?? [];

  function applySearch(e: FormEvent) {
    e.preventDefault();
    const value = search.trim() || undefined;
    // Incoming messages are searched by sender, everything else by recipient.
    setFilters((f) =>
      f.direction === 'inbound'
        ? { ...f, from: value, to: undefined }
        : { ...f, to: value, from: undefined },
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Messages"
        subtitle="Every SMS sent and received, which phone handled it, and each step until delivery."
        actions={
          <fieldset className="inline-flex gap-1 rounded-lg border bg-background p-1">
            <legend className="sr-only">Environment</legend>
            {(['live', 'test'] as const).map((env) => (
              <button
                key={env}
                type="button"
                aria-pressed={filters.environment === env}
                onClick={() => setFilters((f) => ({ ...f, environment: env }))}
                className={cn(
                  'rounded-md px-3 py-1 text-xs font-medium capitalize transition-colors',
                  filters.environment === env
                    ? 'bg-muted text-foreground ring-1 ring-border'
                    : 'text-muted-foreground',
                )}
              >
                {env}
              </button>
            ))}
          </fieldset>
        }
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <fieldset className="inline-flex gap-1 rounded-lg border bg-background p-1">
            <legend className="sr-only">Direction</legend>
            {DIRECTIONS.map((d) => (
              <button
                key={d.label}
                type="button"
                aria-pressed={filters.direction === d.value}
                onClick={() => {
                  setSearch('');
                  setFilters((f) => ({
                    ...f,
                    direction: d.value,
                    to: undefined,
                    from: undefined,
                    status:
                      (d.value === 'inbound' && f.status !== 'received') ||
                      (d.value === 'outbound' && f.status === 'received')
                        ? undefined
                        : f.status,
                  }));
                }}
                className={cn(
                  'rounded-md px-3 py-1 text-xs font-medium transition-colors',
                  filters.direction === d.value
                    ? 'bg-muted text-foreground ring-1 ring-border'
                    : 'text-muted-foreground',
                )}
              >
                {d.label}
              </button>
            ))}
          </fieldset>
          {STATUSES.filter(
            (s) =>
              s.value === undefined ||
              (filters.direction === 'inbound'
                ? s.value === 'received'
                : filters.direction === 'outbound'
                  ? s.value !== 'received'
                  : true),
          ).map((s) => (
            <Pill
              key={s.label}
              active={filters.status === s.value}
              onClick={() => setFilters((f) => ({ ...f, status: s.value }))}
            >
              {s.label}
            </Pill>
          ))}
        </div>
        <form onSubmit={applySearch} className="relative w-full lg:w-72">
          <HugeiconsIcon
            icon={Search01Icon}
            strokeWidth={2}
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint"
          />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={
              filters.direction === 'inbound'
                ? 'Sender, e.g. AX-HDFCBK'
                : 'Recipient, e.g. +919876543210'
            }
            aria-label={
              filters.direction === 'inbound' ? 'Filter by sender' : 'Filter by recipient'
            }
            className="pl-8 font-mono"
          />
        </form>
      </div>

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {messages.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : messages.isError ? (
          <EmptyState
            title="Could not load messages"
            description={messages.error.message}
            action={
              <Button variant="outline" onClick={() => messages.refetch()}>
                Try again
              </Button>
            }
          />
        ) : rows.length === 0 ? (
          filters.status || filters.to || filters.from ? (
            <EmptyState title="No messages match these filters" />
          ) : filters.direction === 'inbound' ? (
            <EmptyState
              icon={<HugeiconsIcon icon={ArrowDownLeft01Icon} strokeWidth={1.8} />}
              title="No incoming messages yet"
              description={
                filters.environment === 'test'
                  ? 'Incoming SMS are always live. Switch to Live to see them.'
                  : 'Turn on Forward incoming SMS in a device’s settings. Messages the phone receives then appear here and in message.received webhooks.'
              }
              action={
                <Button asChild variant="outline" size="sm">
                  <Link href={`/projects/${projectId}/devices`}>Open devices</Link>
                </Button>
              }
            />
          ) : (
            <div className="flex flex-col items-center gap-4 px-6 py-10">
              <EmptyState
                className="py-0"
                icon={<HugeiconsIcon icon={Message01Icon} strokeWidth={1.8} />}
                title={`No ${filters.environment} messages yet`}
                description={
                  filters.environment === 'test'
                    ? 'Messages sent with a bk_test_ key appear here. They are simulated and never leave Bridge.'
                    : 'Messages sent with a bk_live_ key go out through your paired phones and appear here.'
                }
              />
              <CodeBlock
                className="w-full max-w-2xl"
                language="shell"
                code={`curl ${apiUrl}/v1/messages \\\n  -H "Authorization: Bearer $BRIDGE_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"to": "+919876543210", "message": "Hello from Bridge"}'`}
              />
              <Button asChild variant="outline" size="sm">
                <Link href={`/projects/${projectId}/api-keys`}>Get an API key</Link>
              </Button>
            </div>
          )
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-5">Number</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Via</TableHead>
                  <TableHead className="text-right">Segments</TableHead>
                  <TableHead className="pr-5 text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((m) => (
                  <TableRow
                    key={m.id}
                    className="cursor-pointer"
                    onClick={() => setOpen(m.id)}
                    onKeyDown={(e) => e.key === 'Enter' && setOpen(m.id)}
                    tabIndex={0}
                  >
                    <TableCell className="pl-5">
                      <Party m={m} />
                    </TableCell>
                    <TableCell className="max-w-[18rem] truncate text-muted-foreground">
                      {m.purpose === 'otp' ? (
                        <span className="mr-1.5 rounded bg-muted px-1.5 py-0.5 text-[0.65rem] font-semibold text-foreground">
                          OTP
                        </span>
                      ) : null}
                      {m.body ?? <span className="italic">Removed after retention</span>}
                    </TableCell>
                    <TableCell>
                      <MessageStatus status={m.status} />
                      {m.status === 'failed' && m.error_code ? (
                        <span className="ml-2 font-mono text-[0.68rem] text-muted-foreground">
                          {m.error_code}
                        </span>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {viaLabel(m.provider, deviceName(m.device_id))}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-muted-foreground">
                      {m.segments ?? '—'}
                    </TableCell>
                    <TableCell
                      className="pr-5 text-right text-muted-foreground"
                      title={formatDateTime(m.created_at)}
                    >
                      {formatRelative(m.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {messages.hasNextPage ? (
              <div className="flex justify-center border-t p-3">
                <Button
                  variant="ghost"
                  onClick={() => messages.fetchNextPage()}
                  disabled={messages.isFetchingNextPage}
                >
                  {messages.isFetchingNextPage ? 'Loading…' : 'Load older messages'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>

      <MessageDialog
        projectId={projectId}
        messageId={open}
        devices={devices.data ?? []}
        onClose={() => setOpen(null)}
      />
    </div>
  );
}
