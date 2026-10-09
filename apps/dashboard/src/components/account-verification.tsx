'use client';

import type { PhoneVerificationSent } from '@bridge/api-types';
import { Cancel01Icon, Mail01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useRouter } from 'next/navigation';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { useAuthConfig } from '@/app/(auth)/auth-form';
import { StatusBadge } from '@/components/kit/status-badge';
import { useConsole } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { BridgeApiError } from '@/lib/api';
import { useVerificationMutations } from '@/lib/queries';
import { cn } from '@/lib/utils';

export { useAuthConfig };

function messageOf(err: unknown): string {
  if (err instanceof BridgeApiError) return err.details?.[0]?.message ?? err.message;
  return 'Bridge could not be reached. Check your connection and try again.';
}

/** Seconds to wait from a 429, whose message ends "Retry after N seconds." */
function retryAfterSeconds(err: unknown): number | null {
  if (!(err instanceof BridgeApiError) || err.status !== 429) return null;
  const m = /Retry after (\d+) seconds/.exec(err.message);
  return m ? Number(m[1]) : null;
}

/** Whole seconds left until `until` (epoch ms), ticking every second. */
function useCountdown(until: number | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (until === null) return;
    setNow(Date.now());
    const timer = setInterval(() => {
      const t = Date.now();
      setNow(t);
      if (t >= until) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [until]);
  return until === null ? 0 : Math.max(0, Math.ceil((until - now) / 1000));
}

export function VerifiedBadge() {
  return <StatusBadge kind="success">Verified</StatusBadge>;
}

function CodeField({
  id,
  value,
  onChange,
  length,
  invalid,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  length: number;
  invalid: boolean;
}) {
  return (
    <Input
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, length))}
      inputMode="numeric"
      autoComplete="one-time-code"
      pattern="[0-9]*"
      maxLength={length}
      placeholder={'0'.repeat(Math.min(length, 6))}
      aria-invalid={invalid}
      autoFocus
      className="h-10 text-center font-mono text-lg tracking-[0.4em] placeholder:text-muted-foreground/40 md:text-lg"
      required
    />
  );
}

function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-md bg-destructive/8 px-3 py-2 text-xs text-destructive">
      {message}
    </p>
  );
}

function ResendButton({
  wait,
  pending,
  label,
  onClick,
}: {
  wait: number;
  pending: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      className="mr-auto text-muted-foreground"
      disabled={pending || wait > 0}
      onClick={onClick}
    >
      {pending ? 'Sending…' : wait > 0 ? `${label} in ${wait}s` : label}
    </Button>
  );
}

const DIALOG_CLASS = 'w-[calc(100%-2rem)] max-w-md';

/** Enter the emailed code; can ask for a new one. Sign-up already sent the first code. */
export function EmailVerifyDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { user } = useConsole();
  const router = useRouter();
  const { sendEmailCode, confirmEmail } = useVerificationMutations();
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [resendAt, setResendAt] = useState<number | null>(null);
  const wait = useCountdown(resendAt);
  // First a send step (an account may never have been sent a code), unless a
  // code was already requested in this dialog or the user says they have one.
  const [stage, setStage] = useState<'send' | 'code'>('send');

  // biome-ignore lint/correctness/useExhaustiveDependencies: reset only when the dialog opens
  useEffect(() => {
    if (!open) return;
    setCode('');
    setError(null);
    setNote(null);
    setStage(resendAt !== null ? 'code' : 'send');
  }, [open]);

  async function send() {
    setError(null);
    setNote(null);
    try {
      const sent = await sendEmailCode.mutateAsync();
      setResendAt(Date.parse(sent.resend_available_at));
      setCode('');
      setStage('code');
      setNote(`A code is on its way to ${sent.email}.`);
    } catch (err) {
      const retry = retryAfterSeconds(err);
      if (retry !== null) setResendAt(Date.now() + retry * 1000);
      setError(messageOf(err));
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await confirmEmail.mutateAsync(code);
      toast.success('Email address verified');
      onOpenChange(false);
      router.refresh();
    } catch (err) {
      if (err instanceof BridgeApiError && err.code === 'already_verified') {
        onOpenChange(false);
        router.refresh();
        return;
      }
      setError(messageOf(err));
      if (err instanceof BridgeApiError && err.code === 'code_expired') setCode('');
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={DIALOG_CLASS}>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Verify your email address</DialogTitle>
            <DialogDescription>
              {stage === 'send' ? (
                <>
                  We will email a 6-digit code to{' '}
                  <span className="font-medium text-foreground">{user.email}</span>. It expires
                  after 15 minutes.
                </>
              ) : (
                <>
                  Enter the 6-digit code we emailed to{' '}
                  <span className="font-medium text-foreground">{user.email}</span>. Codes expire
                  after 15 minutes; if yours did not arrive or has expired, send a new one.
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          {stage === 'send' ? (
            <>
              <FormError message={error} />
              <DialogFooter className="flex-row flex-wrap items-center">
                <Button type="button" variant="ghost" onClick={() => setStage('code')}>
                  I already have a code
                </Button>
                <Button
                  type="button"
                  disabled={sendEmailCode.isPending || wait > 0}
                  onClick={() => void send()}
                >
                  {sendEmailCode.isPending
                    ? 'Sending…'
                    : wait > 0
                      ? `Send code in ${wait}s`
                      : 'Send code'}
                </Button>
              </DialogFooter>
            </>
          ) : (
            <>
              <div className="flex flex-col gap-2">
                <Label htmlFor="email-code">Verification code</Label>
                <CodeField
                  id="email-code"
                  value={code}
                  onChange={(v) => {
                    setCode(v);
                    setError(null);
                  }}
                  length={6}
                  invalid={error !== null}
                />
              </div>
              {note && !error ? (
                <p role="status" className="text-xs text-muted-foreground">
                  {note}
                </p>
              ) : null}
              <FormError message={error} />
              <DialogFooter className="flex-row flex-wrap items-center">
                <ResendButton
                  wait={wait}
                  pending={sendEmailCode.isPending}
                  label="Send a new code"
                  onClick={() => void send()}
                />
                <Button type="submit" disabled={confirmEmail.isPending || code.length !== 6}>
                  {confirmEmail.isPending ? 'Verifying…' : 'Verify'}
                </Button>
              </DialogFooter>
            </>
          )}
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Change the email address: new address and password, then the code sent to the new address. */
export function EmailChangeDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { user } = useConsole();
  const router = useRouter();
  const { requestEmailChange, confirmEmailChange } = useVerificationMutations();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [resendAt, setResendAt] = useState<number | null>(null);
  const wait = useCountdown(resendAt);

  useEffect(() => {
    if (!open) return;
    setEmail('');
    setPassword('');
    setSentTo(null);
    setCode('');
    setError(null);
  }, [open]);

  async function send() {
    setError(null);
    try {
      const sent = await requestEmailChange.mutateAsync({ email: email.trim(), password });
      setSentTo(sent.email);
      setCode('');
      setResendAt(Date.parse(sent.resend_available_at));
    } catch (err) {
      const retry = retryAfterSeconds(err);
      if (retry !== null) setResendAt(Date.now() + retry * 1000);
      setError(messageOf(err));
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!sentTo) {
      await send();
      return;
    }
    setError(null);
    try {
      await confirmEmailChange.mutateAsync(code);
      toast.success(`Your email address is now ${sentTo}`);
      onOpenChange(false);
      router.refresh();
    } catch (err) {
      setError(messageOf(err));
      if (err instanceof BridgeApiError && err.code === 'code_expired') setCode('');
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={DIALOG_CLASS}>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Change email address</DialogTitle>
            <DialogDescription>
              {sentTo ? (
                <>
                  Enter the 6-digit code we emailed to{' '}
                  <span className="font-medium text-foreground">{sentTo}</span>. Your address
                  changes once the code is confirmed; you stay signed in.{' '}
                  <button
                    type="button"
                    className="font-medium text-primary hover:underline"
                    onClick={() => {
                      setSentTo(null);
                      setError(null);
                    }}
                  >
                    Use another address
                  </button>
                </>
              ) : (
                <>
                  You sign in with <span className="font-medium text-foreground">{user.email}</span>{' '}
                  today. We email a code to the new address to make sure it is yours, and let the
                  current one know.
                </>
              )}
            </DialogDescription>
          </DialogHeader>

          {sentTo ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="change-code">Verification code</Label>
              <CodeField
                id="change-code"
                value={code}
                onChange={(v) => {
                  setCode(v);
                  setError(null);
                }}
                length={6}
                invalid={error !== null}
              />
            </div>
          ) : (
            <>
              <div className="flex flex-col gap-2">
                <Label htmlFor="change-email">New email address</Label>
                <Input
                  id="change-email"
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => {
                    setEmail(e.target.value);
                    setError(null);
                  }}
                  maxLength={254}
                  autoFocus
                  required
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="change-password">Current password</Label>
                <Input
                  id="change-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value);
                    setError(null);
                  }}
                  required
                />
              </div>
            </>
          )}

          <FormError message={error} />

          <DialogFooter className={cn('flex-row flex-wrap items-center', !sentTo && 'justify-end')}>
            {sentTo ? (
              <>
                <ResendButton
                  wait={wait}
                  pending={requestEmailChange.isPending}
                  label="Send a new code"
                  onClick={() => void send()}
                />
                <Button type="submit" disabled={confirmEmailChange.isPending || code.length !== 6}>
                  {confirmEmailChange.isPending ? 'Confirming…' : 'Change email'}
                </Button>
              </>
            ) : (
              <>
                <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={requestEmailChange.isPending || !email.trim() || !password || wait > 0}
                >
                  {requestEmailChange.isPending
                    ? 'Sending…'
                    : wait > 0
                      ? `Send code in ${wait}s`
                      : 'Send code'}
                </Button>
              </>
            )}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Add or change the phone number: enter it, receive a code by SMS, enter the code. */
export function PhoneVerifyDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { user } = useConsole();
  const router = useRouter();
  const { sendPhoneCode, confirmPhone } = useVerificationMutations();
  const [phone, setPhone] = useState('');
  const [sent, setSent] = useState<PhoneVerificationSent | null>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [resendAt, setResendAt] = useState<number | null>(null);
  const wait = useCountdown(resendAt);

  useEffect(() => {
    if (!open) return;
    setPhone('');
    setSent(null);
    setCode('');
    setError(null);
  }, [open]);

  async function send(number: string) {
    setError(null);
    try {
      const r = await sendPhoneCode.mutateAsync(number.trim());
      setSent(r);
      setCode('');
      setResendAt(Date.parse(r.resend_available_at));
    } catch (err) {
      const retry = retryAfterSeconds(err);
      if (retry !== null) setResendAt(Date.now() + retry * 1000);
      setError(messageOf(err));
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!sent) {
      await send(phone);
      return;
    }
    setError(null);
    try {
      await confirmPhone.mutateAsync({ phone: sent.to, code });
      toast.success('Phone number verified');
      onOpenChange(false);
      router.refresh();
    } catch (err) {
      setError(messageOf(err));
      if (err instanceof BridgeApiError && err.code === 'code_expired') setCode('');
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={DIALOG_CLASS}>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{user.phone ? 'Change phone number' : 'Add a phone number'}</DialogTitle>
            <DialogDescription>
              {sent ? (
                <>
                  We {sent.environment === 'live' ? 'texted' : 'created'} a code for{' '}
                  <span className="font-mono font-medium text-foreground">{sent.to}</span>.{' '}
                  {sent.environment === 'live'
                    ? 'It can take a minute to arrive.'
                    : 'No SMS was sent.'}{' '}
                  <button
                    type="button"
                    className="font-medium text-primary hover:underline"
                    onClick={() => {
                      setSent(null);
                      setError(null);
                    }}
                  >
                    Use another number
                  </button>
                </>
              ) : (
                'Bridge texts a code to the number to make sure it is yours.'
              )}
            </DialogDescription>
          </DialogHeader>

          {sent ? (
            <>
              {sent.test_code ? (
                <p className="rounded-md bg-muted px-3 py-2 text-xs">
                  Test key: the code is{' '}
                  <span className="font-mono font-semibold tracking-widest">{sent.test_code}</span>
                </p>
              ) : null}
              <div className="flex flex-col gap-2">
                <Label htmlFor="phone-code">Verification code</Label>
                <CodeField
                  id="phone-code"
                  value={code}
                  onChange={(v) => {
                    setCode(v);
                    setError(null);
                  }}
                  length={10}
                  invalid={error !== null}
                />
              </div>
            </>
          ) : (
            <div className="flex flex-col gap-2">
              <Label htmlFor="phone-number">Phone number</Label>
              <Input
                id="phone-number"
                type="tel"
                inputMode="tel"
                autoComplete="tel"
                placeholder="+919876543210"
                value={phone}
                onChange={(e) => {
                  setPhone(e.target.value);
                  setError(null);
                }}
                aria-invalid={error !== null}
                className="font-mono"
                autoFocus
                required
              />
              <p className="text-xs text-muted-foreground">
                International format with the country code, starting with +.
              </p>
            </div>
          )}

          <FormError message={error} />

          <DialogFooter className={cn('flex-row flex-wrap items-center', !sent && 'justify-end')}>
            {sent ? (
              <>
                <ResendButton
                  wait={wait}
                  pending={sendPhoneCode.isPending}
                  label="Resend code"
                  onClick={() => void send(sent.to)}
                />
                <Button type="submit" disabled={confirmPhone.isPending || code.length < 4}>
                  {confirmPhone.isPending ? 'Verifying…' : 'Verify'}
                </Button>
              </>
            ) : (
              <>
                <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={sendPhoneCode.isPending || phone.trim().length < 4 || wait > 0}
                >
                  {sendPhoneCode.isPending
                    ? 'Sending…'
                    : wait > 0
                      ? `Send code in ${wait}s`
                      : 'Send code'}
                </Button>
              </>
            )}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** A slim reminder above the console while the email address is unverified. */
export function EmailVerifyBanner() {
  const { user } = useConsole();
  const config = useAuthConfig();
  const [dismissed, setDismissed] = useState(true);
  const [open, setOpen] = useState(false);
  const storageKey = `bridge:verify-email-banner:${user.id}`;

  // Read after mount so the server render and the first client render match.
  useEffect(() => {
    try {
      setDismissed(window.sessionStorage.getItem(storageKey) === 'dismissed');
    } catch {
      setDismissed(false);
    }
  }, [storageKey]);

  if (user.email_verified || !config.data?.email_verification) return null;

  function dismiss() {
    setDismissed(true);
    try {
      window.sessionStorage.setItem(storageKey, 'dismissed');
    } catch {
      // Storage may be unavailable; the banner stays hidden until reload.
    }
  }

  return (
    <>
      {dismissed ? null : (
        <div className="flex shrink-0 items-center gap-3 border-b border-warning/25 bg-warning/8 px-4 py-2 sm:px-6">
          <HugeiconsIcon
            icon={Mail01Icon}
            strokeWidth={2}
            className="size-4 shrink-0 text-warning"
            aria-hidden
          />
          <p className="min-w-0 flex-1 truncate text-xs/relaxed">
            <span className="font-medium">Verify your email address</span>
            <span className="hidden text-muted-foreground sm:inline">
              {' '}
              · confirm {user.email} with a 6-digit code
            </span>
          </p>
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            Verify
          </Button>
          <button
            type="button"
            onClick={dismiss}
            aria-label="Dismiss"
            className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-3.5" />
          </button>
        </div>
      )}
      <EmailVerifyDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
