import type * as React from 'react';
import { cn } from '@/lib/utils';

export type StatusKind = 'success' | 'warning' | 'danger' | 'info' | 'neutral';

const styles: Record<StatusKind, { text: string; dot: string }> = {
  success: { text: 'text-success', dot: 'bg-success' },
  warning: { text: 'text-warning', dot: 'bg-warning' },
  danger: { text: 'text-destructive', dot: 'bg-destructive' },
  info: { text: 'text-primary', dot: 'bg-primary' },
  neutral: { text: 'text-muted-foreground', dot: 'bg-faint' },
};

/** Status as a coloured dot and label. `live` makes the dot pulse like a signal. */
export function StatusBadge({
  kind = 'neutral',
  live = false,
  className,
  children,
  ...props
}: React.ComponentProps<'span'> & { kind?: StatusKind; live?: boolean }) {
  const s = styles[kind];
  return (
    <span
      data-slot="status-badge"
      data-kind={kind}
      className={cn(
        'inline-flex w-fit items-center gap-1.5 whitespace-nowrap text-[0.8rem] font-medium',
        s.text,
        className,
      )}
      {...props}
    >
      <span
        className={cn('size-1.5 shrink-0 rounded-full', s.dot, live && 'signal-live')}
        aria-hidden
      />
      {children}
    </span>
  );
}
