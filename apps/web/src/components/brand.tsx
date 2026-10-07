/** The Bridge mark: two towers carrying a deck. Inherits currentColor. */
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

export function Wordmark() {
  return (
    <span className="flex items-center gap-2.5">
      <span className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground">
        <BridgeMark className="size-5" />
      </span>
      <span className="font-display text-[1.05rem] font-bold tracking-tight">Bridge</span>
    </span>
  );
}
