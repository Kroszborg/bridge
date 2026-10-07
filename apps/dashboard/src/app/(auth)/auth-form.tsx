'use client';

import { ViewIcon, ViewOffSlashIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { type FormEvent, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { api, BridgeApiError, unwrap } from '@/lib/api';

type Mode = 'login' | 'signup';

/** Only same-origin relative paths are allowed as post-login destinations. */
function safeNext(next: string | null): string {
  return next && /^\/(?!\/)[\w\-/]*$/.test(next) ? next : '/';
}

export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const params = useSearchParams();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const invite = mode === 'signup' ? params.get('invite') : null;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      if (mode === 'signup') {
        await unwrap(
          api.POST('/v1/auth/signup', {
            body: { email, password, name: name || undefined, invite_token: invite ?? undefined },
          }),
        );
      } else {
        await unwrap(api.POST('/v1/auth/login', { body: { email, password } }));
      }
      router.replace(safeNext(params.get('next')));
      router.refresh();
    } catch (err) {
      setPending(false);
      if (err instanceof BridgeApiError) {
        setError(err.details?.[0]?.message ?? err.message);
      } else {
        setError('Something went wrong. Try again.');
      }
    }
  }

  const signup = mode === 'signup';
  return (
    <>
      <h2 className="font-display text-2xl font-semibold tracking-tight">
        {signup ? 'Create your account' : 'Sign in'}
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {invite
          ? 'Create an account to accept your invite. You join the team right away.'
          : signup
            ? 'You get a workspace and a default project right away.'
            : 'Welcome back. Sign in to your Bridge console.'}
      </p>

      <form onSubmit={onSubmit} className="mt-8 flex flex-col gap-4" noValidate={false}>
        {signup ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="name">Name</Label>
            <Input
              id="name"
              autoComplete="name"
              placeholder="Ada Lovelace"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
            />
          </div>
        ) : null}
        <div className="flex flex-col gap-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            placeholder="you@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            maxLength={254}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password">Password</Label>
          <div className="relative">
            <Input
              id="password"
              type={showPassword ? 'text' : 'password'}
              autoComplete={signup ? 'new-password' : 'current-password'}
              className="pr-10"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={signup ? 10 : 1}
              maxLength={128}
              aria-describedby={signup ? 'password-hint' : undefined}
            />
            <button
              type="button"
              onClick={() => setShowPassword((v) => !v)}
              aria-label={showPassword ? 'Hide password' : 'Show password'}
              aria-pressed={showPassword}
              className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
            >
              <HugeiconsIcon
                icon={showPassword ? ViewOffSlashIcon : ViewIcon}
                size={18}
                strokeWidth={1.8}
              />
            </button>
          </div>
          {signup ? (
            <p id="password-hint" className="text-xs text-muted-foreground">
              At least 10 characters. A passphrase works well.
            </p>
          ) : null}
        </div>

        {error ? (
          <p
            role="alert"
            className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            {error}
          </p>
        ) : null}

        <Button type="submit" size="lg" disabled={pending} className="mt-2 w-full">
          {pending
            ? signup
              ? 'Creating account…'
              : 'Signing in…'
            : signup
              ? 'Create account'
              : 'Sign in'}
        </Button>
      </form>

      <p className="mt-6 text-center text-sm text-muted-foreground">
        {signup ? 'Already have an account? ' : 'New to Bridge? '}
        <Link
          href={
            signup
              ? invite
                ? `/login?next=${encodeURIComponent(`/invite/${invite}`)}`
                : '/login'
              : '/signup'
          }
          className="font-medium text-primary underline-offset-4 hover:underline"
        >
          {signup ? 'Sign in' : 'Create an account'}
        </Link>
      </p>
    </>
  );
}
