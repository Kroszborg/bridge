import type * as React from 'react';
import { cn } from '@/lib/utils';

/** A titled card: header row with an optional action, then the body. */
export function SectionCard({
  title,
  description,
  action,
  children,
  contentClassName,
  className,
  ...props
}: Omit<React.ComponentProps<'section'>, 'title'> & {
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  contentClassName?: string;
}) {
  return (
    <section
      data-slot="section-card"
      className={cn(
        'flex min-w-0 flex-col rounded-xl border bg-card text-card-foreground',
        className,
      )}
      {...props}
    >
      <div className="flex items-center justify-between gap-3 border-b px-5 py-3.5">
        <div className="min-w-0">
          <h2 className="font-display text-sm font-semibold">{title}</h2>
          {description ? (
            <p className="mt-0.5 text-xs/relaxed text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {action ? <div className="shrink-0 text-xs/relaxed">{action}</div> : null}
      </div>
      <div className={cn('p-5', contentClassName)}>{children}</div>
    </section>
  );
}
