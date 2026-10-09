'use client';

import { animate, createScope, onScroll, type Scope } from 'animejs';
import { type CSSProperties, type ReactNode, useEffect, useRef, useState } from 'react';

/*
 * The site's small effects. Text reveals are CSS, so the copy is readable
 * before (and without) JavaScript; scroll and pointer effects use anime.js.
 * Everything settles to a still page under prefers-reduced-motion.
 */

/** The hero headline: words arrive one by one out of a soft blur (CSS). */
export function HeadlineReveal({ lines }: { lines: { text: string; className?: string }[] }) {
  let order = 0;
  const words = lines.map((line) => {
    const parts = line.text.split(' ');
    return parts.map((word, n) => ({
      id: `${line.text}#${n}`,
      word,
      last: n === parts.length - 1,
      i: order++,
    }));
  });
  return (
    <>
      {lines.map((line, li) => (
        <span key={line.text} className={`block ${line.className ?? ''}`}>
          {words[li]?.map((w) => (
            <span key={w.id} className="inline-block whitespace-pre">
              <span className="word-in inline-block" style={{ '--i': w.i } as CSSProperties}>
                {w.word}
              </span>
              {w.last ? '' : ' '}
            </span>
          ))}
        </span>
      ))}
    </>
  );
}

/** Content that rises in after the headline (CSS). `delay` is in seconds. */
export function FadeUp({
  children,
  delay = 0,
  className,
}: {
  children: ReactNode;
  delay?: number;
  className?: string;
}) {
  return (
    <div className={`fade-up ${className ?? ''}`} style={{ '--d': `${delay}s` } as CSSProperties}>
      {children}
    </div>
  );
}

/**
 * A screenshot that starts tilted back and slightly smaller, and settles flat
 * as it scrolls into view, in step with the scrollbar.
 */
export function RisingShot({ children, className }: { children: ReactNode; className?: string }) {
  const root = useRef<HTMLDivElement>(null);
  const scope = useRef<Scope | null>(null);
  useEffect(() => {
    const el = root.current;
    if (!el || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    scope.current = createScope({ root }).add(() => {
      animate('.rising', {
        rotateX: [18, 0],
        scale: [0.9, 1],
        translateY: [40, 0],
        ease: 'outQuad',
        autoplay: onScroll({ target: el, enter: 'bottom top', leave: '75% top', sync: 0.25 }),
      });
    });
    return () => scope.current?.revert();
  }, []);
  return (
    <div ref={root} className={className} style={{ perspective: 1400 }}>
      <div className="rising" style={{ transformOrigin: 'center top' }}>
        {children}
      </div>
    </div>
  );
}

/** A slow, endless row that pauses on hover (CSS). Static when motion is reduced. */
export function Marquee({ children }: { children: ReactNode }) {
  return (
    <div className="marquee group relative overflow-hidden">
      <div className="marquee-track flex w-max gap-12 group-hover:[animation-play-state:paused]">
        <div className="flex shrink-0 items-center gap-12">{children}</div>
        <div className="marquee-copy flex shrink-0 items-center gap-12" aria-hidden>
          {children}
        </div>
      </div>
    </div>
  );
}

/** A card whose surface lights up under the cursor. */
export function SpotlightCard({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`spotlight relative overflow-hidden ${className ?? ''}`}
      onPointerMove={(e) => {
        const r = e.currentTarget.getBoundingClientRect();
        e.currentTarget.style.setProperty('--mx', `${e.clientX - r.left}px`);
        e.currentTarget.style.setProperty('--my', `${e.clientY - r.top}px`);
      }}
    >
      <div className="relative flex h-full flex-col">{children}</div>
    </div>
  );
}

/** The header's menu on small screens. */
export function MobileMenu({
  links,
  signIn,
}: {
  links: readonly (readonly [string, string])[];
  signIn: { href: string; label: string };
}) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return;
    const close = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    window.addEventListener('keydown', close);
    return () => window.removeEventListener('keydown', close);
  }, [open]);
  return (
    <div className="lg:hidden">
      <button
        type="button"
        aria-label={open ? 'Close menu' : 'Open menu'}
        aria-expanded={open}
        aria-controls="mobile-menu"
        onClick={() => setOpen((v) => !v)}
        className="grid size-9 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        <span className="relative block h-3 w-4">
          <span
            className={`absolute left-0 h-0.5 w-4 rounded bg-current transition-transform duration-300 ${open ? 'top-1.5 rotate-45' : 'top-0'}`}
          />
          <span
            className={`absolute left-0 h-0.5 w-4 rounded bg-current transition-transform duration-300 ${open ? 'top-1.5 -rotate-45' : 'top-[0.6875rem]'}`}
          />
        </span>
      </button>
      <nav
        id="mobile-menu"
        aria-label="Menu"
        data-open={open}
        className="mobile-menu absolute inset-x-0 top-16 border-b bg-background/95 px-4 pb-5 backdrop-blur-md sm:px-6"
      >
        <ul className="flex flex-col py-2">
          {links.map(([href, label]) => (
            <li key={href}>
              <a
                href={href}
                tabIndex={open ? 0 : -1}
                onClick={() => setOpen(false)}
                className="block border-b py-3.5 text-base font-medium"
              >
                {label}
              </a>
            </li>
          ))}
        </ul>
        <a
          href={signIn.href}
          tabIndex={open ? 0 : -1}
          className="mt-3 flex h-11 items-center justify-center rounded-xl bg-primary text-sm font-semibold text-primary-foreground"
        >
          {signIn.label}
        </a>
      </nav>
    </div>
  );
}
