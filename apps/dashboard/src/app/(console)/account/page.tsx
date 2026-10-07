'use client';

import type { Session } from '@bridge/api-types';
import { ComputerIcon, Logout03Icon, SmartPhone01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { useConsole } from '@/components/layout/console-context';
import { useSignOut } from '@/components/layout/user-menu';
import { ThemeSelector } from '@/components/theme-toggle';
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
import { Skeleton } from '@/components/ui/skeleton';
import { showError } from '@/lib/errors';
import { formatDate, formatRelative } from '@/lib/format';
import { useAccountMutations, useSessions } from '@/lib/queries';

/** "Chrome on Windows" from a user agent string; good enough to recognise a browser. */
function describeAgent(ua: string): { label: string; mobile: boolean } {
  if (!ua) return { label: 'Unknown browser', mobile: false };
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : ua.split(/[/ ]/)[0] || 'Browser';
  const os = /Windows/.test(ua)
    ? 'Windows'
    : /Android/.test(ua)
      ? 'Android'
      : /iPhone|iPad/.test(ua)
        ? 'iOS'
        : /Mac OS X/.test(ua)
          ? 'macOS'
          : /Linux/.test(ua)
            ? 'Linux'
            : '';
  return { label: os ? `${browser} on ${os}` : browser, mobile: /Mobile|Android|iPhone/.test(ua) };
}

function ProfileCard() {
  const { user } = useConsole();
  const { updateName } = useAccountMutations();
  const router = useRouter();
  const [name, setName] = useState(user.name);
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await updateName.mutateAsync(name.trim());
      toast.success('Profile saved');
      router.refresh();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <SectionCard title="Profile">
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="profile-name">Name</Label>
          <div className="flex gap-2">
            <Input
              id="profile-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
            />
            <Button
              type="submit"
              size="sm"
              disabled={updateName.isPending || name.trim() === user.name}
            >
              Save
            </Button>
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">Email</span>
          <span className="flex h-7 items-center truncate text-sm text-muted-foreground">
            {user.email}
          </span>
        </div>
        <p className="text-xs text-muted-foreground sm:col-span-2">
          Member since {formatDate(user.created_at)}.
        </p>
      </form>
    </SectionCard>
  );
}

function PasswordCard() {
  const { changePassword } = useAccountMutations();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const mismatch = confirm !== '' && confirm !== next;
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (mismatch) return;
    try {
      await changePassword.mutateAsync({ current_password: current, new_password: next });
      setCurrent('');
      setNext('');
      setConfirm('');
      toast.success('Password changed. Other sessions were signed out.');
    } catch (err) {
      showError(err);
    }
  }
  return (
    <SectionCard title="Password" description="Changing it signs out every other browser.">
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-3">
        <div className="flex flex-col gap-2">
          <Label htmlFor="pw-current">Current password</Label>
          <Input
            id="pw-current"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            required
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pw-new">New password</Label>
          <Input
            id="pw-new"
            type="password"
            autoComplete="new-password"
            minLength={10}
            value={next}
            onChange={(e) => setNext(e.target.value)}
            required
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pw-confirm">Repeat new password</Label>
          <Input
            id="pw-confirm"
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            aria-invalid={mismatch}
            required
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 sm:col-span-3">
          <p className={mismatch ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>
            {mismatch
              ? 'The new passwords do not match.'
              : 'At least 10 characters. A passphrase of a few words works well.'}
          </p>
          <Button
            type="submit"
            disabled={changePassword.isPending || mismatch || next.length < 10 || !current}
          >
            {changePassword.isPending ? 'Changing…' : 'Change password'}
          </Button>
        </div>
      </form>
    </SectionCard>
  );
}

const SESSIONS_SHOWN = 5;

function SessionsCard() {
  const sessions = useSessions();
  const { revokeSession, revokeOthers } = useAccountMutations();
  const [expanded, setExpanded] = useState(false);
  const all = [...(sessions.data ?? [])].sort((a, b) => Number(b.current) - Number(a.current));
  const shown = expanded ? all : all.slice(0, SESSIONS_SHOWN);
  const others = all.filter((s) => !s.current).length;
  return (
    <SectionCard
      title="Sessions"
      description="Browsers signed in to your account."
      action={
        others > 0 ? (
          <Button
            variant="outline"
            size="sm"
            disabled={revokeOthers.isPending}
            onClick={() =>
              revokeOthers.mutate(undefined, {
                onSuccess: (r) =>
                  toast.success(
                    `Signed out ${r.revoked} other session${r.revoked === 1 ? '' : 's'}`,
                  ),
                onError: showError,
              })
            }
          >
            Sign out everywhere else
          </Button>
        ) : undefined
      }
      contentClassName="p-0"
    >
      {sessions.isPending ? (
        <div className="p-5">
          <Skeleton className="h-10 w-full" />
        </div>
      ) : (
        <ul className="divide-y">
          {shown.map((s: Session) => {
            const agent = describeAgent(s.user_agent);
            return (
              <li key={s.id} className="flex items-center justify-between gap-3 px-5 py-3">
                <div className="flex min-w-0 items-center gap-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                    <HugeiconsIcon
                      icon={agent.mobile ? SmartPhone01Icon : ComputerIcon}
                      strokeWidth={2}
                      className="size-4"
                    />
                  </span>
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">
                      {agent.label}
                      {s.current ? (
                        <span className="ml-2 rounded-full bg-primary/12 px-2 py-0.5 text-[0.65rem] font-semibold text-primary">
                          This browser
                        </span>
                      ) : null}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {s.ip ?? 'Unknown IP'} · active {formatRelative(s.last_seen_at)} · signed in{' '}
                      {formatDate(s.created_at)}
                    </div>
                  </div>
                </div>
                {!s.current ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() =>
                      revokeSession.mutate(s.id, {
                        onSuccess: () => toast.success('Session signed out'),
                        onError: showError,
                      })
                    }
                  >
                    Sign out
                  </Button>
                ) : null}
              </li>
            );
          })}
          {all.length > SESSIONS_SHOWN ? (
            <li className="px-5 py-2">
              <Button variant="ghost" size="sm" onClick={() => setExpanded((v) => !v)}>
                {expanded ? 'Show fewer' : `Show all ${all.length} sessions`}
              </Button>
            </li>
          ) : null}
        </ul>
      )}
    </SectionCard>
  );
}

function DeleteAccount() {
  const { deleteAccount } = useAccountMutations();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await deleteAccount.mutateAsync({ password, confirm });
      router.replace('/login');
      router.refresh();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <section className="rounded-xl border border-destructive/40 bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
        <div className="min-w-0">
          <h2 className="font-display text-sm font-semibold">Delete account</h2>
          <p className="mt-0.5 text-xs/relaxed text-muted-foreground">
            Deletes your account and every organization where you are the only member, with all
            their projects and messages.
          </p>
        </div>
        <Button variant="destructive" onClick={() => setOpen(true)}>
          Delete account
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <form onSubmit={submit} className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Delete your account?</DialogTitle>
              <DialogDescription>
                This cannot be undone. If you are the only owner of an organization with other
                members, make someone else an owner first.
              </DialogDescription>
            </DialogHeader>
            <div className="flex flex-col gap-2">
              <Label htmlFor="del-password">Password</Label>
              <Input
                id="del-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="del-confirm">
                Type <span className="font-mono">DELETE</span> to confirm
              </Label>
              <Input
                id="del-confirm"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="off"
                required
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="destructive"
                disabled={deleteAccount.isPending || confirm !== 'DELETE' || !password}
              >
                {deleteAccount.isPending ? 'Deleting…' : 'Delete my account'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export default function AccountPage() {
  const { organizations } = useConsole();
  const signOut = useSignOut();

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <PageHeader title="Account" subtitle="Your profile, security and appearance." />
      <ProfileCard />
      <SectionCard
        title="Appearance"
        description="Dark is the default. System follows your operating system."
      >
        <ThemeSelector />
      </SectionCard>
      <PasswordCard />
      <SessionsCard />
      <SectionCard title="Organizations" contentClassName="p-0">
        <ul className="divide-y">
          {organizations.map((o) => (
            <li key={o.id} className="flex items-center justify-between gap-3 px-5 py-3">
              <div className="min-w-0">
                <Link
                  href={`/organizations/${o.id}/team`}
                  className="truncate text-sm font-medium hover:text-primary"
                >
                  {o.name}
                </Link>
                <div className="font-mono text-xs text-muted-foreground">{o.slug}</div>
              </div>
              <span className="rounded-full border px-2 py-0.5 text-[0.68rem] font-medium capitalize text-muted-foreground">
                {o.role}
              </span>
            </li>
          ))}
        </ul>
      </SectionCard>
      <SectionCard title="Sign out">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs/relaxed text-muted-foreground">
            Sign out of this browser. Other sessions stay signed in.
          </p>
          <Button variant="outline" size="lg" onClick={() => void signOut()}>
            <HugeiconsIcon icon={Logout03Icon} strokeWidth={2} />
            Sign out
          </Button>
        </div>
      </SectionCard>
      <DeleteAccount />
    </div>
  );
}
