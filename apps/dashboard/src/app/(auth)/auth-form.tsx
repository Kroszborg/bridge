'use client';

import type { AuthConfig } from '@bridge/api-types';
import { ArrowRight01Icon, ViewIcon, ViewOffSlashIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { type FormEvent, type KeyboardEvent, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { api, BridgeApiError, unwrap } from '@/lib/api';
import { cn } from '@/lib/utils';

type Mode = 'login' | 'signup';

/** Only same-origin relative paths are allowed as post-login destinations. */
export function safeNext(next: string | null): string {
  return next && /^\/(?!\/)[\w\-/]*$/.test(next) ? next : '/';
}

/** What this server offers on the sign-in pages. */
export function useAuthConfig() {
  return useQuery({
    queryKey: ['auth-config'],
    queryFn: async (): Promise<AuthConfig | null> => {
      const res = await api.GET('/v1/auth/config');
      return res.response.ok && res.data ? res.data : null;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
}

export function errorText(err: unknown): string {
  if (err instanceof BridgeApiError) return err.details?.[0]?.message ?? err.message;
  return 'Bridge could not be reached. Check your connection and try again.';
}

const MIN_PASSWORD = 10;

/**
 * A rough strength estimate to nudge people toward passphrases. The server
 * only requires 10 characters; this never blocks submitting.
 */
export function passwordStrength(pw: string): { score: 0 | 1 | 2 | 3 | 4; label: string } {
  if (pw.length === 0) return { score: 0, label: '' };
  if (pw.length < MIN_PASSWORD)
    return { score: 1, label: `${MIN_PASSWORD - pw.length} more characters` };
  const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((r) => r.test(pw)).length;
  const repetitive = /^(.)\1+$/.test(pw) || /^(?:0123|1234|abcd|qwer|pass)/i.test(pw);
  let score = 2 + (pw.length >= 14 ? 1 : 0) + (pw.length >= 20 || kinds >= 3 ? 1 : 0);
  if (repetitive) score = 2;
  const s = Math.min(score, 4) as 2 | 3 | 4;
  return { score: s, label: s === 2 ? 'Fair' : s === 3 ? 'Good' : 'Strong' };
}

export function PasswordField({
  id = 'password',
  label = 'Password',
  value,
  onChange,
  isNew,
  meter = isNew,
  aside,
}: {
  id?: string;
  label?: string;
  value: string;
  onChange: (v: string) => void;
  isNew: boolean;
  /** Show the strength meter; off for a confirmation field. */
  meter?: boolean;
  aside?: React.ReactNode;
}) {
  const [show, setShow] = useState(false);
  const [caps, setCaps] = useState(false);
  const strength = passwordStrength(value);
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => setCaps(e.getModifierState('CapsLock'));

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <Label htmlFor={id}>{label}</Label>
        {aside}
      </div>
      <div className="relative">
        <Input
          id={id}
          type={show ? 'text' : 'password'}
          autoComplete={isNew ? 'new-password' : 'current-password'}
          className="h-11 pr-10"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyUp={onKey}
          onKeyDown={onKey}
          onBlur={() => setCaps(false)}
          required
          minLength={isNew ? MIN_PASSWORD : 1}
          maxLength={128}
          aria-describedby={`${id}-hint`}
        />
        <button
          type="button"
          onClick={() => setShow((v) => !v)}
          aria-label={show ? 'Hide password' : 'Show password'}
          aria-pressed={show}
          className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
        >
          <HugeiconsIcon icon={show ? ViewOffSlashIcon : ViewIcon} size={18} strokeWidth={1.8} />
        </button>
      </div>
      <div id={`${id}-hint`} className="flex flex-col gap-1.5">
        {meter ? (
          <>
            <div className="grid grid-cols-4 gap-1" aria-hidden>
              {[1, 2, 3, 4].map((i) => (
                <span
                  key={i}
                  className={cn(
                    'h-1 rounded-full bg-muted transition-colors',
                    strength.score >= i &&
                      (strength.score === 1
                        ? 'bg-destructive'
                        : strength.score === 2
                          ? 'bg-warning'
                          : 'bg-success'),
                  )}
                />
              ))}
            </div>
            <p className="flex justify-between gap-3 text-xs text-muted-foreground">
              <span>At least {MIN_PASSWORD} characters. A few unrelated words work well.</span>
              {strength.label ? (
                <span className="shrink-0 font-medium text-foreground">{strength.label}</span>
              ) : null}
            </p>
          </>
        ) : null}
        {caps ? <p className="text-xs font-medium text-warning">Caps Lock is on.</p> : null}
      </div>
    </div>
  );
}

export function FormError({ children }: { children: React.ReactNode }) {
  return (
    <p
      role="alert"
      className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs/relaxed text-destructive"
    >
      {children}
    </p>
  );
}

export function AuthHeading({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <>
      <h2 className="font-display text-2xl/tight font-bold tracking-[-0.02em]">{title}</h2>
      {children ? <p className="mt-1 text-sm/relaxed text-muted-foreground">{children}</p> : null}
    </>
  );
}

const linkClass = 'font-medium text-primary underline-offset-4 hover:underline';

export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const params = useSearchParams();
  const config = useAuthConfig();
  const [name, setName] = useState('');
  const [email, setEmail] = useState(params.get('email') ?? '');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const signup = mode === 'signup';
  const invite = signup ? params.get('invite') : null;
  const next = params.get('next');

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      if (signup) {
        await unwrap(
          api.POST('/v1/auth/signup', {
            body: { email, password, name: name || undefined, invite_token: invite ?? undefined },
          }),
        );
      } else {
        await unwrap(api.POST('/v1/auth/login', { body: { email, password } }));
      }
      router.replace(safeNext(next));
      router.refresh();
    } catch (err) {
      setPending(false);
      if (err instanceof BridgeApiError && err.code === 'conflict' && signup) {
        setError(
          'An account with this email already exists. Sign in instead, or reset the password.',
        );
      } else {
        setError(errorText(err));
      }
    }
  }

  const cfg = config.data;
  const closed = signup && !invite && cfg?.signup_open === false;
  const otherHref = signup
    ? `/login${invite ? `?next=${encodeURIComponent(`/invite/${invite}`)}` : next ? `?next=${encodeURIComponent(next)}` : ''}`
    : `/signup${next ? `?next=${encodeURIComponent(next)}` : ''}`;

  if (closed) {
    return (
      <>
        <AuthHeading title="Sign-ups are closed">
          This Bridge server only accepts new accounts by invitation. Ask a workspace admin to send
          you an invite link.
        </AuthHeading>
        <Button asChild size="lg" variant="outline" className="mt-8 w-full">
          <Link href="/login">Back to sign in</Link>
        </Button>
      </>
    );
  }

  return (
    <>
      <AuthHeading title={signup ? 'Create your account' : 'Sign in to Bridge'}>
        {invite
          ? 'Create an account to accept your invite. You join the team right away.'
          : signup
            ? cfg?.hosted
              ? 'Start free: pair a phone and send. Test messages are always unlimited.'
              : 'You get a workspace and a default project right away.'
            : 'Welcome back.'}
      </AuthHeading>

      <form onSubmit={onSubmit} className="mt-8 flex flex-col gap-4">
        {signup ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="name">
              Name <span className="font-normal text-muted-foreground">(optional)</span>
            </Label>
            <Input
              id="name"
              className="h-11"
              autoComplete="name"
              placeholder="Ada Lovelace"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
            />
          </div>
        ) : null}
        <div className="flex flex-col gap-2">
          <Label htmlFor="email">{signup ? 'Work email' : 'Email'}</Label>
          <Input
            id="email"
            type="email"
            className="h-11"
            autoComplete={signup ? 'email' : 'username'}
            placeholder="you@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            maxLength={254}
            autoFocus={!signup}
          />
        </div>
        <PasswordField
          value={password}
          onChange={setPassword}
          isNew={signup}
          aside={
            !signup && cfg?.password_reset ? (
              <Link
                href={`/forgot-password${email ? `?email=${encodeURIComponent(email)}` : ''}`}
                className="text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
              >
                Forgot password?
              </Link>
            ) : undefined
          }
        />

        {error ? <FormError>{error}</FormError> : null}

        <Button type="submit" size="lg" disabled={pending} className="group mt-2 h-11 w-full">
          {pending
            ? signup
              ? 'Creating account…'
              : 'Signing in…'
            : signup
              ? 'Create account'
              : 'Sign in'}
          {pending ? null : (
            <HugeiconsIcon
              icon={ArrowRight01Icon}
              strokeWidth={2}
              className="size-4 transition-transform group-hover:translate-x-0.5"
            />
          )}
        </Button>

        {signup && cfg?.terms_url && cfg.privacy_url ? (
          <p className="text-center text-xs/relaxed text-muted-foreground">
            By creating an account you agree to the{' '}
            <a href={cfg.terms_url} target="_blank" rel="noreferrer" className={linkClass}>
              Terms
            </a>{' '}
            and{' '}
            <a href={cfg.privacy_url} target="_blank" rel="noreferrer" className={linkClass}>
              Privacy Policy
            </a>
            .
          </p>
        ) : null}
      </form>

      {signup || cfg?.signup_open !== false ? (
        <p className="mt-6 text-center text-sm text-muted-foreground">
          {signup ? 'Already have an account? ' : 'New to Bridge? '}
          <Link href={otherHref} className={linkClass}>
            {signup ? 'Sign in' : 'Create an account'}
          </Link>
        </p>
      ) : null}
    </>
  );
}
