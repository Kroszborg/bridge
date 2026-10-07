import { cn } from '@/lib/utils';

/** The Bridge mark: a suspension bridge, two towers carrying a deck. Inherits currentColor. */
export function BridgeMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={className}
    >
      <path d="M6.5 20V5M17.5 20V5M2 15.5h20M2.5 10 6.5 5c2.5 6 8.5 6 11 0l4 5" />
    </svg>
  );
}

/** Mark on an accent tile plus the wordmark. */
export function Wordmark({ className, subtitle }: { className?: string; subtitle?: string }) {
  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground">
        <BridgeMark className="size-5" />
      </span>
      <div className="leading-tight">
        <div className="font-display text-[0.95rem] font-bold tracking-tight">Bridge</div>
        {subtitle ? (
          <div className="text-[0.6rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">
            {subtitle}
          </div>
        ) : null}
      </div>
    </div>
  );
}
