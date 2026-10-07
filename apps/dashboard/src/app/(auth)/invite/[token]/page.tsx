'use client';

import { UserGroupIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { api, BridgeApiError, unwrap } from '@/lib/api';
import { formatDate } from '@/lib/format';

const REASONS: Record<string, string> = {
  used: 'This invite link was already used. Each link works once; ask for a new one.',
  revoked: 'This invite link was revoked. Ask the person who sent it for a new one.',
  expired: 'This invite link has expired. Links work for 7 days; ask for a new one.',
};

export default function InvitePage() {
  const { token } = useParams<{ token: string }>();
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [joining, setJoining] = useState(false);
  const preview = useQuery({
    queryKey: ['invite', token],
    queryFn: () => unwrap(api.GET('/v1/invites/{token}', { params: { path: { token } } })),
    retry: false,
  });
  // Signed in already? Then the invite can be accepted right here.
  const me = useQuery({
    queryKey: ['invite-me'],
    queryFn: async () => {
      const res = await api.GET('/v1/me');
      return res.response.ok ? res.data : null;
    },
    retry: false,
  });

  async function accept() {
    setJoining(true);
    setError(null);
    try {
      const org = await unwrap(
        api.POST('/v1/invites/{token}/accept', { params: { path: { token } } }),
      );
      router.replace(`/organizations/${org.id}/team`);
      router.refresh();
    } catch (err) {
      setJoining(false);
      setError(err instanceof BridgeApiError ? err.message : 'Something went wrong. Try again.');
    }
  }

  if (preview.isPending || me.isPending) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-80" />
        <Skeleton className="mt-6 h-10 w-full" />
      </div>
    );
  }
  if (preview.isError) {
    return (
      <>
        <h2 className="font-display text-2xl font-semibold tracking-tight">Invite not found</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Check that you copied the whole link. Invite links start with{' '}
          <span className="font-mono">/invite/bi_</span>.
        </p>
      </>
    );
  }
  const inv = preview.data;
  return (
    <>
      <span className="mb-5 grid size-11 place-items-center rounded-xl bg-primary/12 text-primary">
        <HugeiconsIcon icon={UserGroupIcon} strokeWidth={2} className="size-5" />
      </span>
      <h2 className="font-display text-2xl font-semibold tracking-tight">
        Join {inv.organization_name}
      </h2>
      <p className="mt-2 text-sm text-muted-foreground">
        {inv.invited_by ? `${inv.invited_by} invited you` : 'You were invited'} to join as{' '}
        <span className="font-medium text-foreground">
          {inv.role === 'admin' ? 'an admin' : `a ${inv.role}`}
        </span>
        .{inv.valid ? ` The link expires ${formatDate(inv.expires_at)}.` : ''}
      </p>

      {!inv.valid ? (
        <p
          role="alert"
          className="mt-6 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {REASONS[inv.reason ?? ''] ?? 'This invite link cannot be used.'}
        </p>
      ) : me.data ? (
        <div className="mt-8 flex flex-col gap-3">
          <Button size="lg" onClick={accept} disabled={joining}>
            {joining ? 'Joining…' : `Join as ${me.data.user.email}`}
          </Button>
          <p className="text-center text-xs text-muted-foreground">
            Not you?{' '}
            <Link
              href={`/login?next=${encodeURIComponent(`/invite/${token}`)}`}
              className="font-medium text-primary hover:underline"
            >
              Sign in with another account
            </Link>
          </p>
        </div>
      ) : (
        <div className="mt-8 flex flex-col gap-3">
          <Button size="lg" asChild>
            <Link href={`/signup?invite=${encodeURIComponent(token)}`}>
              Create an account and join
            </Link>
          </Button>
          <Button size="lg" variant="outline" asChild>
            <Link href={`/login?next=${encodeURIComponent(`/invite/${token}`)}`}>
              I already have an account
            </Link>
          </Button>
        </div>
      )}
      {error ? (
        <p
          role="alert"
          className="mt-4 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          {error}
        </p>
      ) : null}
    </>
  );
}
