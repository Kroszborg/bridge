'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { type FormEvent, useState } from 'react';
import { Button } from '@/components/ui/button';
import { api, BridgeApiError, unwrap } from '@/lib/api';
import { AuthHeading, errorText, FormError, PasswordField } from '../auth-form';

export function ResetPassword() {
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get('token') ?? '';
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [expired, setExpired] = useState(false);
  const [pending, setPending] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError('The two passwords do not match.');
      return;
    }
    setPending(true);
    setError(null);
    try {
      await unwrap(api.POST('/v1/auth/password-reset/confirm', { body: { token, password } }));
      router.replace('/');
      router.refresh();
    } catch (err) {
      setPending(false);
      if (err instanceof BridgeApiError && err.status === 410) setExpired(true);
      else setError(errorText(err));
    }
  }

  if (!token || expired) {
    return (
      <>
        <AuthHeading title="This link no longer works">
          Reset links work once and expire after an hour. Request a new one and use the latest
          email.
        </AuthHeading>
        <Button asChild size="lg" className="mt-8 w-full">
          <Link href="/forgot-password">Request a new link</Link>
        </Button>
      </>
    );
  }

  return (
    <>
      <AuthHeading title="Choose a new password">
        You&apos;ll be signed in, and signed out everywhere else.
      </AuthHeading>
      <form onSubmit={onSubmit} className="mt-8 flex flex-col gap-4">
        <PasswordField label="New password" value={password} onChange={setPassword} isNew />
        <PasswordField
          id="confirm"
          label="Confirm new password"
          value={confirm}
          onChange={setConfirm}
          isNew
          meter={false}
        />
        {error ? <FormError>{error}</FormError> : null}
        <Button type="submit" size="lg" disabled={pending} className="mt-2 w-full">
          {pending ? 'Saving…' : 'Save and sign in'}
        </Button>
      </form>
    </>
  );
}
