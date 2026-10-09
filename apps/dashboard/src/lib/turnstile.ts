'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

type TurnstileApi = {
  render: (el: HTMLElement, options: Record<string, unknown>) => string;
  reset: (id: string) => void;
  remove: (id: string) => void;
};

declare global {
  interface Window {
    turnstile?: TurnstileApi;
  }
}

const SCRIPT = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
let loading: Promise<TurnstileApi> | null = null;

function loadTurnstile(): Promise<TurnstileApi> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  loading ??= new Promise<TurnstileApi>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = SCRIPT;
    s.async = true;
    s.onload = () =>
      window.turnstile ? resolve(window.turnstile) : reject(new Error('turnstile'));
    s.onerror = () => {
      loading = null;
      reject(new Error('turnstile'));
    };
    document.head.appendChild(s);
  });
  return loading;
}

export type TurnstileOptions = {
  /** Checked by the server, so a token made for one form cannot be used on another. */
  action?: string;
  theme?: 'light' | 'dark' | 'auto';
};

/**
 * Cloudflare Turnstile, rendered explicitly into a container and shown only
 * when it needs the person to interact. Each token is used once: getToken
 * hands out the current token (waiting for one if needed) and starts the next.
 */
export function useTurnstile(
  siteKey: string | null | undefined,
  { action, theme = 'auto' }: TurnstileOptions = {},
) {
  const container = useRef<HTMLDivElement>(null);
  const widget = useRef<string | null>(null);
  const token = useRef<string | null>(null);
  const waiters = useRef<((t: string) => void)[]>([]);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!siteKey) return;
    let cancelled = false;
    loadTurnstile()
      .then((ts) => {
        if (cancelled || !container.current) return;
        widget.current = ts.render(container.current, {
          sitekey: siteKey,
          appearance: 'interaction-only',
          theme,
          size: 'flexible',
          ...(action ? { action } : {}),
          callback: (t: string) => {
            setFailed(false);
            const waiting = waiters.current.shift();
            if (waiting) waiting(t);
            else token.current = t;
          },
          'expired-callback': () => {
            token.current = null;
          },
          'error-callback': () => {
            setFailed(true);
            return true; // handled: no console error from Turnstile
          },
        });
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
      if (widget.current) window.turnstile?.remove(widget.current);
      widget.current = null;
    };
  }, [siteKey, action, theme]);

  const getToken = useCallback((): Promise<string> => {
    if (!siteKey) return Promise.resolve('');
    const ready = token.current;
    if (ready) {
      token.current = null;
      return Promise.resolve(ready);
    }
    return new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => {
        waiters.current = waiters.current.filter((w) => w !== done);
        reject(new Error('turnstile_timeout'));
      }, 60_000);
      const done = (t: string) => {
        clearTimeout(timer);
        resolve(t);
      };
      waiters.current.push(done);
    });
  }, [siteKey]);

  /** Starts a fresh challenge after a token was used. */
  const next = useCallback(() => {
    token.current = null;
    if (widget.current) window.turnstile?.reset(widget.current);
  }, []);

  return { container, getToken, next, failed, enabled: Boolean(siteKey) };
}
