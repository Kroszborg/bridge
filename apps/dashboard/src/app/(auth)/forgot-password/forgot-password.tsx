'use client';

import { MailSend01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { type FormEvent, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { api, unwrap } from '@/lib/api';
import { AuthHeading, errorText, FormError, useAuthConfig } from '../auth-form';

export function ForgotPassword() {
  const params = useSearchParams();
  const config = useAuthConfig();
  const [email, setEmail] = useState(params.get('email') ?? '');
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      await unwrap(api.POST('/v1/auth/password-reset', { body: { email } }));
      setSentTo(email);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setPending(false);
    }
  }

  const back = (
    <p className="mt-6 text-center text-sm text-muted-foreground">
      Remembered it?{' '}
      <Link
        href={`/login${email ? `?email=${encodeURIComponent(email)}` : ''}`}
        className="font-medium text-primary underline-offset-4 hover:underline"
      >
        Back to sign in
      </Link>
    </p>
  );

  if (config.data && !config.data.password_reset) {
    return (
      <>
        <AuthHeading title="Reset your password">
          This Bridge server cannot send email, so passwords cannot be reset by link. Ask the
          server&apos;s administrator to reset it for you.
        </AuthHeading>
        {back}
      </>
    );
  }

  if (sentTo) {
    return (
      <>
        <span className="mb-5 flex size-11 items-center justify-center rounded-full bg-primary/12 text-primary">
          <HugeiconsIcon icon={MailSend01Icon} strokeWidth={2} className="size-5" />
        </span>
        <AuthHeading title="Check your email">
          If an account uses <span className="font-medium text-foreground">{sentTo}</span>, a link
          to choose a new password is on its way. It works once and expires in an hour. Check spam
          if it has not arrived in a few minutes.
        </AuthHeading>
        <Button variant="outline" size="lg" className="mt-8 w-full" onClick={() => setSentTo(null)}>
          Use a different email
        </Button>
        {back}
      </>
    );
  }

  return (
    <>
      <AuthHeading title="Reset your password">
        Enter the email you sign in with and we&apos;ll send a link to choose a new password.
      </AuthHeading>
      <form onSubmit={onSubmit} className="mt-8 flex flex-col gap-4">
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
            autoFocus
          />
        </div>
        {error ? <FormError>{error}</FormError> : null}
        <Button type="submit" size="lg" disabled={pending} className="mt-2 w-full">
          {pending ? 'Sending…' : 'Send reset link'}
        </Button>
      </form>
      {back}
    </>
  );
}
