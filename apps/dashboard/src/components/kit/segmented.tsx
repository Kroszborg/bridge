'use client';

import { cn } from '@/lib/utils';

/** A small row of mutually exclusive choices, such as Live / Test. */
export function Segmented<T extends string | number>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (v: T) => void;
}) {
  return (
    <fieldset className="inline-flex gap-1 rounded-lg border bg-background p-1">
      <legend className="sr-only">{label}</legend>
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-md px-3 py-1 text-xs font-medium transition-colors',
            value === o.value
              ? 'bg-muted text-foreground ring-1 ring-border'
              : 'text-muted-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </fieldset>
  );
}
