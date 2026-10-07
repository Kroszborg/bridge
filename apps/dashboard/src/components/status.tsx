'use client';

import type { StatusComponent, StatusDay } from '@bridge/api-types';
import { Alert02Icon, CancelCircleIcon, CheckmarkCircle02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useState } from 'react';
import { cn } from '@/lib/utils';

type State = 'operational' | 'degraded' | 'outage';

export const STATE: Record<
  State,
  { label: string; text: string; bg: string; icon: typeof CheckmarkCircle02Icon }
> = {
  operational: {
    label: 'Operational',
    text: 'text-success',
    bg: 'bg-success',
    icon: CheckmarkCircle02Icon,
  },
  degraded: { label: 'Degraded', text: 'text-warning', bg: 'bg-warning', icon: Alert02Icon },
  outage: {
    label: 'Outage',
    text: 'text-destructive',
    bg: 'bg-destructive',
    icon: CancelCircleIcon,
  },
};

const OVERALL: Record<State, string> = {
  operational: 'All systems operational',
  degraded: 'Some systems are degraded',
  outage: 'Some systems are down',
};

const pct = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 2 });
const dayFmt = new Intl.DateTimeFormat('en', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
});

/** State as an icon and a label: never color alone. */
export function StateLabel({ state, className }: { state: State; className?: string }) {
  const s = STATE[state];
  return (
    <span className={cn('inline-flex items-center gap-1.5 text-sm font-medium', s.text, className)}>
      <HugeiconsIcon icon={s.icon} strokeWidth={2} className="size-4" />
      {s.label}
    </span>
  );
}

export function OverallBanner({ state, updatedAt }: { state: State; updatedAt?: string }) {
  const s = STATE[state];
  return (
    <div
      role="status"
      className={cn(
        'flex flex-wrap items-center justify-between gap-3 rounded-xl border p-5',
        state === 'operational' && 'border-success/30 bg-success/8',
        state === 'degraded' && 'border-warning/40 bg-warning/8',
        state === 'outage' && 'border-destructive/40 bg-destructive/8',
      )}
    >
      <span className="flex items-center gap-3">
        <HugeiconsIcon icon={s.icon} strokeWidth={2} className={cn('size-6', s.text)} />
        <span className="font-display text-lg font-semibold">{OVERALL[state]}</span>
      </span>
      {updatedAt ? (
        <span className="text-xs text-muted-foreground">
          Updated{' '}
          {new Date(updatedAt).toLocaleTimeString([], {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
          })}
        </span>
      ) : null}
    </div>
  );
}

function dayClass(d: StatusDay) {
  switch (d.status) {
    case 'operational':
      return 'bg-success';
    case 'degraded':
      return 'bg-warning';
    case 'outage':
      return 'bg-destructive';
    default:
      return 'bg-muted';
  }
}

function dayText(d: StatusDay) {
  const date = dayFmt.format(new Date(`${d.date}T00:00:00Z`));
  if (d.status === 'no_data' || d.uptime == null) return `${date}: no data`;
  return `${date}: ${STATE[d.status as State].label.toLowerCase()}, ${pct.format(d.uptime)} uptime`;
}

/** 90 daily bars, oldest first. Each bar is focusable and has its own readout. */
export function UptimeBars({ days }: { days: StatusDay[] }) {
  const [hover, setHover] = useState<StatusDay | null>(null);
  const shown = hover ?? null;
  return (
    <div className="flex flex-col gap-1.5">
      <ol className="flex h-8 items-stretch gap-[2px]" aria-label="Daily uptime, last 90 days">
        {days.map((d) => (
          <li key={d.date} className="flex min-w-0 flex-1">
            <button
              type="button"
              aria-label={dayText(d)}
              onMouseEnter={() => setHover(d)}
              onMouseLeave={() => setHover(null)}
              onFocus={() => setHover(d)}
              onBlur={() => setHover(null)}
              className={cn(
                'w-full rounded-[2px] transition-opacity hover:opacity-70 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring',
                dayClass(d),
              )}
            />
          </li>
        ))}
      </ol>
      <div className="flex justify-between text-[0.7rem] text-muted-foreground">
        <span>90 days ago</span>
        <span aria-live="polite" className="font-medium text-foreground">
          {shown ? dayText(shown) : ''}
        </span>
        <span>Today</span>
      </div>
    </div>
  );
}

export function ComponentRow({ c }: { c: StatusComponent }) {
  return (
    <li className="flex flex-col gap-3 px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">{c.name}</span>
            {c.informational ? (
              <span className="rounded-full border px-1.5 text-[0.6rem] font-semibold uppercase tracking-wider text-muted-foreground">
                Info
              </span>
            ) : null}
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">{c.detail || c.description}</p>
        </div>
        <div className="flex items-center gap-3">
          {c.uptime_90d != null ? (
            <span className="text-xs tabular-nums text-muted-foreground">
              {pct.format(c.uptime_90d)} uptime
            </span>
          ) : null}
          <StateLabel state={c.status as State} />
        </div>
      </div>
      <UptimeBars days={c.days} />
    </li>
  );
}

export function StatusLegend() {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {(['operational', 'degraded', 'outage'] as const).map((s) => (
        <li key={s} className="flex items-center gap-1.5">
          <span aria-hidden className={cn('size-2.5 rounded-[2px]', STATE[s].bg)} />
          {STATE[s].label}
        </li>
      ))}
      <li className="flex items-center gap-1.5">
        <span aria-hidden className="size-2.5 rounded-[2px] bg-muted" />
        No data
      </li>
    </ul>
  );
}
