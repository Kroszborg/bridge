'use client';

import type { WidgetConfig, WidgetSendResult } from '@bridge/api-types';
import { Alert02Icon, CheckmarkCircle02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useEffect, useId, useRef, useState } from 'react';
import { BridgeMark } from '@/components/brand';
import { ThemeToggle } from '@/components/theme-toggle';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { COUNTRIES, countryByCode, defaultCountry, splitE164, toE164 } from '@/lib/countries';
import { useTurnstile } from '@/lib/turnstile';
import { cn } from '@/lib/utils';
import { errorText, WidgetError, waitText, widgetApi } from '@/lib/widget-api';

type Phase =
  | { kind: 'loading' }
  | { kind: 'fatal'; title: string; message: string }
  | { kind: 'phone' }
  | { kind: 'code'; phone: string; sent: WidgetSendResult }
  | { kind: 'done'; host: string };

/** redirect_uri with params added, keeping its own query string. */
function returnUrl(redirectUri: string, params: Record<string, string | null>): string {
  const url = new URL(redirectUri);
  for (const [k, v] of Object.entries(params)) {
    if (v !== null) url.searchParams.set(k, v);
  }
  return url.toString();
}

const KEY_PATTERN = /^bpk_[0-9A-Za-z]{32}$/;

export function HostedVerify({
  publishableKey,
  redirectUri,
  state,
}: {
  publishableKey: string;
  redirectUri: string | null;
  state: string | null;
}) {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' });
  const [config, setConfig] = useState<WidgetConfig | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      if (!KEY_PATTERN.test(publishableKey)) {
        return setPhase({
          kind: 'fatal',
          title: 'This verification link is not valid',
          message: 'Go back to the site that sent you here and try again.',
        });
      }
      if (!redirectUri) {
        return setPhase({
          kind: 'fatal',
          title: 'This link is missing its return address',
          message:
            'The site that sent you here did not say where to return to. Go back and try again.',
        });
      }
      try {
        const [cfg] = await Promise.all([
          widgetApi.config(publishableKey),
          widgetApi.checkRedirect(publishableKey, redirectUri),
        ]);
        if (cancelled) return;
        setConfig(cfg);
        setPhase({ kind: 'phone' });
      } catch (err) {
        if (cancelled) return;
        const e = err instanceof WidgetError ? err : null;
        if (e?.status === 400) {
          setPhase({
            kind: 'fatal',
            title: 'This site is not set up to return here',
            message:
              'The return address in this link is not registered for the app, so this page will not send you there. If you run the site, add it under Redirect URIs in the Bridge dashboard.',
          });
        } else if (e?.status === 404) {
          setPhase({
            kind: 'fatal',
            title: 'This verification link is not valid',
            message: 'The app it points to does not exist. Go back and try again.',
          });
        } else if (e?.status === 403) {
          setPhase({
            kind: 'fatal',
            title: 'This page is not set up correctly',
            message:
              'The verification service does not recognise this page. If you run Bridge, check that BRIDGE_DASHBOARD_URL is this page’s address.',
          });
        } else {
          setPhase({
            kind: 'fatal',
            title: 'Verification is not available right now',
            message: e ? errorText(e, 'send') : 'Something went wrong. Try again in a moment.',
          });
        }
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [publishableKey, redirectUri]);

  const checked = config !== null && redirectUri !== null;
  const cancelHref = checked ? returnUrl(redirectUri, { error: 'cancelled', state }) : null;

  function finish(token: string) {
    if (!redirectUri) return;
    setPhase({ kind: 'done', host: new URL(redirectUri).host });
    window.location.replace(returnUrl(redirectUri, { bridge_token: token, state }));
  }

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="flex w-full items-center justify-end px-3 pt-3">
        <ThemeToggle />
      </header>
      <main className="flex flex-1 justify-center px-4 pt-2 pb-10 sm:items-center sm:pt-0">
        <div className="w-full max-w-sm">
          <section
            aria-labelledby="verify-title"
            className="rounded-2xl border bg-card p-6 text-card-foreground shadow-sm sm:p-8"
          >
            <Brand config={config} loading={phase.kind === 'loading'} />
            {config?.environment === 'test' && phase.kind !== 'fatal' ? (
              <p className="mb-5 rounded-lg border border-warning/40 bg-warning/8 px-3 py-2 text-xs/relaxed">
                <span className="font-semibold text-warning">Test mode.</span> No SMS is sent. The
                code is shown on the next step.
              </p>
            ) : null}
            {phase.kind === 'loading' ? (
              <div className="flex flex-col gap-3" aria-busy="true" aria-live="polite">
                <span className="sr-only">Loading</span>
                <Skeleton className="h-11 w-full" />
                <Skeleton className="h-11 w-full" />
              </div>
            ) : phase.kind === 'fatal' ? (
              <Fatal title={phase.title} message={phase.message} />
            ) : phase.kind === 'done' ? (
              <div role="status" className="flex flex-col items-center gap-3 py-4 text-center">
                <HugeiconsIcon
                  icon={CheckmarkCircle02Icon}
                  strokeWidth={1.8}
                  className="size-10 text-success"
                />
                <p className="font-display text-lg font-semibold">Phone number verified</p>
                <p className="text-sm text-muted-foreground">Returning you to {phase.host}…</p>
              </div>
            ) : config ? (
              <Steps
                publishableKey={publishableKey}
                config={config}
                phase={phase}
                setPhase={setPhase}
                onVerified={finish}
              />
            ) : null}
          </section>
          <footer className="mt-5 flex items-center justify-between gap-4 px-1 text-xs text-muted-foreground">
            {cancelHref && phase.kind !== 'done' ? (
              <a
                href={cancelHref}
                className="rounded underline-offset-4 hover:text-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none"
              >
                Cancel
              </a>
            ) : (
              <span />
            )}
            <span className="inline-flex items-center gap-1.5">
              <BridgeMark className="size-3.5" />
              Secured by Bridge
            </span>
          </footer>
        </div>
      </main>
    </div>
  );
}

function Brand({ config, loading }: { config: WidgetConfig | null; loading: boolean }) {
  const name = config?.app_name ?? '';
  return (
    <div className="mb-6 flex flex-col gap-4">
      {loading ? (
        <Skeleton className="size-11 rounded-xl" />
      ) : name ? (
        <span
          aria-hidden
          className="grid size-11 place-items-center rounded-xl bg-primary/12 font-display text-lg font-bold text-primary"
        >
          {name.trim().charAt(0).toUpperCase()}
        </span>
      ) : null}
      <div className="flex flex-col gap-1">
        {name ? (
          <p className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
            {name}
          </p>
        ) : null}
        <h1 id="verify-title" className="font-display text-2xl font-semibold tracking-tight">
          Verify your phone
        </h1>
      </div>
    </div>
  );
}

function Fatal({ title, message }: { title: string; message: string }) {
  return (
    <div role="alert" className="flex flex-col gap-2">
      <p className="flex items-center gap-2 font-display text-base font-semibold">
        <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} className="size-4 text-destructive" />
        {title}
      </p>
      <p className="text-sm/relaxed text-muted-foreground">{message}</p>
    </div>
  );
}

function Steps({
  publishableKey,
  config,
  phase,
  setPhase,
  onVerified,
}: {
  publishableKey: string;
  config: WidgetConfig;
  phase: Extract<Phase, { kind: 'phone' | 'code' }>;
  setPhase: (p: Phase) => void;
  onVerified: (token: string) => void;
}) {
  const turnstile = useTurnstile(config.turnstile_site_key);
  const [country, setCountry] = useState('US');
  const [national, setNational] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState<'' | 'checking' | 'sending'>('');

  // The browser's region is only known on the client.
  useEffect(() => setCountry(defaultCountry()), []);

  async function send(to: string): Promise<boolean> {
    setError('');
    try {
      setBusy(turnstile.enabled ? 'checking' : 'sending');
      const token = await turnstile.getToken();
      setBusy('sending');
      const sent = await widgetApi.send(publishableKey, to, token);
      setPhase({ kind: 'code', phone: to, sent });
      return true;
    } catch (err) {
      setError(
        err instanceof Error && err.message === 'turnstile_timeout'
          ? 'The security check did not finish. Try again.'
          : errorText(err, 'send'),
      );
      return false;
    } finally {
      setBusy('');
      turnstile.next();
    }
  }

  return (
    <div className="flex flex-col gap-5">
      {phase.kind === 'phone' ? (
        <PhoneStep
          country={country}
          setCountry={setCountry}
          national={national}
          setNational={setNational}
          busy={busy}
          error={error}
          setError={setError}
          onSubmit={send}
        />
      ) : (
        <CodeStep
          key={phase.sent.verification_id}
          publishableKey={publishableKey}
          config={config}
          phone={phase.phone}
          sent={phase.sent}
          resend={() => send(phase.phone)}
          resendBusy={busy !== ''}
          resendError={error}
          onChangeNumber={() => {
            setError('');
            setPhase({ kind: 'phone' });
          }}
          onVerified={onVerified}
        />
      )}
      {turnstile.enabled ? (
        <div className="flex flex-col gap-2">
          <div ref={turnstile.container} className="empty:hidden" />
          {turnstile.failed ? (
            <p role="alert" className="text-xs text-destructive">
              The security check could not load. Check your connection, or disable content blockers
              for this page, then reload.
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

const big = 'h-11 text-base md:text-base px-3';

function PhoneStep({
  country,
  setCountry,
  national,
  setNational,
  busy,
  error,
  setError,
  onSubmit,
}: {
  country: string;
  setCountry: (c: string) => void;
  national: string;
  setNational: (v: string) => void;
  busy: '' | 'checking' | 'sending';
  error: string;
  setError: (e: string) => void;
  onSubmit: (e164: string) => Promise<boolean>;
}) {
  const id = useId();
  const c = countryByCode(country);

  function submit(e: FormEvent) {
    e.preventDefault();
    const e164 = toE164(national, country);
    if (!e164) {
      setError('Enter a valid phone number, including the area code.');
      return;
    }
    onSubmit(e164);
  }

  function onNumber(value: string) {
    setError('');
    // A pasted or autofilled international number picks its own country.
    if (value.trim().startsWith('+')) {
      const split = splitE164(value, country);
      if (split && split.national.length >= 4) {
        setCountry(split.country);
        setNational(split.national);
        return;
      }
    }
    setNational(value);
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
      <p className="text-sm/relaxed text-muted-foreground">
        We will text a code to this number to confirm it is yours.
      </p>
      <div className="flex flex-col gap-2">
        <Label htmlFor={`${id}-number`}>Phone number</Label>
        <div className="flex gap-2">
          <div className="relative shrink-0 rounded-md has-focus-visible:ring-2 has-focus-visible:ring-ring/30">
            <div
              aria-hidden
              className="flex h-11 items-center gap-1.5 rounded-md border border-input bg-input/20 px-3 font-mono text-sm dark:bg-input/30"
            >
              <span>{country}</span>
              <span className="text-muted-foreground">+{c?.dial}</span>
              <svg viewBox="0 0 12 12" className="size-3 text-muted-foreground" aria-hidden>
                <path d="M3 4.5 6 7.5 9 4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            </div>
            <select
              aria-label="Country"
              value={country}
              onChange={(e) => {
                setCountry(e.target.value);
                setError('');
              }}
              className="absolute inset-0 cursor-pointer opacity-0"
            >
              {COUNTRIES.map((x) => (
                <option key={x.code} value={x.code}>
                  {x.name} (+{x.dial})
                </option>
              ))}
            </select>
          </div>
          <Input
            id={`${id}-number`}
            type="tel"
            inputMode="tel"
            autoComplete="tel"
            autoFocus
            value={national}
            onChange={(e) => onNumber(e.target.value)}
            placeholder={country === 'IN' ? '98765 43210' : 'Phone number'}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? `${id}-error` : undefined}
            className={cn(big, 'font-mono placeholder:font-sans')}
          />
        </div>
      </div>
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
      <Button type="submit" size="lg" className="h-11 w-full text-sm" disabled={busy !== ''}>
        {busy === 'checking'
          ? 'Checking your browser…'
          : busy === 'sending'
            ? 'Sending code…'
            : 'Send code'}
      </Button>
    </form>
  );
}

function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return now;
}

function CodeStep({
  publishableKey,
  config,
  phone,
  sent,
  resend,
  resendBusy,
  resendError,
  onChangeNumber,
  onVerified,
}: {
  publishableKey: string;
  config: WidgetConfig;
  phone: string;
  sent: WidgetSendResult;
  resend: () => Promise<boolean>;
  resendBusy: boolean;
  resendError: string;
  onChangeNumber: () => void;
  onVerified: (token: string) => void;
}) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [checking, setChecking] = useState(false);
  const [dead, setDead] = useState(false);
  const lastTried = useRef('');
  const resendAt = new Date(sent.resend_available_at).getTime();
  const now = useNow(true);
  const wait = Math.max(0, Math.ceil((resendAt - now) / 1000));
  const length = config.code_length;

  useEffect(() => input.current?.focus(), []);

  async function check(value: string) {
    if (checking || value.length !== length) return;
    lastTried.current = value;
    setChecking(true);
    setError('');
    try {
      const r = await widgetApi.verify(publishableKey, sent.verification_id, value);
      if (r.valid && r.token) return onVerified(r.token);
      if (r.status === 'pending') {
        setError(
          `That code is not right. ${r.attempts_remaining} attempt${r.attempts_remaining === 1 ? '' : 's'} left.`,
        );
        setCode('');
        input.current?.focus();
      } else {
        setDead(true);
        setError(
          r.status === 'expired'
            ? 'This code has expired. Send a new one.'
            : r.status === 'failed'
              ? 'Too many wrong codes. Send a new one.'
              : 'A newer code was sent. Use the latest one, or send a new one.',
        );
      }
    } catch (err) {
      setError(errorText(err, 'verify'));
    } finally {
      setChecking(false);
    }
  }

  function onInput(v: string) {
    const digits = v.replace(/\D/g, '').slice(0, length);
    setCode(digits);
    setError('');
    if (digits.length === length && digits !== lastTried.current && !dead) check(digits);
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm/relaxed text-muted-foreground">
        Enter the {length}-digit code sent to{' '}
        <span className="font-mono whitespace-nowrap text-foreground">{phone}</span>.{' '}
        <button
          type="button"
          onClick={onChangeNumber}
          className="rounded font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none"
        >
          Change number
        </button>
      </p>
      {sent.code ? (
        <p className="rounded-lg border border-warning/40 bg-warning/8 px-3 py-2 text-sm">
          Test code{' '}
          <span className="ml-1 font-mono font-semibold tracking-[0.2em]">{sent.code}</span>
        </p>
      ) : null}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          lastTried.current = '';
          check(code);
        }}
        className="flex flex-col gap-4"
        noValidate
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${id}-code`}>Code</Label>
          <Input
            ref={input}
            id={`${id}-code`}
            value={code}
            onChange={(e) => onInput(e.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern={`\\d{${length}}`}
            maxLength={length}
            placeholder={'•'.repeat(length)}
            disabled={dead}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? `${id}-error` : undefined}
            className={cn(big, 'h-12 text-center font-mono text-xl tracking-[0.4em] md:text-xl')}
          />
        </div>
        {error || resendError ? (
          <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
            {error || resendError}
          </p>
        ) : null}
        <Button
          type="submit"
          size="lg"
          className="h-11 w-full text-sm"
          disabled={checking || dead || code.length !== length}
        >
          {checking ? 'Checking…' : 'Verify'}
        </Button>
      </form>
      <div className="flex items-center justify-center text-sm">
        {wait > 0 ? (
          <span className="text-muted-foreground" aria-live="off">
            Send a new code in {waitText(wait)}
          </span>
        ) : (
          <button
            type="button"
            onClick={() => resend()}
            disabled={resendBusy}
            className="rounded font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none disabled:opacity-50"
          >
            {resendBusy ? 'Sending…' : 'Send a new code'}
          </button>
        )}
      </div>
    </div>
  );
}
