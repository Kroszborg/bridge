/** One headline number with a label and an optional note. */
export function StatTile({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-xl border bg-card p-4">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-display text-2xl font-semibold tracking-tight">{value}</span>
      {note ? <span className="truncate text-xs text-muted-foreground">{note}</span> : null}
    </div>
  );
}
