'use client';

import type { UsageDay } from '@bridge/api-types';
import { useState } from 'react';
import { DailyLine, Legend, type Series, StackedColumns } from '@/components/charts';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { Segmented } from '@/components/kit/segmented';
import { StatTile } from '@/components/kit/stat-tile';
import { useProjectId } from '@/components/layout/console-context';
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
import { useBrowserTimeZone, useUsageHistory } from '@/lib/queries';
import { cn } from '@/lib/utils';

const number = new Intl.NumberFormat('en');
const pct = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 });

const outgoing: Series<'delivered' | 'sent' | 'failed'>[] = [
  { key: 'delivered', label: 'Delivered', color: 'var(--viz-1)' },
  { key: 'sent', label: 'Sent, no delivery report', color: 'var(--viz-2)' },
  { key: 'failed', label: 'Failed', color: 'var(--viz-3)' },
];
const incoming: Series<'inbound'>[] = [
  { key: 'inbound', label: 'Incoming', color: 'var(--viz-1)' },
];
const requests: Series<'ok' | 'errors'>[] = [
  { key: 'ok', label: 'Successful', color: 'var(--viz-1)' },
  { key: 'errors', label: 'Errors (4xx and 5xx)', color: 'var(--viz-3)' },
];

function totals(days: UsageDay[]) {
  const t = {
    outbound: 0,
    delivered: 0,
    sent: 0,
    failed: 0,
    pending: 0,
    inbound: 0,
    segments: 0,
    requests: 0,
    errors: 0,
  };
  for (const d of days) {
    t.outbound += d.outbound;
    t.delivered += d.delivered;
    t.sent += d.sent;
    t.failed += d.failed;
    t.pending += d.pending;
    t.inbound += d.inbound;
    t.segments += d.segments;
    t.requests += d.requests;
    t.errors += d.client_errors + d.server_errors;
  }
  return t;
}

export default function UsagePage() {
  const projectId = useProjectId() ?? '';
  const [environment, setEnvironment] = useState<'live' | 'test'>('live');
  const [days, setDays] = useState(30);
  const [asTable, setAsTable] = useState(false);
  const history = useUsageHistory(projectId, environment, days);
  const tz = useBrowserTimeZone();

  const data = (history.data?.days ?? []).map((d) => ({
    ...d,
    ok: d.requests - d.client_errors - d.server_errors,
    errors: d.client_errors + d.server_errors,
    // No requests that day: leave a gap rather than plotting a misleading 0 ms.
    p95: d.requests > 0 ? Math.round(d.p95_latency_ms) : (null as unknown as number),
  }));
  const t = totals(history.data?.days ?? []);
  const finished = t.delivered + t.sent + t.failed;
  const devices = history.data?.devices ?? [];
  const traffic = Math.max(
    1,
    devices.reduce((n, d) => n + d.outbound + d.inbound, 0),
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Usage"
        subtitle={`Daily volume, delivery and API health. Days follow your time zone${tz ? ` (${tz})` : ''}.`}
      />

      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          label="Environment"
          value={environment}
          onChange={setEnvironment}
          options={[
            { value: 'live', label: 'Live' },
            { value: 'test', label: 'Test' },
          ]}
        />
        <Segmented
          label="Range"
          value={days}
          onChange={setDays}
          options={[
            { value: 7, label: 'Last 7 days' },
            { value: 30, label: 'Last 30 days' },
            { value: 90, label: 'Last 90 days' },
          ]}
        />
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto"
          onClick={() => setAsTable((v) => !v)}
          aria-pressed={asTable}
        >
          {asTable ? 'Show charts' : 'Show as table'}
        </Button>
      </div>

      {history.isPending ? (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
          <Skeleton className="h-72" />
        </div>
      ) : history.isError ? (
        <EmptyState
          title="Could not load usage"
          description={history.error.message}
          action={
            <Button variant="outline" onClick={() => history.refetch()}>
              Try again
            </Button>
          }
        />
      ) : (
        <div
          className={cn(
            'flex flex-col gap-6 transition-opacity',
            history.isPlaceholderData && 'opacity-60',
          )}
        >
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatTile
              label="Messages sent"
              value={number.format(t.outbound)}
              note={`${number.format(t.segments)} segments`}
            />
            <StatTile
              label="Delivery rate"
              value={finished > 0 ? pct.format((t.delivered + t.sent) / finished) : '—'}
              note={
                finished > 0
                  ? `${number.format(t.delivered)} confirmed by the carrier`
                  : 'No finished messages yet'
              }
            />
            <StatTile
              label="Failed"
              value={number.format(t.failed)}
              note={t.pending > 0 ? `${number.format(t.pending)} still in progress` : undefined}
            />
            <StatTile
              label="Incoming"
              value={number.format(t.inbound)}
              note={
                environment === 'test'
                  ? 'Incoming SMS are always live'
                  : 'From phones with forwarding on'
              }
            />
          </div>

          {asTable ? (
            <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-5">Date</TableHead>
                    <TableHead className="text-right">Sent</TableHead>
                    <TableHead className="text-right">Delivered</TableHead>
                    <TableHead className="text-right">No report</TableHead>
                    <TableHead className="text-right">Failed</TableHead>
                    <TableHead className="text-right">Incoming</TableHead>
                    <TableHead className="text-right">Segments</TableHead>
                    <TableHead className="text-right">Requests</TableHead>
                    <TableHead className="text-right">Errors</TableHead>
                    <TableHead className="pr-5 text-right">p95</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {[...data].reverse().map((d) => (
                    <TableRow key={d.date} className="tabular-nums">
                      <TableCell className="pl-5">{d.date}</TableCell>
                      <TableCell className="text-right">{number.format(d.outbound)}</TableCell>
                      <TableCell className="text-right">{number.format(d.delivered)}</TableCell>
                      <TableCell className="text-right">{number.format(d.sent)}</TableCell>
                      <TableCell className="text-right">{number.format(d.failed)}</TableCell>
                      <TableCell className="text-right">{number.format(d.inbound)}</TableCell>
                      <TableCell className="text-right">{number.format(d.segments)}</TableCell>
                      <TableCell className="text-right">{number.format(d.requests)}</TableCell>
                      <TableCell className="text-right">{number.format(d.errors)}</TableCell>
                      <TableCell className="pr-5 text-right">
                        {d.requests ? `${Math.round(d.p95_latency_ms)} ms` : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <>
              <SectionCard
                title="Outgoing messages"
                description="Per day, by final status. Messages still in progress are left out."
                contentClassName="flex flex-col gap-3"
              >
                <Legend series={outgoing} />
                <StackedColumns
                  data={data}
                  series={outgoing}
                  height={240}
                  label={`Outgoing messages per day for the last ${days} days`}
                  footer={(row) => `${number.format(Number(row.segments))} segments`}
                />
              </SectionCard>

              <div className="grid gap-6 lg:grid-cols-2">
                <SectionCard
                  title="Incoming messages"
                  description="Received by phones with forwarding on."
                >
                  <StackedColumns
                    data={data}
                    series={incoming}
                    height={180}
                    label="Incoming messages per day"
                  />
                </SectionCard>
                <SectionCard
                  title="API requests"
                  description="From the request log, kept 14 days by default."
                  contentClassName="flex flex-col gap-3"
                >
                  <Legend series={requests} />
                  <StackedColumns
                    data={data}
                    series={requests}
                    height={180}
                    label="API requests per day"
                    footer={(row) =>
                      `${number.format(Number(row.client_errors))} client · ${number.format(Number(row.server_errors))} server errors`
                    }
                  />
                </SectionCard>
              </div>

              <SectionCard
                title="API response time"
                description="95th percentile per day. Days without requests are left blank."
              >
                <DailyLine
                  data={data}
                  dataKey="p95"
                  name="p95"
                  height={160}
                  format={(v) => `${number.format(v)} ms`}
                  label="95th percentile API response time per day"
                />
              </SectionCard>
            </>
          )}

          <SectionCard
            title="Phones"
            description="Messages each phone handled in this period, busiest first."
            contentClassName="p-0"
          >
            {devices.length === 0 ? (
              <p className="px-5 py-6 text-sm text-muted-foreground">
                No phone handled messages in this period.
              </p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-5">Phone</TableHead>
                    <TableHead className="text-right">Sent</TableHead>
                    <TableHead className="text-right">Delivered</TableHead>
                    <TableHead className="text-right">Failed</TableHead>
                    <TableHead className="text-right">Incoming</TableHead>
                    <TableHead className="pr-5">Share of traffic</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {devices.map((d) => {
                    const share = (d.outbound + d.inbound) / traffic;
                    return (
                      <TableRow key={d.device_id} className="tabular-nums">
                        <TableCell className="pl-5 font-medium">
                          {d.name || (
                            <span className="font-mono text-xs text-muted-foreground">
                              {d.device_id}
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="text-right">{number.format(d.outbound)}</TableCell>
                        <TableCell className="text-right">{number.format(d.delivered)}</TableCell>
                        <TableCell className="text-right">{number.format(d.failed)}</TableCell>
                        <TableCell className="text-right">{number.format(d.inbound)}</TableCell>
                        <TableCell className="w-56 pr-5">
                          <div className="flex items-center gap-3">
                            <div
                              className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted"
                              aria-hidden
                            >
                              <div
                                className="h-full rounded-full bg-viz-1"
                                style={{ width: `${Math.max(2, share * 100)}%` }}
                              />
                            </div>
                            <span className="w-12 text-right text-xs text-muted-foreground">
                              {pct.format(share)}
                            </span>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </SectionCard>
        </div>
      )}
    </div>
  );
}
