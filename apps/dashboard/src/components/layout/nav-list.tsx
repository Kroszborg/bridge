'use client';

import { ArrowUpRight01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import * as React from 'react';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import { useCan, useConsole, useOrganization, useProjectId } from './console-context';
import { buildNav, isActive } from './nav-items';

/**
 * Grouped navigation shared by the desktop rail and the mobile drawer. A thin
 * phosphor indicator at the rail's left edge slides between active items.
 */
export function NavList({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const projectId = useProjectId();
  const { apiUrl, user } = useConsole();
  const org = useOrganization();
  const canAdmin = useCan('admin');
  const navRef = React.useRef<HTMLElement>(null);
  const [bar, setBar] = React.useState<{ top: number; height: number } | null>(null);
  const [settled, setSettled] = React.useState(false);
  const groups = buildNav({
    projectId,
    organizationId: org?.id,
    apiUrl,
    canAdmin,
    operator: user.operator,
  });

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-measure when the route changes
  React.useLayoutEffect(() => {
    const nav = navRef.current;
    if (!nav) return;
    const measure = () => {
      const active = nav.querySelector<HTMLElement>('[aria-current="page"]');
      setBar(active ? { top: active.offsetTop + 7, height: active.offsetHeight - 14 } : null);
    };
    measure();
    const raf = requestAnimationFrame(() => setSettled(true));
    window.addEventListener('resize', measure);
    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener('resize', measure);
    };
  }, [pathname, projectId]);

  const itemClass = 'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors';

  return (
    <nav
      ref={navRef}
      className="scroll-slim relative flex flex-1 flex-col gap-6 overflow-y-auto px-3 py-3"
    >
      <span
        aria-hidden
        className={cn(
          'pointer-events-none absolute top-0 left-0 w-[3px] rounded-r-full bg-primary',
          settled &&
            'motion-safe:transition-[transform,height,opacity] motion-safe:duration-200 motion-safe:ease-out',
          bar ? 'opacity-100' : 'opacity-0',
        )}
        style={{ transform: `translateY(${bar?.top ?? 0}px)`, height: bar?.height ?? 0 }}
      />
      {groups.map((group) => (
        <div key={group.title} className="flex flex-col gap-1">
          <span className="px-3 pb-1.5 text-[0.62rem] font-semibold uppercase tracking-[0.14em] text-faint">
            {group.title}
          </span>
          {group.items.map((item) => {
            const icon = (
              <HugeiconsIcon icon={item.icon} strokeWidth={2} className="size-[1.15rem] shrink-0" />
            );
            if (item.soon) {
              return (
                <Tooltip key={item.href}>
                  <TooltipTrigger asChild>
                    <span aria-disabled className={cn(itemClass, 'cursor-default text-faint')}>
                      {icon}
                      <span className="flex-1 truncate">{item.label}</span>
                      <span className="rounded-full border px-1.5 text-[0.6rem] font-semibold uppercase tracking-wider">
                        Soon
                      </span>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent side="right">{item.soon}</TooltipContent>
                </Tooltip>
              );
            }
            if (item.external) {
              return (
                <a
                  key={item.href}
                  href={item.href}
                  target="_blank"
                  rel="noreferrer"
                  className={cn(itemClass, 'text-sidebar-foreground hover:bg-muted')}
                >
                  <span className="text-muted-foreground">{icon}</span>
                  <span className="flex-1 truncate">{item.label}</span>
                  <HugeiconsIcon
                    icon={ArrowUpRight01Icon}
                    strokeWidth={2}
                    className="size-3.5 text-faint"
                  />
                </a>
              );
            }
            const active = isActive(pathname, item);
            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={onNavigate}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  itemClass,
                  active
                    ? 'bg-sidebar-accent font-semibold text-sidebar-accent-foreground'
                    : 'text-sidebar-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                <span
                  className={cn(
                    'transition-colors',
                    active ? 'text-primary' : 'text-muted-foreground',
                  )}
                >
                  {icon}
                </span>
                <span className="flex-1 truncate">{item.label}</span>
              </Link>
            );
          })}
        </div>
      ))}
    </nav>
  );
}
