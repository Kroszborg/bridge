'use client';

import { ArrowDown01Icon, Search01Icon, Tick02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { Command } from 'cmdk';
import { Popover as PopoverPrimitive } from 'radix-ui';
import { useMemo, useState } from 'react';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { type Environment, useDevices } from '@/lib/queries';
import { type SegmentInfo, segmentInfo } from '@/lib/sms';
import { cn } from '@/lib/utils';

const ENVIRONMENTS: { value: Environment; label: string; hint: string }[] = [
  { value: 'test', label: 'Test', hint: 'Simulated. Nothing is sent.' },
  { value: 'live', label: 'Live', hint: 'Real SMS through your phones or providers.' },
];

/** Test or Live as two radio cards. Live needs an admin. */
export function EnvironmentChoice({
  name,
  value,
  onChange,
  canLive,
  hideLegend = false,
  className,
}: {
  name: string;
  value: Environment;
  onChange: (v: Environment) => void;
  canLive: boolean;
  /** Keep the legend for screen readers only, when a heading already names the group. */
  hideLegend?: boolean;
  className?: string;
}) {
  return (
    <fieldset className={cn('flex flex-col gap-2', className)}>
      <legend className={hideLegend ? 'sr-only' : 'mb-2 text-sm font-medium'}>Environment</legend>
      <div className="grid grid-cols-2 gap-2">
        {ENVIRONMENTS.map((env) => {
          const disabled = env.value === 'live' && !canLive;
          return (
            <label
              key={env.value}
              className={cn(
                'flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5',
                disabled && 'cursor-not-allowed opacity-60',
              )}
            >
              <input
                type="radio"
                name={name}
                checked={value === env.value}
                onChange={() => onChange(env.value)}
                disabled={disabled}
                className="mt-0.5 accent-primary"
              />
              <span className="flex flex-col">
                <span className="text-sm font-medium">{env.label}</span>
                <span className="text-xs text-muted-foreground">{env.hint}</span>
              </span>
            </label>
          );
        })}
      </div>
      {!canLive ? (
        <p className="text-xs text-muted-foreground">Only admins can send live messages.</p>
      ) : null}
    </fieldset>
  );
}

/** "GSM-7 · 42/160 characters   1 segment", as under the Playground's message box. */
export function SegmentLine({
  text,
  info,
  prefix,
}: {
  text: string;
  info?: SegmentInfo;
  prefix?: string;
}) {
  const seg = info ?? segmentInfo(text);
  return (
    <>
      <p className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground">
        <span>
          {prefix ? `${prefix} · ` : ''}
          {seg.encoding === 'gsm7' ? 'GSM-7' : 'Unicode (UCS-2)'} · {seg.units}/
          {seg.perSegment * seg.segments} {seg.encoding === 'gsm7' ? 'characters' : 'units'}
        </span>
        <span className={cn(seg.segments > 1 && 'font-medium text-foreground')}>
          {seg.segments} segment{seg.segments > 1 ? 's' : ''}
        </span>
      </p>
      {seg.encoding === 'ucs2' && text.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          {seg.unicodeChars.map((c) => `“${c}”`).join(' ')} switch the message to Unicode, which
          fits 70 characters per SMS instead of 160.
        </p>
      ) : null}
    </>
  );
}

/** Choose a phone, or let Bridge pick. Value "auto" means Bridge picks. */
export function DeviceSelect({
  id,
  projectId,
  value,
  onChange,
  disabled,
}: {
  id: string;
  projectId: string;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  const devices = useDevices(projectId, 30_000);
  const known = (devices.data ?? []).some((d) => d.id === value);
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="auto">Bridge picks</SelectItem>
        {(devices.data ?? [])
          .filter((d) => d.status !== 'disabled')
          .map((d) => (
            <SelectItem key={d.id} value={d.id}>
              {d.name} {d.status === 'online' ? '' : '(offline)'}
            </SelectItem>
          ))}
        {value !== 'auto' && !known ? (
          <SelectItem value={value}>
            <span className="font-mono">{value}</span>
          </SelectItem>
        ) : null}
      </SelectContent>
    </Select>
  );
}

// Browsers still report some zones by their old IANA names; show the current ones.
const RENAMED: Record<string, string> = {
  'Asia/Calcutta': 'Asia/Kolkata',
  'Asia/Katmandu': 'Asia/Kathmandu',
  'Asia/Rangoon': 'Asia/Yangon',
  'Asia/Saigon': 'Asia/Ho_Chi_Minh',
  'Atlantic/Faeroe': 'Atlantic/Faroe',
  'Europe/Kiev': 'Europe/Kyiv',
  'America/Godthab': 'America/Nuuk',
  'Pacific/Truk': 'Pacific/Chuuk',
  'Pacific/Ponape': 'Pacific/Pohnpei',
};

function allTimeZones(): string[] {
  try {
    const zones = [...new Set(Intl.supportedValuesOf('timeZone').map((z) => RENAMED[z] ?? z))];
    return zones.includes('UTC') ? zones : ['UTC', ...zones];
  } catch {
    return ['UTC'];
  }
}

/** The browser's IANA time zone, or UTC. */
export function browserTimeZone(): string {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    return RENAMED[zone] ?? zone;
  } catch {
    return 'UTC';
  }
}

function offsetLabel(zone: string): string {
  try {
    const part = new Intl.DateTimeFormat('en', { timeZone: zone, timeZoneName: 'shortOffset' })
      .formatToParts(new Date())
      .find((p) => p.type === 'timeZoneName');
    return part?.value ?? '';
  } catch {
    return '';
  }
}

/** A searchable list of every IANA time zone the browser knows. */
export function TimeZoneSelect({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const zones = useMemo(() => {
    const list = allTimeZones();
    return list.includes(value) || !value ? list : [value, ...list];
  }, [value]);

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild disabled={disabled}>
        <button
          id={id}
          type="button"
          role="combobox"
          aria-expanded={open}
          aria-controls={`${id}-list`}
          className="flex h-7 w-full items-center justify-between gap-2 rounded-md border border-input bg-input/20 px-2 py-0.5 text-left text-xs/relaxed outline-none transition-colors focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30"
        >
          <span className="truncate">
            {value.replaceAll('_', ' ')}{' '}
            <span className="text-muted-foreground">{offsetLabel(value)}</span>
          </span>
          <HugeiconsIcon
            icon={ArrowDown01Icon}
            strokeWidth={2}
            className="size-3.5 shrink-0 text-muted-foreground"
          />
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="start"
          sideOffset={4}
          className="z-50 w-(--radix-popover-trigger-width) min-w-64 overflow-hidden rounded-lg bg-popover text-popover-foreground shadow-md ring-1 ring-foreground/10"
        >
          <Command loop>
            <div className="flex items-center gap-2 border-b px-2.5">
              <HugeiconsIcon
                icon={Search01Icon}
                strokeWidth={2}
                className="size-3.5 text-muted-foreground"
              />
              <Command.Input
                placeholder="Search time zones"
                className="h-8 flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground"
              />
            </div>
            <Command.List id={`${id}-list`} className="scroll-slim max-h-64 overflow-y-auto p-1">
              <Command.Empty className="px-3 py-4 text-center text-xs text-muted-foreground">
                No time zone matches.
              </Command.Empty>
              {zones.map((z) => (
                <Command.Item
                  key={z}
                  value={z}
                  keywords={[z.replaceAll('_', ' ')]}
                  onSelect={() => {
                    onChange(z);
                    setOpen(false);
                  }}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-xs aria-selected:bg-muted"
                >
                  <HugeiconsIcon
                    icon={Tick02Icon}
                    strokeWidth={2}
                    className={cn('size-3 shrink-0', z === value ? 'text-primary' : 'invisible')}
                  />
                  <span className="flex-1 truncate">{z.replaceAll('_', ' ')}</span>
                </Command.Item>
              ))}
            </Command.List>
          </Command>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}

/** Small uppercase label over a fact, as on other detail views. */
export function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
        {label}
      </dt>
      <dd className="truncate text-sm">{children}</dd>
    </div>
  );
}

/** An inline error or hint under a field. */
export function FieldNote({ error, children }: { error?: string; children?: React.ReactNode }) {
  if (error) return <p className="text-xs text-destructive">{error}</p>;
  if (!children) return null;
  return <p className="text-xs/relaxed text-muted-foreground">{children}</p>;
}
