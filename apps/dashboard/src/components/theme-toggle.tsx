'use client';

import { ComputerIcon, Moon02Icon, Sun01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useTheme } from 'next-themes';
import { useSyncExternalStore } from 'react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const emptySubscribe = () => () => {};
const useHydrated = () =>
  useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false,
  );

/** Icon button that flips between dark and light. */
export function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  const hydrated = useHydrated();
  const dark = !hydrated || resolvedTheme === 'dark';
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
    >
      <HugeiconsIcon
        icon={dark ? Sun01Icon : Moon02Icon}
        strokeWidth={2}
        className="size-[1.1rem]"
      />
    </Button>
  );
}

const OPTIONS = [
  { value: 'dark', label: 'Dark', icon: Moon02Icon },
  { value: 'light', label: 'Light', icon: Sun01Icon },
  { value: 'system', label: 'System', icon: ComputerIcon },
] as const;

/** Three-way theme selector for the account page. */
export function ThemeSelector() {
  const { theme, setTheme } = useTheme();
  const hydrated = useHydrated();
  return (
    <fieldset className="inline-flex gap-1 rounded-lg border bg-background p-1">
      <legend className="sr-only">Theme</legend>
      {OPTIONS.map((o) => {
        const active = hydrated && theme === o.value;
        return (
          <label
            key={o.value}
            className={cn(
              'inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring',
              active
                ? 'bg-muted text-foreground ring-1 ring-border'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            <input
              type="radio"
              name="theme"
              value={o.value}
              checked={active}
              onChange={() => setTheme(o.value)}
              className="sr-only"
            />
            <HugeiconsIcon icon={o.icon} strokeWidth={2} className="size-3.5" />
            {o.label}
          </label>
        );
      })}
    </fieldset>
  );
}
