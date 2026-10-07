'use client';

import { usePathname } from 'next/navigation';
import * as React from 'react';

import { cn } from '@/lib/utils';

/**
 * Routes whose table rows already played their first-load entrance this
 * session. Module-level (client only) so nav-back never re-staggers.
 */
const staggeredRoutes = new Set<string>();

function Table({ className, ...props }: React.ComponentProps<'table'>) {
  return (
    <div data-slot="table-container" className="relative w-full overflow-x-auto">
      <table
        data-slot="table"
        className={cn('w-full caption-bottom text-xs', className)}
        {...props}
      />
    </div>
  );
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return <thead data-slot="table-header" className={cn('[&_tr]:border-b', className)} {...props} />;
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  const pathname = usePathname();
  // First render on a fresh route gets the row-stagger entrance (SSR included
  // — the server never mutates the set, so hydration always matches). The
  // attribute is dropped right after the entrance so sorting/pagination and
  // nav-back never re-stagger (see globals.css `tbody[data-stagger]`).
  const [stagger, setStagger] = React.useState(
    () => typeof window === 'undefined' || !staggeredRoutes.has(pathname),
  );

  // biome-ignore lint/correctness/useExhaustiveDependencies: mount-only; the entrance is decided once per mounted table
  React.useEffect(() => {
    staggeredRoutes.add(pathname);
    if (!stagger) return;
    const timer = setTimeout(() => setStagger(false), 700);
    return () => clearTimeout(timer);
  }, []);

  return (
    <tbody
      data-slot="table-body"
      data-stagger={stagger || undefined}
      className={cn('[&_tr:last-child]:border-0', className)}
      {...props}
    />
  );
}

function TableFooter({ className, ...props }: React.ComponentProps<'tfoot'>) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn('border-t bg-muted/50 font-medium [&>tr]:last:border-b-0', className)}
      {...props}
    />
  );
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        'border-b transition-colors hover:bg-muted/50 has-aria-expanded:bg-muted/50 data-[state=selected]:bg-muted',
        className,
      )}
      {...props}
    />
  );
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        'h-10 px-2 text-left align-middle font-medium whitespace-nowrap text-foreground [&:has([role=checkbox])]:pr-0',
        className,
      )}
      {...props}
    />
  );
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return (
    <td
      data-slot="table-cell"
      className={cn('p-2 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0', className)}
      {...props}
    />
  );
}

function TableCaption({ className, ...props }: React.ComponentProps<'caption'>) {
  return (
    <caption
      data-slot="table-caption"
      className={cn('mt-4 text-xs text-muted-foreground', className)}
      {...props}
    />
  );
}

export { Table, TableBody, TableCaption, TableCell, TableFooter, TableHead, TableHeader, TableRow };
