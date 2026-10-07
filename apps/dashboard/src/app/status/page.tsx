'use client';

import Link from 'next/link';
import { Wordmark } from '@/components/brand';
import { ComponentRow, OverallBanner, StatusLegend } from '@/components/status';
import { ThemeToggle } from '@/components/theme-toggle';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useStatusPage } from '@/lib/queries';

/** Public service status: no sign-in, no customer data. */
export default function StatusPage() {
  const status = useStatusPage();
  return (
    <div className="min-h-dvh bg-background">
      <header className="border-b">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-4 sm:px-6">
          <Link href="/" aria-label="Bridge console">
            <Wordmark subtitle="Status" />
          </Link>
          <ThemeToggle />
        </div>
      </header>
      <main className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8 sm:px-6 sm:py-12">
        {status.isPending ? (
          <>
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-96 w-full" />
          </>
        ) : status.isError ? (
          <div
            role="alert"
            className="flex flex-col items-start gap-3 rounded-xl border border-destructive/40 bg-destructive/8 p-5"
          >
            <p className="font-display text-lg font-semibold">The Bridge API is not responding</p>
            <p className="text-sm text-muted-foreground">
              The status service itself is unreachable, which usually means the API is down. This
              page retries automatically.
            </p>
            <Button variant="outline" size="sm" onClick={() => status.refetch()}>
              Try again
            </Button>
          </div>
        ) : (
          <>
            <OverallBanner state={status.data.status} updatedAt={status.data.updated_at} />
            <section className="overflow-hidden rounded-xl border bg-card">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3.5">
                <h1 className="font-display text-sm font-semibold">Components</h1>
                <StatusLegend />
              </div>
              <ul className="divide-y">
                {status.data.components.map((c) => (
                  <ComponentRow key={c.id} c={c} />
                ))}
              </ul>
            </section>
            <p className="text-center text-xs/relaxed text-muted-foreground">
              Health is measured every minute by this Bridge installation. Days are UTC. Phones are
              shown for information: a phone going offline is the owner&apos;s to fix, not a Bridge
              outage.
            </p>
          </>
        )}
      </main>
    </div>
  );
}
