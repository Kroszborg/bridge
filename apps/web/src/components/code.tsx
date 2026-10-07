'use client';

import { Copy01Icon, Tick02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useEffect, useId, useState } from 'react';

export function CopyButton({ value, label = 'Copy' }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <button
      type="button"
      onClick={() =>
        navigator.clipboard.writeText(value).then(
          () => setCopied(true),
          () => undefined,
        )
      }
      aria-label={copied ? 'Copied' : label}
      className="grid size-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
    >
      <HugeiconsIcon
        icon={copied ? Tick02Icon : Copy01Icon}
        strokeWidth={2}
        className={copied ? 'size-4 text-primary' : 'size-4'}
      />
    </button>
  );
}

export type Snippet = { id: string; label: string; language: string; code: string };

/** Tabbed code samples: a real tablist where arrow keys move between tabs. */
export function CodeTabs({ snippets }: { snippets: Snippet[] }) {
  const [active, setActive] = useState(snippets[0]?.id ?? '');
  const base = useId();
  const current = snippets.find((s) => s.id === active) ?? snippets[0];
  if (!current) return null;
  const move = (dir: number) => {
    const i = snippets.findIndex((s) => s.id === active);
    const next = snippets[(i + dir + snippets.length) % snippets.length];
    if (next) {
      setActive(next.id);
      document.getElementById(`${base}-tab-${next.id}`)?.focus();
    }
  };
  return (
    <div className="overflow-hidden rounded-2xl border bg-card">
      <div className="flex items-center justify-between gap-3 border-b px-2">
        <div role="tablist" aria-label="Code samples" className="flex">
          {snippets.map((s) => (
            <button
              key={s.id}
              id={`${base}-tab-${s.id}`}
              type="button"
              role="tab"
              aria-selected={s.id === active}
              aria-controls={`${base}-panel`}
              tabIndex={s.id === active ? 0 : -1}
              onClick={() => setActive(s.id)}
              onKeyDown={(e) => {
                if (e.key === 'ArrowRight') move(1);
                if (e.key === 'ArrowLeft') move(-1);
              }}
              className={`relative px-4 py-3 text-sm font-medium transition-colors ${
                s.id === active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {s.label}
              {s.id === active ? (
                <span
                  aria-hidden
                  className="absolute inset-x-3 -bottom-px h-0.5 rounded-full bg-primary"
                />
              ) : null}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2 pr-1">
          <span className="hidden font-mono text-[0.7rem] text-faint sm:inline">
            {current.language}
          </span>
          <CopyButton value={current.code} label="Copy code" />
        </div>
      </div>
      <pre
        id={`${base}-panel`}
        role="tabpanel"
        aria-labelledby={`${base}-tab-${current.id}`}
        className="overflow-x-auto p-5 font-mono text-[0.8rem] leading-relaxed text-foreground"
      >
        <code>{current.code}</code>
      </pre>
    </div>
  );
}
