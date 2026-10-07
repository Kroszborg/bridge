'use client';

import type { CreatedInvite, Member } from '@bridge/api-types';
import {
  Delete02Icon,
  Link01Icon,
  Logout03Icon,
  PlusSignIcon,
  UserGroupIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useParams, useRouter } from 'next/navigation';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { CopyButton } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { useConsole } from '@/components/layout/console-context';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { showError } from '@/lib/errors';
import { formatDate, formatRelative } from '@/lib/format';
import { useInvites, useMembers, useTeamMutations } from '@/lib/queries';

type Role = 'owner' | 'admin' | 'member';
const RANK: Record<Role, number> = { member: 1, admin: 2, owner: 3 };

const ROLES: { value: Role; label: string; description: string }[] = [
  {
    value: 'member',
    label: 'Member',
    description: 'Sees everything in the projects and sends test messages.',
  },
  {
    value: 'admin',
    label: 'Admin',
    description: 'Also manages API keys, phones, webhooks, projects and members.',
  },
  {
    value: 'owner',
    label: 'Owner',
    description: 'Full control, including deleting projects and managing owners.',
  },
];

function initials(name: string, email: string) {
  const source = name.trim() || email.split('@')[0] || '?';
  const parts = source.split(/[\s._-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '?') + (parts[1]?.[0] ?? '')).toUpperCase();
}

function InviteDialog({
  organizationId,
  myRole,
  open,
  onOpenChange,
}: {
  organizationId: string;
  myRole: Role;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const { createInvite } = useTeamMutations(organizationId);
  const [role, setRole] = useState<Role>('member');
  const [email, setEmail] = useState('');
  const [created, setCreated] = useState<CreatedInvite | null>(null);
  useEffect(() => {
    if (open) {
      setRole('member');
      setEmail('');
      setCreated(null);
    }
  }, [open]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      setCreated(await createInvite.mutateAsync({ role, email: email.trim() || undefined }));
    } catch (err) {
      showError(err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {created ? (
          <div className="flex min-w-0 flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Share this invite link</DialogTitle>
              <DialogDescription>
                It works once and expires {formatDate(created.expires_at)}. Bridge doesn&apos;t send
                email, so send it yourself{created.email ? ` to ${created.email}` : ''}.
              </DialogDescription>
            </DialogHeader>
            <div className="flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 truncate font-mono text-xs">{created.url}</code>
              <CopyButton value={created.url} label="Copy link" showLabel />
            </div>
            <p className="text-xs text-muted-foreground">
              Whoever opens it joins as{' '}
              <span className="font-medium text-foreground capitalize">{created.role}</span>. If the
              link ends up with the wrong person, revoke it under Pending invites.
            </p>
            <DialogFooter>
              <Button onClick={() => onOpenChange(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={submit} className="flex flex-col gap-5">
            <DialogHeader>
              <DialogTitle>Invite people</DialogTitle>
              <DialogDescription>
                Create a single-use link that adds one person to this organization.
              </DialogDescription>
            </DialogHeader>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-2 text-sm font-medium">Role</legend>
              {ROLES.filter((r) => r.value !== 'owner' || myRole === 'owner').map((r) => (
                <label
                  key={r.value}
                  className="flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5"
                >
                  <input
                    type="radio"
                    name="role"
                    value={r.value}
                    checked={role === r.value}
                    onChange={() => setRole(r.value)}
                    className="mt-0.5 accent-primary"
                  />
                  <span className="flex flex-col">
                    <span className="text-sm font-medium">{r.label}</span>
                    <span className="text-xs text-muted-foreground">{r.description}</span>
                  </span>
                </label>
              ))}
            </fieldset>
            <div className="flex flex-col gap-2">
              <Label htmlFor="invite-email">
                Who is it for?{' '}
                <span className="font-normal text-muted-foreground">(optional note)</span>
              </Label>
              <Input
                id="invite-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="grace@example.com"
                maxLength={254}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createInvite.isPending}>
                <HugeiconsIcon icon={Link01Icon} strokeWidth={2} />
                {createInvite.isPending ? 'Creating…' : 'Create invite link'}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function RemoveDialog({
  organizationId,
  member,
  orgName,
  onClose,
}: {
  organizationId: string;
  member: Member | null;
  orgName: string;
  onClose: () => void;
}) {
  const { removeMember } = useTeamMutations(organizationId);
  const router = useRouter();
  const leaving = member?.you ?? false;
  async function confirm() {
    if (!member) return;
    try {
      await removeMember.mutateAsync(member.id);
      if (leaving) {
        toast.success(`You left ${orgName}`);
        router.replace('/');
        router.refresh();
        return;
      }
      toast.success(`Removed ${member.name || member.email}`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={member !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {leaving ? `Leave ${orgName}?` : `Remove ${member?.name || member?.email}?`}
          </DialogTitle>
          <DialogDescription>
            {leaving
              ? 'You lose access to its projects right away. An admin can invite you again.'
              : 'They lose access to every project in this organization right away. Their API keys keep working until you revoke them.'}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={removeMember.isPending}>
            {removeMember.isPending
              ? 'Removing…'
              : leaving
                ? 'Leave organization'
                : 'Remove member'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OrgName({
  organizationId,
  name,
  canEdit,
}: {
  organizationId: string;
  name: string;
  canEdit: boolean;
}) {
  const { rename } = useTeamMutations(organizationId);
  const router = useRouter();
  const [value, setValue] = useState(name);
  useEffect(() => setValue(name), [name]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await rename.mutateAsync(value.trim());
      toast.success('Organization renamed');
      router.refresh();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <SectionCard title="Organization" description="The name everyone in it sees.">
      <form onSubmit={submit} className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Organization name"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          maxLength={80}
          disabled={!canEdit}
          className="max-w-sm"
          required
        />
        {canEdit ? (
          <Button
            type="submit"
            size="sm"
            disabled={rename.isPending || value.trim() === name || !value.trim()}
          >
            {rename.isPending ? 'Saving…' : 'Save'}
          </Button>
        ) : (
          <span className="text-xs text-muted-foreground">
            Only admins can rename the organization.
          </span>
        )}
      </form>
    </SectionCard>
  );
}

export default function TeamPage() {
  const { organizationId } = useParams<{ organizationId: string }>();
  const { organizations } = useConsole();
  const org = organizations.find((o) => o.id === organizationId);
  const myRole = (org?.role ?? 'member') as Role;
  const isAdmin = RANK[myRole] >= RANK.admin;
  const members = useMembers(organizationId);
  const invites = useInvites(organizationId, isAdmin);
  const { updateRole, revokeInvite } = useTeamMutations(organizationId);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [removing, setRemoving] = useState<Member | null>(null);

  if (!org) {
    return (
      <EmptyState
        title="Organization not found"
        description="It may have been deleted, or you are no longer a member."
      />
    );
  }

  // Which roles I may assign to this member.
  const assignable = (m: Member): Role[] => {
    if (!isAdmin || m.you) return [];
    if (m.role === 'owner' && myRole !== 'owner') return [];
    return myRole === 'owner' ? ['owner', 'admin', 'member'] : ['admin', 'member'];
  };
  const owners = (members.data ?? []).filter((m) => m.role === 'owner').length;
  // The last owner cannot leave or be removed; the API refuses it too.
  const canRemove = (m: Member) =>
    !(m.role === 'owner' && owners <= 1) &&
    (m.you || (isAdmin && (m.role !== 'owner' || myRole === 'owner')));

  async function changeRole(m: Member, role: Role) {
    try {
      await updateRole.mutateAsync({ memberId: m.id, role });
      toast.success(`${m.name || m.email} is now ${role === 'admin' ? 'an admin' : `a ${role}`}`);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <div className="flex max-w-5xl flex-col gap-6">
      <PageHeader
        title="Team"
        subtitle={`People in ${org.name}. Everyone here can see all of its projects.`}
        actions={
          isAdmin ? (
            <Button size="lg" onClick={() => setInviteOpen(true)}>
              <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
              Invite people
            </Button>
          ) : undefined
        }
      />

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {members.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : members.isError ? (
          <EmptyState title="Could not load members" description={members.error.message} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Member</TableHead>
                <TableHead>Role</TableHead>
                <TableHead className="hidden md:table-cell">Joined</TableHead>
                <TableHead className="hidden md:table-cell">Last active</TableHead>
                <TableHead className="pr-5 text-right">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.data.map((m) => {
                const roles = assignable(m);
                return (
                  <TableRow key={m.id}>
                    <TableCell className="pl-5">
                      <div className="flex min-w-0 items-center gap-3">
                        <Avatar className="size-8">
                          <AvatarFallback className="bg-primary/12 text-[0.7rem] font-semibold text-primary">
                            {initials(m.name, m.email)}
                          </AvatarFallback>
                        </Avatar>
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium">
                            {m.name || m.email.split('@')[0]}
                            {m.you ? (
                              <span className="ml-1.5 text-xs font-normal text-muted-foreground">
                                (you)
                              </span>
                            ) : null}
                          </div>
                          <div className="truncate text-xs text-muted-foreground">{m.email}</div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      {roles.length > 0 ? (
                        <Select value={m.role} onValueChange={(v) => void changeRole(m, v as Role)}>
                          <SelectTrigger
                            className="w-28"
                            aria-label={`Role of ${m.name || m.email}`}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {roles.map((r) => (
                              <SelectItem key={r} value={r} className="capitalize">
                                {r}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <span className="rounded-full border px-2 py-0.5 text-[0.7rem] font-medium capitalize text-muted-foreground">
                          {m.role}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">
                      {formatDate(m.joined_at)}
                    </TableCell>
                    <TableCell
                      className="hidden text-muted-foreground md:table-cell"
                      title={m.last_active_at}
                    >
                      {formatRelative(m.last_active_at)}
                    </TableCell>
                    <TableCell className="pr-5 text-right">
                      {canRemove(m) ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-muted-foreground hover:text-destructive"
                          onClick={() => setRemoving(m)}
                        >
                          <HugeiconsIcon
                            icon={m.you ? Logout03Icon : Delete02Icon}
                            strokeWidth={2}
                          />
                          {m.you ? 'Leave' : 'Remove'}
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>

      {isAdmin ? (
        <SectionCard
          title="Pending invites"
          description="Links that have not been used yet. Each works once."
          contentClassName="p-0"
        >
          {invites.isPending ? (
            <div className="p-5">
              <Skeleton className="h-8 w-full" />
            </div>
          ) : !invites.data?.length ? (
            <EmptyState
              className="py-8"
              icon={<HugeiconsIcon icon={UserGroupIcon} strokeWidth={1.8} />}
              title="No pending invites"
              description="Invite teammates to share phones, API keys and message history."
            />
          ) : (
            <ul className="divide-y">
              {invites.data.map((i) => (
                <li
                  key={i.id}
                  className="flex flex-wrap items-center justify-between gap-3 px-5 py-3"
                >
                  <div className="min-w-0">
                    <div className="text-sm">
                      <span className="font-medium capitalize">{i.role}</span>
                      {i.email ? (
                        <span className="text-muted-foreground"> · for {i.email}</span>
                      ) : null}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      Created by {i.invited_by || 'someone'} · expires{' '}
                      {formatRelative(i.expires_at)}
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() =>
                      revokeInvite.mutate(i.id, {
                        onSuccess: () => toast.success('Invite revoked'),
                        onError: showError,
                      })
                    }
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </SectionCard>
      ) : null}

      <OrgName organizationId={organizationId} name={org.name} canEdit={isAdmin} />

      <SectionCard title="Roles" contentClassName="p-0">
        <ul className="divide-y">
          {ROLES.map((r) => (
            <li key={r.value} className="flex flex-col gap-0.5 px-5 py-3 sm:flex-row sm:gap-6">
              <span className="w-20 shrink-0 text-sm font-medium">{r.label}</span>
              <span className="text-xs/relaxed text-muted-foreground">{r.description}</span>
            </li>
          ))}
        </ul>
      </SectionCard>

      <InviteDialog
        organizationId={organizationId}
        myRole={myRole}
        open={inviteOpen}
        onOpenChange={setInviteOpen}
      />
      <RemoveDialog
        organizationId={organizationId}
        member={removing}
        orgName={org.name}
        onClose={() => setRemoving(null)}
      />
    </div>
  );
}
