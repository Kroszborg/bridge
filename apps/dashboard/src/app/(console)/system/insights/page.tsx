'use client';

import type { InsightsBilling, SystemInsights } from '@bridge/api-types';
import { RefreshIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useState } from 'react';
import { Legend, type Series, StackedColumns } from '@/components/charts';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { Segmented } from '@/components/kit/segmented';
import { StatTile } from '@/components/kit/stat-tile';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
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
import { formatDateTime, formatRelative } from '@/lib/format';
import { useSystemInsights } from '@/lib/queries';
import { cn } from '@/lib/utils';

const number = new Intl.NumberFormat('en');
const pct = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 });
const clock = new Intl.DateTimeFormat('en', { hour: '2-digit', minute: '2-digit' });

const liveSeries: Series<'delivered' | 'sent' | 'failed'>[] = [
  { key: 'delivered', label: 'Delivered', color: 'var(--viz-1)' },
  { key: 'sent', label: 'Sent, no delivery report', color: 'var(--viz-2)' },
  { key: 'failed', label: 'Failed', color: 'var(--viz-3)' },
];
const testSeries: Series<'test'>[] = [
  { key: 'test', label: 'Test messages', color: 'var(--viz-2)' },
];
const signupSeries: Series<'users'>[] = [
  { key: 'users', label: 'Accounts', color: 'var(--viz-1)' },
];

const ranges = [
  { value: 7, label: '7d' },
  { value: 30, label: '30d' },
  { value: 90, label: '90d' },
];

function money(cents: number, currency: string) {
  return new Intl.NumberFormat('en', { style: 'currency', currency }).format(cents / 100);
}

/** A share of a total as a thin bar and a percentage, as on the Usage page. */
function ShareBar({ share }: { share: number }) {
  return (
    <div className="flex items-center gap-3">
      <div className="h-1.5 min-w-16 flex-1 overflow-hidden rounded-full bg-muted" aria-hidden>
        <div
          className="h-full rounded-full bg-viz-1"
          style={{ width: `${share > 0 ? Math.max(2, share * 100) : 0}%` }}
        />
      </div>
      <span className="w-12 text-right text-xs text-muted-foreground">{pct.format(share)}</span>
    </div>
  );
}

/** A table body row spanning every column, for "nothing here yet". */
function NoRows({ colSpan, children }: { colSpan: number; children: React.ReactNode }) {
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell colSpan={colSpan} className="px-5 py-6 text-center text-sm text-muted-foreground">
        {children}
      </TableCell>
    </TableRow>
  );
}

function ChartEmpty({ children }: { children: React.ReactNode }) {
  return (
    <p className="grid h-40 place-items-center text-center text-sm text-muted-foreground">
      {children}
    </p>
  );
}

const subscriptionKinds: Record<string, StatusKind> = {
  active: 'success',
  past_due: 'warning',
  incomplete: 'warning',
  cancelled: 'neutral',
  expired: 'neutral',
};

export default function InsightsPage() {
  const [days, setDays] = useState(30);
  const insights = useSystemInsights(days);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Insights"
        subtitle="How this Bridge installation is used, from its own database. Days are UTC; refreshes every minute."
        actions={
          <>
            {insights.data ? (
              <span className="text-xs text-muted-foreground" aria-live="polite">
                Updated {clock.format(new Date(insights.dataUpdatedAt))}
              </span>
            ) : null}
            <Button
              variant="outline"
              size="icon"
              aria-label="Refresh"
              title="Refresh"
              disabled={insights.isFetching}
              onClick={() => void insights.refetch()}
            >
              <HugeiconsIcon
                icon={RefreshIcon}
                strokeWidth={2}
                className={cn(insights.isFetching && 'animate-spin')}
              />
            </Button>
            <Segmented label="Range" value={days} onChange={setDays} options={ranges} />
          </>
        }
      />

      {insights.isPending ? (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-6">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
          <Skeleton className="h-72" />
          <Skeleton className="h-64" />
        </div>
      ) : insights.isError ? (
        <EmptyState
          title="Could not load insights"
          description={insights.error.message}
          action={
            <Button variant="outline" onClick={() => insights.refetch()}>
              Try again
            </Button>
          }
        />
      ) : (
        <div
          className={cn(
            'flex flex-col gap-6 transition-opacity',
            insights.isPlaceholderData && 'opacity-60',
          )}
        >
          <Insights data={insights.data} />
        </div>
      )}
    </div>
  );
}

function Insights({ data }: { data: SystemInsights }) {
  const { totals, messages, verify, billing, activity } = data;
  const days = data.range.days;
  const live = messages.totals;
  const signupsInRange = data.signups.reduce((n, d) => n + d.users, 0);
  const anyLive = messages.daily.some((d) => d.delivered + d.sent + d.failed > 0);
  const anyTest = messages.daily.some((d) => d.test > 0);
  const failedTotal = messages.top_errors.reduce((n, e) => n + e.count, 0);
  const providerTotal = messages.by_provider.reduce((n, p) => n + p.count, 0);
  const topTotal = Math.max(
    1,
    data.top_organizations.reduce((n, o) => n + o.messages, 0),
  );

  return (
    <>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-6">
        <StatTile
          label="Users"
          value={number.format(totals.users)}
          note={`+${number.format(totals.users_in_range)} in ${days} days · ${number.format(totals.users_email_verified)} verified`}
        />
        <StatTile
          label="Workspaces"
          value={number.format(totals.organizations)}
          note={`${number.format(activity.active_organizations_7d)} sent this week · ${number.format(totals.projects)} projects`}
        />
        <StatTile
          label="Phones online"
          value={`${number.format(totals.phones.online)} / ${number.format(totals.phones.total)}`}
          note={`${number.format(totals.api_keys)} active API keys`}
        />
        <StatTile
          label={`Live SMS, ${days} days`}
          value={number.format(live.live)}
          note={
            messages.delivery_rate != null
              ? `${pct.format(messages.delivery_rate)} delivered of finished`
              : 'No delivered or failed yet'
          }
        />
        <StatTile
          label="Verify rate"
          value={verify.verify_rate != null ? pct.format(verify.verify_rate) : '—'}
          note={`${number.format(verify.verified)} of ${number.format(verify.started)} codes`}
        />
        {billing ? (
          <StatTile
            label="MRR (estimate)"
            value={money(billing.mrr_cents, billing.currency)}
            note={`${number.format(totals.subscriptions_active)} paid subscriptions`}
          />
        ) : (
          <StatTile
            label="Active users, 7 days"
            value={number.format(activity.active_users_7d)}
            note="Used the dashboard this week"
          />
        )}
      </div>

      <SectionCard
        title="Live messages"
        description={`Outgoing live SMS per day by status. ${number.format(live.pending)} still in progress · ${number.format(live.inbound)} received.`}
        contentClassName="flex flex-col gap-3"
      >
        {anyLive ? (
          <>
            <Legend series={liveSeries} />
            <StackedColumns
              data={messages.daily}
              series={liveSeries}
              height={240}
              label={`Live messages per day for the last ${days} days, by status`}
            />
          </>
        ) : (
          <ChartEmpty>No live messages in this period.</ChartEmpty>
        )}
      </SectionCard>

      <div className="grid gap-6 lg:grid-cols-2">
        <SectionCard
          title="Sign-ups"
          description={`${number.format(signupsInRange)} new accounts and ${number.format(totals.organizations_in_range)} new workspaces.`}
        >
          {signupsInRange > 0 ? (
            <StackedColumns
              data={data.signups}
              series={signupSeries}
              height={180}
              label={`New accounts per day for the last ${days} days`}
              footer={(row) => `${number.format(Number(row.organizations))} new workspaces`}
            />
          ) : (
            <ChartEmpty>No sign-ups in this period.</ChartEmpty>
          )}
        </SectionCard>
        <SectionCard
          title="Test messages"
          description={`${number.format(live.test)} simulated sends from test keys.`}
        >
          {anyTest ? (
            <StackedColumns
              data={messages.daily}
              series={testSeries}
              height={180}
              label={`Test messages per day for the last ${days} days`}
            />
          ) : (
            <ChartEmpty>No test messages in this period.</ChartEmpty>
          )}
        </SectionCard>
      </div>

      <SectionCard
        title="Top workspaces"
        description="By live SMS sent in this period."
        contentClassName="p-0"
      >
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-5">Workspace</TableHead>
              {billing ? <TableHead>Plan</TableHead> : null}
              <TableHead className="text-right">Phones</TableHead>
              <TableHead className="text-right">Live SMS</TableHead>
              <TableHead className="w-56 pr-5">Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.top_organizations.length === 0 ? (
              <NoRows colSpan={billing ? 5 : 4}>No workspace sent live SMS in this period.</NoRows>
            ) : (
              data.top_organizations.map((o) => (
                <TableRow key={o.organization_id} className="tabular-nums">
                  <TableCell className="pl-5">
                    <div className="font-medium">{o.name}</div>
                    <div className="font-mono text-[0.7rem] text-muted-foreground">
                      {o.organization_id}
                    </div>
                  </TableCell>
                  {billing ? <TableCell>{o.plan ?? '—'}</TableCell> : null}
                  <TableCell className="text-right">{number.format(o.phones)}</TableCell>
                  <TableCell className="text-right">{number.format(o.messages)}</TableCell>
                  <TableCell className="pr-5">
                    <ShareBar share={o.messages / topTotal} />
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </SectionCard>

      <SectionCard
        title="Recent sign-ups"
        description={`The newest accounts. ${number.format(totals.users_with_phone)} of ${number.format(totals.users)} have verified a mobile number.`}
        contentClassName="p-0"
      >
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-5">Account</TableHead>
              <TableHead>Joined</TableHead>
              <TableHead>Verified</TableHead>
              <TableHead className="pr-5 text-right">Workspaces</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.recent_signups.length === 0 ? (
              <NoRows colSpan={4}>No accounts yet.</NoRows>
            ) : (
              data.recent_signups.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="pl-5">
                    <div className="font-medium">{u.email}</div>
                    {u.name ? <div className="text-muted-foreground">{u.name}</div> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    <time dateTime={u.created_at} title={formatDateTime(u.created_at)}>
                      {formatRelative(u.created_at)}
                    </time>
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-3">
                      <StatusBadge kind={u.email_verified ? 'success' : 'neutral'}>
                        Email
                        <span className="sr-only">
                          {u.email_verified ? ' verified' : ' not verified'}
                        </span>
                      </StatusBadge>
                      <StatusBadge kind={u.phone_verified ? 'success' : 'neutral'}>
                        Phone
                        <span className="sr-only">
                          {u.phone_verified ? ' verified' : ' not verified'}
                        </span>
                      </StatusBadge>
                    </div>
                  </TableCell>
                  <TableCell className="pr-5 text-right tabular-nums">
                    {number.format(u.organizations)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </SectionCard>

      <div className="grid gap-6 lg:grid-cols-2">
        <SectionCard
          title="Top errors"
          description="Why live SMS failed in this period."
          contentClassName="p-0"
        >
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Error code</TableHead>
                <TableHead className="text-right">Messages</TableHead>
                <TableHead className="w-44 pr-5">Share of failures</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {messages.top_errors.length === 0 ? (
                <NoRows colSpan={3}>No live SMS failed in this period.</NoRows>
              ) : (
                messages.top_errors.map((e) => (
                  <TableRow key={e.error_code} className="tabular-nums">
                    <TableCell className="pl-5 font-mono text-xs">{e.error_code}</TableCell>
                    <TableCell className="text-right">{number.format(e.count)}</TableCell>
                    <TableCell className="pr-5">
                      <ShareBar share={e.count / Math.max(1, failedTotal)} />
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </SectionCard>
        <SectionCard
          title="Routes"
          description="Outgoing messages by route: android is a paired phone, simulator is test mode."
          contentClassName="p-0"
        >
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Route</TableHead>
                <TableHead className="text-right">Messages</TableHead>
                <TableHead className="w-44 pr-5">Share</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {messages.by_provider.length === 0 ? (
                <NoRows colSpan={3}>No messages sent in this period.</NoRows>
              ) : (
                messages.by_provider.map((p) => (
                  <TableRow key={p.provider} className="tabular-nums">
                    <TableCell className="pl-5 font-mono text-xs">{p.provider}</TableCell>
                    <TableCell className="text-right">{number.format(p.count)}</TableCell>
                    <TableCell className="pr-5">
                      <ShareBar share={p.count / Math.max(1, providerTotal)} />
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </SectionCard>
      </div>

      {billing ? <Billing billing={billing} /> : null}
    </>
  );
}

function Billing({ billing }: { billing: InsightsBilling }) {
  const workspaces = Math.max(
    1,
    billing.plan_mix.reduce((n, p) => n + p.organizations, 0),
  );
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <SectionCard
        title="Plan mix"
        description="Workspaces on each plan now."
        contentClassName="p-0"
      >
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-5">Plan</TableHead>
              <TableHead className="text-right">Price</TableHead>
              <TableHead className="text-right">Workspaces</TableHead>
              <TableHead className="w-44 pr-5">Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {billing.plan_mix.map((p) => (
              <TableRow key={p.plan} className="tabular-nums">
                <TableCell className="pl-5 font-medium">{p.name}</TableCell>
                <TableCell className="text-right text-muted-foreground">
                  {p.price_cents ? `${money(p.price_cents, billing.currency)}/mo` : 'Free'}
                </TableCell>
                <TableCell className="text-right">{number.format(p.organizations)}</TableCell>
                <TableCell className="pr-5">
                  <ShareBar share={p.organizations / workspaces} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </SectionCard>
      <SectionCard
        title="Recent billing changes"
        description="Subscriptions that changed most recently."
        contentClassName="p-0"
      >
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-5">Workspace</TableHead>
              <TableHead>Plan</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="pr-5 text-right">When</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {billing.recent_changes.length === 0 ? (
              <NoRows colSpan={4}>No paid subscriptions yet.</NoRows>
            ) : (
              billing.recent_changes.map((c) => (
                <TableRow key={c.organization_id}>
                  <TableCell className="pl-5 font-medium">{c.organization_name}</TableCell>
                  <TableCell>{c.plan}</TableCell>
                  <TableCell>
                    <StatusBadge kind={subscriptionKinds[c.status] ?? 'neutral'}>
                      {c.status.replace('_', ' ')}
                      {c.cancel_at_period_end && c.status === 'active' ? ', cancels' : ''}
                    </StatusBadge>
                  </TableCell>
                  <TableCell className="whitespace-nowrap pr-5 text-right text-muted-foreground">
                    <time dateTime={c.at} title={formatDateTime(c.at)}>
                      {formatRelative(c.at)}
                    </time>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </SectionCard>
    </div>
  );
}
