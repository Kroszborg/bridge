'use client';

import { EmptyState } from '@/components/kit/empty-state';
import { Button } from '@/components/ui/button';

export default function ConsoleError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <EmptyState
      title="This page failed to load"
      description={
        error.digest
          ? `Reference ${error.digest}. Try again, or check the dashboard server logs.`
          : error.message
      }
      action={
        <Button variant="outline" onClick={reset}>
          Try again
        </Button>
      }
    />
  );
}
