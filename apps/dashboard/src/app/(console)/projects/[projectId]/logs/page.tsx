'use client';

import type { RequestLog } from '@bridge/api-types';
import { LeftToRightListBulletIcon, Search01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, Fragment, useState } from 'react';
import { CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { StatusBadge } from '@/components/kit/status-badge';
import { useProjectId } from '@/components/layout/console-context';
import { MessageDialog } from '@/components/messages';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
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
import { formatDateTime, formatRelative } from '@/lib/format';
import { type RequestLogFilters, useApiKeys, useDevices, useRequestLogs } from '@/lib/queries';
import { cn } from '@/lib/utils';

const STATUS: { value: RequestLogFilters['status']; label: string }[] = [
  { value: undefined, label: 'All' },
  { value: 'success', label: 'Succeeded' },
  { value: 'error', label: 'Failed' },
];

function statusKind(status: number) {
  if (status >= 500) return 'danger';
  if (status >= 400) return 'warning';
  return 'success';
}

const timeFmt = new Intl.DateTimeFormat('en', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
});

export default function LogsPage() {
  const projectId = useProjectId() ?? '';
  const [filters, setFilters] = useState<RequestLogFilters>({ environment: 'live' });
  const [path, setPath] = useState('');
  const [expanded, setExpanded] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const logs = useRequestLogs(projectId, filters);
  const keys = useApiKeys(projectId);
  const devices = useDevices(projectId);
  const keyName = (id: string | null) => keys.data?.find((k) => k.id === id)?.name;
  const rows = logs.data?.pages.flatMap((p) => p.data) ?? [];
  const envKeys = keys.data?.filter((k) => k.environment === filters.environment) ?? [];

  function applyPath(e: FormEvent) {
    e.preventDefault();
    setFilters((f) => ({ ...f, path: path.trim() || undefined }));
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Logs"
        subtitle="Requests made with this project's API keys. Only metadata is kept, never bodies or query strings, for 14 days by default."
        actions={
          <fieldset className="inline-flex gap-1 rounded-lg border bg-background p-1">
            <legend className="sr-only">Environment</legend>
            {(['live', 'test'] as const).map((env) => (
              <button
                key={env}
                type="button"
                aria-pressed={filters.environment === env}
                onClick={() =>
                  setFilters((f) => ({ ...f, environment: env, api_key_id: undefined }))
                }
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

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
        <div className="flex flex-wrap items-center gap-2">
          {STATUS.map((s) => (
            <button
              key={s.label}
              type="button"
              aria-pressed={filters.status === s.value}
              onClick={() => setFilters((f) => ({ ...f, status: s.value }))}
              className={cn(
                'rounded-full border px-3 py-1 text-xs/relaxed font-medium transition-colors',
                filters.status === s.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'bg-card text-muted-foreground hover:bg-muted',
              )}
            >
              {s.label}
            </button>
          ))}
          <Select
            value={filters.method ?? 'all'}
            onValueChange={(v) =>
              setFilters((f) => ({
                ...f,
                method: v === 'all' ? undefined : (v as RequestLogFilters['method']),
              }))
            }
          >
            <SelectTrigger className="w-32" aria-label="Method">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All methods</SelectItem>
              {(['GET', 'POST', 'PUT', 'PATCH', 'DELETE'] as const).map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={filters.api_key_id ?? 'all'}
            onValueChange={(v) =>
              setFilters((f) => ({ ...f, api_key_id: v === 'all' ? undefined : v }))
            }
          >
            <SelectTrigger className="w-44" aria-label="API key">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All keys</SelectItem>
              {envKeys.map((k) => (
                <SelectItem key={k.id} value={k.id}>
                  {k.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <form onSubmit={applyPath} className="relative w-full lg:ml-auto lg:w-64">
          <HugeiconsIcon
            icon={Search01Icon}
            strokeWidth={2}
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint"
          />
          <Input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="Path, e.g. /v1/messages"
            aria-label="Filter by path"
            className="pl-8 font-mono"
          />
        </form>
      </div>

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {logs.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : logs.isError ? (
          <EmptyState
            title="Could not load logs"
            description={logs.error.message}
            action={
              <Button variant="outline" onClick={() => logs.refetch()}>
                Try again
              </Button>
            }
          />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={LeftToRightListBulletIcon} strokeWidth={1.8} />}
            title={
              filters.status || filters.method || filters.api_key_id || filters.path
                ? 'No requests match these filters'
                : `No ${filters.environment} requests yet`
            }
            description="Requests made with an API key appear here within a second or two. The dashboard's own requests are not listed."
          />
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-5">Time</TableHead>
                  <TableHead>Request</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="hidden md:table-cell">Key</TableHead>
                  <TableHead className="text-right">Duration</TableHead>
                  <TableHead className="hidden pr-5 lg:table-cell">Resource</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((l: RequestLog) => (
                  <Fragment key={l.id}>
                    <TableRow
                      className="cursor-pointer"
                      tabIndex={0}
                      aria-expanded={expanded === l.id}
                      onClick={() => setExpanded(expanded === l.id ? null : l.id)}
                      onKeyDown={(e) =>
                        e.key === 'Enter' && setExpanded(expanded === l.id ? null : l.id)
                      }
                    >
                      <TableCell
                        className="pl-5 tabular-nums text-muted-foreground"
                        title={formatDateTime(l.created_at)}
                      >
                        {timeFmt.format(new Date(l.created_at))}
                      </TableCell>
                      <TableCell className="max-w-[22rem] truncate font-mono text-xs">
                        <span className="mr-2 font-semibold">{l.method}</span>
                        {l.path}
                      </TableCell>
                      <TableCell>
                        <StatusBadge kind={statusKind(l.status)}>{l.status}</StatusBadge>
                        {l.error_code ? (
                          <span className="ml-2 font-mono text-[0.68rem] text-muted-foreground">
                            {l.error_code}
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground md:table-cell">
                        {keyName(l.api_key_id) ?? '—'}
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">
                        {l.duration_ms} ms
                      </TableCell>
                      <TableCell className="hidden pr-5 lg:table-cell">
                        {l.resource_id?.startsWith('msg_') ? (
                          <button
                            type="button"
                            className="font-mono text-xs text-primary hover:underline"
                            onClick={(e) => {
                              e.stopPropagation();
                              setMessage(l.resource_id);
                            }}
                          >
                            {l.resource_id}
                          </button>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                    {expanded === l.id ? (
                      <TableRow className="bg-muted/30 hover:bg-muted/30">
                        <TableCell colSpan={6} className="px-5 py-3 whitespace-normal">
                          <dl className="grid gap-x-6 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-4">
                            <div className="min-w-0">
                              <dt className="text-muted-foreground">Request ID</dt>
                              <dd>
                                <CopyField value={l.request_id} />
                              </dd>
                            </div>
                            <div>
                              <dt className="text-muted-foreground">Time</dt>
                              <dd>
                                {formatDateTime(l.created_at)} · {formatRelative(l.created_at)}
                              </dd>
                            </div>
                            <div>
                              <dt className="text-muted-foreground">Client IP</dt>
                              <dd className="font-mono">{l.ip ?? '—'}</dd>
                            </div>
                            <div className="min-w-0">
                              <dt className="text-muted-foreground">User agent</dt>
                              <dd className="truncate font-mono" title={l.user_agent ?? undefined}>
                                {l.user_agent ?? '—'}
                              </dd>
                            </div>
                          </dl>
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
            {logs.hasNextPage ? (
              <div className="flex justify-center border-t p-3">
                <Button
                  variant="ghost"
                  onClick={() => logs.fetchNextPage()}
                  disabled={logs.isFetchingNextPage}
                >
                  {logs.isFetchingNextPage ? 'Loading…' : 'Load older requests'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>

      <MessageDialog
        projectId={projectId}
        messageId={message}
        devices={devices.data ?? []}
        onClose={() => setMessage(null)}
      />
    </div>
  );
}
