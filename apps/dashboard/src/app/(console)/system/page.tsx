'use client';

import Link from 'next/link';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { StatusBadge } from '@/components/kit/status-badge';
import { useConsole } from '@/components/layout/console-context';
import { ComponentRow, OverallBanner, StatusLegend } from '@/components/status';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { formatRelative } from '@/lib/format';
import { useSystemHealth } from '@/lib/queries';

const number = new Intl.NumberFormat('en');

function bytes(n: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

function duration(seconds: number) {
  if (seconds < 1) return '—';
  if (seconds < 120) return `${Math.round(seconds)} s`;
  if (seconds < 7200) return `${Math.round(seconds / 60)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
}

/** A retention window in the unit people set it in: "30 days", or hours below a day. */
function period(seconds: number) {
  const hours = Math.round(seconds / 3600);
  if (hours % 24 === 0) return `${hours / 24} day${hours === 24 ? '' : 's'}`;
  return `${hours} h`;
}

function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-xl border bg-card p-4">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-display text-2xl font-semibold tracking-tight">{value}</span>
      {note ? <span className="truncate text-xs text-muted-foreground">{note}</span> : null}
    </div>
  );
}

export default function SystemPage() {
  const { user } = useConsole();
  const health = useSystemHealth();

  if (!user.operator) {
    return (
      <EmptyState
        title="System health is for operators"
        description="It shows internals of this Bridge installation. Operators are set with BRIDGE_OPERATOR_EMAILS (by default, the first account)."
        action={
          <Button asChild variant="outline">
            <Link href="/status">Open the public status page</Link>
          </Button>
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="System health"
        subtitle="Processes, job queues and resources of this Bridge installation. Refreshes every 10 seconds."
        actions={
          <Button asChild variant="outline">
            <a href="/status" target="_blank" rel="noreferrer">
              Public status page
            </a>
          </Button>
        }
      />
      {health.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-16" />
          <Skeleton className="h-64" />
        </div>
      ) : health.isError ? (
        <EmptyState title="Could not load system health" description={health.error.message} />
      ) : (
        <>
          <OverallBanner state={health.data.status} />
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <Stat
              label="Phones online"
              value={`${health.data.phones.online} / ${health.data.phones.total}`}
              note="Across every project"
            />
            <Stat
              label="Messages waiting for a phone"
              value={number.format(health.data.backlog.waiting)}
              note={
                health.data.backlog.waiting
                  ? `Oldest ${duration(health.data.backlog.oldest_seconds)}`
                  : 'Nothing waiting'
              }
            />
            <Stat
              label="Database"
              value={bytes(health.data.database.size_bytes)}
              note={`PostgreSQL ${health.data.database.version} · ${health.data.database.connections} connections`}
            />
            <Stat
              label="Version"
              value={health.data.version}
              note={`Bodies kept ${period(health.data.retention.message_bodies_seconds)} · logs ${period(health.data.retention.request_logs_seconds)}`}
            />
          </div>

          <SectionCard
            title="Processes"
            description="API servers and workers that checked in during the last hour."
            contentClassName="p-0"
          >
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-5">Process</TableHead>
                  <TableHead>Host</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead>Started</TableHead>
                  <TableHead className="pr-5 text-right">Last check-in</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {health.data.instances.map((i) => (
                  <TableRow key={i.id}>
                    <TableCell className="pl-5">
                      <StatusBadge kind={i.alive ? 'success' : 'danger'}>
                        {i.kind === 'api' ? 'API server' : 'Worker'}
                      </StatusBadge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{i.hostname}</TableCell>
                    <TableCell className="font-mono text-xs">{i.version}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelative(i.started_at)}
                    </TableCell>
                    <TableCell className="pr-5 text-right text-muted-foreground">
                      {formatRelative(i.seen_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </SectionCard>

          <SectionCard
            title="Job queues"
            description="River jobs by state. Available jobs waiting long means workers are behind."
            contentClassName="p-0"
          >
            {health.data.queues.length === 0 ? (
              <p className="px-5 py-4 text-sm text-muted-foreground">No pending jobs.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-5">Queue</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead className="text-right">Jobs</TableHead>
                    <TableHead className="pr-5 text-right">Oldest waiting</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {health.data.queues.map((q) => (
                    <TableRow key={`${q.queue}-${q.state}`} className="tabular-nums">
                      <TableCell className="pl-5 font-mono text-xs">{q.queue}</TableCell>
                      <TableCell className="capitalize">{q.state}</TableCell>
                      <TableCell className="text-right">{number.format(q.jobs)}</TableCell>
                      <TableCell className="pr-5 text-right">
                        {q.state === 'available' ? duration(q.oldest_available_seconds) : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </SectionCard>

          <section className="overflow-hidden rounded-xl border bg-card">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3.5">
              <h2 className="font-display text-sm font-semibold">Components, last 90 days</h2>
              <StatusLegend />
            </div>
            <ul className="divide-y">
              {health.data.components.map((c) => (
                <ComponentRow key={c.id} c={c} />
              ))}
            </ul>
          </section>
        </>
      )}
    </div>
  );
}
