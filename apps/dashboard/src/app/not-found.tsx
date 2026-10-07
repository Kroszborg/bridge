import Link from 'next/link';
import { Wordmark } from '@/components/brand';
import { Button } from '@/components/ui/button';

export default function NotFound() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-6 text-center">
      <Wordmark />
      <div>
        <p className="font-mono text-xs text-faint">404</p>
        <h1 className="mt-1 font-display text-xl font-semibold">This page does not exist</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          The link may be old, or the project may belong to another account.
        </p>
      </div>
      <Button asChild size="lg">
        <Link href="/">Go to your projects</Link>
      </Button>
    </div>
  );
}
