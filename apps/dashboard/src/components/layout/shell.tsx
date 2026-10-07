'use client';

import { Cancel01Icon, Menu01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { usePathname } from 'next/navigation';
import { type ReactNode, useEffect, useState } from 'react';
import { Wordmark } from '@/components/brand';
import { CommandPalette, CommandTrigger } from '@/components/command-palette';
import { ThemeToggle } from '@/components/theme-toggle';
import { cn } from '@/lib/utils';
import { Breadcrumb } from './breadcrumb';
import { NavList } from './nav-list';
import { ProjectSwitcher } from './project-switcher';
import { UserMenu } from './user-menu';

function RailContent({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <>
      <ProjectSwitcher />
      <NavList onNavigate={onNavigate} />
      <div className="border-t p-3">
        <UserMenu />
      </div>
    </>
  );
}

function MobileNav() {
  const [open, setOpen] = useState(false);
  const pathname = usePathname();

  // biome-ignore lint/correctness/useExhaustiveDependencies: close the drawer on navigation
  useEffect(() => setOpen(false), [pathname]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('keydown', onKey);
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = '';
    };
  }, [open]);

  return (
    <div className="md:hidden">
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="Open navigation"
        className="grid size-9 place-items-center rounded-lg border bg-card text-muted-foreground transition-colors hover:bg-muted"
      >
        <HugeiconsIcon icon={Menu01Icon} strokeWidth={2} className="size-5" />
      </button>
      <div
        aria-hidden
        onClick={() => setOpen(false)}
        className={cn(
          'fixed inset-0 z-40 bg-black/50 transition-opacity',
          open ? 'opacity-100' : 'pointer-events-none opacity-0',
        )}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Navigation"
        inert={!open}
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85%] flex-col border-r bg-sidebar shadow-xl transition-transform duration-200',
          open ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex h-16 items-center justify-between px-5">
          <Wordmark subtitle="Console" />
          <button
            type="button"
            onClick={() => setOpen(false)}
            aria-label="Close navigation"
            className="grid size-8 place-items-center rounded-lg text-muted-foreground hover:bg-muted"
          >
            <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-4" />
          </button>
        </div>
        <RailContent onNavigate={() => setOpen(false)} />
      </div>
    </div>
  );
}

function Topbar({ children }: { children: ReactNode }) {
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const scroller = document.querySelector('[data-shell-scroll]');
    if (!scroller) return;
    const onScroll = () => setScrolled(scroller.scrollTop > 2);
    onScroll();
    scroller.addEventListener('scroll', onScroll, { passive: true });
    return () => scroller.removeEventListener('scroll', onScroll);
  }, []);
  return (
    <header
      className={cn(
        'flex h-16 shrink-0 items-center gap-3 border-b px-4 transition-[background-color,border-color] duration-200 sm:px-6',
        scrolled ? 'border-border bg-card/75 backdrop-blur-md' : 'border-transparent bg-background',
      )}
    >
      {children}
    </header>
  );
}

/** Console frame: fixed rail on desktop, drawer on mobile; only <main> scrolls. */
export function ConsoleShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-dvh overflow-hidden">
      <aside className="hidden w-64 shrink-0 flex-col border-r bg-sidebar md:flex">
        <div className="flex h-16 items-center px-5">
          <Wordmark subtitle="Console" />
        </div>
        <RailContent />
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar>
          <MobileNav />
          <Breadcrumb />
          <div className="ml-auto flex items-center gap-2">
            <CommandTrigger />
            <ThemeToggle />
            <UserMenu compact />
          </div>
        </Topbar>
        <main
          data-shell-scroll
          className="route-enter scroll-slim flex-1 overflow-y-auto p-4 sm:p-6"
        >
          {children}
        </main>
      </div>
      <CommandPalette />
    </div>
  );
}
