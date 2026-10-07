'use client';

import type { ReactNode } from 'react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { cn } from '@/lib/utils';

/**
 * Chart conventions (validated palette in globals.css, --viz-1..3):
 * thin marks (bars <= 24px, 2px lines), 4px rounded data-ends, a 2px surface
 * gap between stacked segments, solid hairline grid, one y-axis, a legend for
 * two or more series, and a hover tooltip where values lead.
 */
export type Series<K extends string> = {
  key: K;
  label: string;
  /** A CSS color, normally var(--viz-n). */
  color: string;
};

const number = new Intl.NumberFormat('en');
const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });
const dayFmt = new Intl.DateTimeFormat('en', { day: 'numeric', month: 'short', timeZone: 'UTC' });
const weekdayFmt = new Intl.DateTimeFormat('en', {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  timeZone: 'UTC',
});

/** "2026-10-05" → "5 Oct". Dates are calendar days, so they are formatted in UTC. */
export function shortDay(date: string): string {
  return dayFmt.format(new Date(`${date}T00:00:00Z`));
}

function longDay(date: string): string {
  return weekdayFmt.format(new Date(`${date}T00:00:00Z`));
}

export function Legend<K extends string>({
  series,
  mark = 'rect',
}: {
  series: Series<K>[];
  mark?: 'rect' | 'line';
}) {
  if (series.length < 2) return null;
  return (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {series.map((s) => (
        <li key={s.key} className="flex items-center gap-1.5">
          <span
            aria-hidden
            className={cn(
              'shrink-0',
              mark === 'rect' ? 'size-2.5 rounded-[3px]' : 'h-0.5 w-3 rounded-full',
            )}
            style={{ background: s.color }}
          />
          {s.label}
        </li>
      ))}
    </ul>
  );
}

type Row = Record<string, string | number>;

function TooltipBox<K extends string>({
  active,
  payload,
  label,
  series,
  format,
  footer,
}: {
  active?: boolean;
  payload?: readonly { payload?: unknown }[];
  label?: unknown;
  series: Series<K>[];
  format: (v: number) => string;
  footer?: (row: Row) => ReactNode;
}) {
  if (!active || !payload?.length) return null;
  const row = payload[0]?.payload as Row;
  return (
    <div className="min-w-40 rounded-lg border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="mb-1.5 font-medium text-foreground">{longDay(String(label))}</p>
      <ul className="flex flex-col gap-1">
        {[...series].reverse().map((s) => (
          <li key={s.key} className="flex items-center gap-2">
            <span
              aria-hidden
              className="h-0.5 w-3 shrink-0 rounded-full"
              style={{ background: s.color }}
            />
            <span className="font-semibold tabular-nums text-foreground">
              {format(Number(row[s.key] ?? 0))}
            </span>
            <span className="text-muted-foreground">{s.label}</span>
          </li>
        ))}
      </ul>
      {footer ? (
        <div className="mt-1.5 border-t pt-1.5 text-muted-foreground">{footer(row)}</div>
      ) : null}
    </div>
  );
}

const axisTick = { fill: 'var(--muted-foreground)', fontSize: 11 };

function xInterval(n: number): number | 'preserveStartEnd' {
  if (n <= 10) return 0;
  return 'preserveStartEnd';
}

/** Daily stacked columns. Series are stacked bottom-up in the given order. */
export function StackedColumns<K extends string>({
  data,
  series,
  height = 220,
  format = (v) => number.format(v),
  footer,
  label,
}: {
  data: (Row & { date: string })[];
  series: Series<K>[];
  height?: number;
  format?: (v: number) => string;
  footer?: (row: Row) => ReactNode;
  /** Accessible name of the chart. */
  label: string;
}) {
  const last = series.length - 1;
  return (
    <div role="img" aria-label={label} className="w-full" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          margin={{ top: 4, right: 4, bottom: 0, left: -12 }}
          barCategoryGap="20%"
        >
          <CartesianGrid vertical={false} stroke="var(--viz-grid)" />
          <XAxis
            dataKey="date"
            tickFormatter={shortDay}
            tick={axisTick}
            tickLine={false}
            axisLine={{ stroke: 'var(--viz-axis)' }}
            interval={xInterval(data.length)}
            minTickGap={16}
          />
          <YAxis
            tick={axisTick}
            tickLine={false}
            axisLine={false}
            allowDecimals={false}
            tickFormatter={(v: number) => compact.format(v)}
            width={44}
          />
          <Tooltip
            cursor={{ fill: 'var(--muted)', opacity: 0.6 }}
            content={(p) => (
              <TooltipBox
                active={p.active}
                payload={p.payload}
                label={p.label}
                series={series}
                format={format}
                footer={footer}
              />
            )}
          />
          {series.map((s, i) => (
            <Bar
              key={s.key}
              dataKey={s.key}
              name={s.label}
              stackId="a"
              fill={s.color}
              maxBarSize={24}
              // The surface-colored stroke is the 2px gap between stacked segments.
              stroke="var(--card)"
              strokeWidth={i === 0 ? 0 : 2}
              radius={i === last ? [4, 4, 0, 0] : 0}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

/** A single daily line, for a measure on its own scale (never a second axis). */
export function DailyLine({
  data,
  dataKey,
  color = 'var(--viz-1)',
  name,
  height = 160,
  format,
  label,
}: {
  data: (Row & { date: string })[];
  dataKey: string;
  color?: string;
  name: string;
  height?: number;
  format: (v: number) => string;
  label: string;
}) {
  const series: Series<string>[] = [{ key: dataKey, label: name, color }];
  return (
    <div role="img" aria-label={label} className="w-full" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
          <CartesianGrid vertical={false} stroke="var(--viz-grid)" />
          <XAxis
            dataKey="date"
            tickFormatter={shortDay}
            tick={axisTick}
            tickLine={false}
            axisLine={{ stroke: 'var(--viz-axis)' }}
            interval={xInterval(data.length)}
            minTickGap={16}
          />
          <YAxis
            tick={axisTick}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v: number) => format(v)}
            width={52}
          />
          <Tooltip
            cursor={{ stroke: 'var(--viz-axis)', strokeWidth: 1 }}
            content={(p) => (
              <TooltipBox
                active={p.active}
                payload={p.payload}
                label={p.label}
                series={series}
                format={format}
              />
            )}
          />
          <Line
            type="monotone"
            dataKey={dataKey}
            name={name}
            stroke={color}
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
            dot={false}
            activeDot={{ r: 4, fill: color, stroke: 'var(--card)', strokeWidth: 2 }}
            connectNulls={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
