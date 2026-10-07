'use client';

import type { AuditEntry } from '@bridge/api-types';
import {
  ApiIcon,
  Key01Icon,
  SecurityCheckIcon,
  Settings02Icon,
  SmartPhone01Icon,
  UserCircleIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useParams } from 'next/navigation';
import { Fragment, useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { useConsole } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
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
import { formatDateTime, formatRelative } from '@/lib/format';
import { useAuditLog, useProjectsByOrg } from '@/lib/queries';
import { cn } from '@/lib/utils';

const CATEGORIES = [
  { value: undefined, label: 'All' },
  { value: 'api_key.', label: 'API keys' },
  { value: 'device.', label: 'Phones' },
  { value: 'webhook.', label: 'Webhooks' },
  { value: 'member.', label: 'Members' },
  { value: 'invite.', label: 'Invites' },
  { value: 'user.', label: 'Account security' },
  { value: 'project.', label: 'Projects' },
] as const;

/** A readable sentence for each recorded action. */
const ACTIONS: Record<string, string> = {
  'api_key.created': 'created an API key',
  'api_key.revoked': 'revoked an API key',
  'device.paired': 'paired a phone',
  'device.removed': 'removed a phone',
  'device.inbound_forwarding_enabled': 'turned on incoming SMS forwarding',
  'device.inbound_forwarding_disabled': 'turned off incoming SMS forwarding',
  'webhook.created': 'added a webhook endpoint',
  'webhook.updated': 'changed a webhook endpoint',
  'webhook.deleted': 'removed a webhook endpoint',
  'webhook.secret_revealed': 'revealed a webhook signing secret',
  'webhook.secret_rotated': 'rotated a webhook signing secret',
  'member.role_changed': 'changed a member’s role',
  'member.removed': 'removed a member',
  'member.left': 'left the organization',
  'invite.created': 'created an invite link',
  'invite.revoked': 'revoked an invite link',
  'invite.accepted': 'joined through an invite',
  'organization.created': 'created the organization',
  'organization.renamed': 'renamed the organization',
  'project.created': 'created a project',
  'project.renamed': 'renamed a project',
  'project.deleted': 'deleted a project',
  'user.signed_up': 'signed up',
  'user.password_changed': 'changed their password',
  'user.session_revoked': 'signed out a session',
  'user.other_sessions_revoked': 'signed out all other sessions',
};

function actorIcon(type: string) {
  switch (type) {
    case 'api_key':
      return Key01Icon;
    case 'device':
      return SmartPhone01Icon;
    case 'system':
      return Settings02Icon;
    default:
      return UserCircleIcon;
  }
}

/** Short context for an entry, from its metadata. */
function summary(e: AuditEntry): string {
  const m = e.metadata as Record<string, unknown>;
  const parts: string[] = [];
  if (typeof m.name === 'string') parts.push(m.name);
  if (typeof m.url === 'string') parts.push(m.url);
  if (m.from !== undefined && m.to !== undefined) parts.push(`${String(m.from)} → ${String(m.to)}`);
  if (typeof m.role === 'string' && m.from === undefined) parts.push(m.role);
  if (typeof m.environment === 'string') parts.push(m.environment);
  return parts.join(' · ');
}

export default function AuditPage() {
  const { organizationId } = useParams<{ organizationId: string }>();
  const { organizations } = useConsole();
  const org = organizations.find((o) => o.id === organizationId);
  const projectsByOrg = useProjectsByOrg(org ? [org] : []);
  const projects = projectsByOrg[organizationId] ?? [];
  const [action, setAction] = useState<string | undefined>(undefined);
  const [projectId, setProjectId] = useState<string | undefined>(undefined);
  const [open, setOpen] = useState<string | null>(null);
  const log = useAuditLog(organizationId, { action, project_id: projectId });
  const rows = log.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Audit log"
        subtitle={`Security-relevant changes in ${org?.name ?? 'this organization'}: who did what, and when. Visible to owners and admins.`}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex flex-wrap gap-2">
          {CATEGORIES.map((c) => (
            <button
              key={c.label}
              type="button"
              aria-pressed={action === c.value}
              onClick={() => setAction(c.value)}
              className={cn(
                'rounded-full border px-3 py-1 text-xs/relaxed font-medium transition-colors',
                action === c.value
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'bg-card text-muted-foreground hover:bg-muted',
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
        <Select
          value={projectId ?? 'all'}
          onValueChange={(v) => setProjectId(v === 'all' ? undefined : v)}
        >
          <SelectTrigger className="w-full lg:w-56" aria-label="Project">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All projects</SelectItem>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {log.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : log.isError ? (
          <EmptyState title="Could not load the audit log" description={log.error.message} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={SecurityCheckIcon} strokeWidth={1.8} />}
            title="Nothing recorded yet"
            description="Changes to keys, phones, webhooks, members and account security appear here."
          />
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-5">Event</TableHead>
                  <TableHead className="hidden lg:table-cell">Project</TableHead>
                  <TableHead className="hidden md:table-cell">IP</TableHead>
                  <TableHead className="pr-5 text-right">When</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((e) => {
                  const detail = summary(e);
                  return (
                    <Fragment key={e.id}>
                      <TableRow
                        className="cursor-pointer"
                        tabIndex={0}
                        aria-expanded={open === e.id}
                        onClick={() => setOpen(open === e.id ? null : e.id)}
                        onKeyDown={(ev) =>
                          ev.key === 'Enter' && setOpen(open === e.id ? null : e.id)
                        }
                      >
                        <TableCell className="max-w-[36rem] pl-5 whitespace-normal">
                          <div className="flex items-start gap-3">
                            <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-full bg-muted text-muted-foreground">
                              <HugeiconsIcon
                                icon={
                                  e.actor.type === 'api_key' ? ApiIcon : actorIcon(e.actor.type)
                                }
                                strokeWidth={2}
                                className="size-3.5"
                              />
                            </span>
                            <div className="min-w-0 text-sm">
                              <span className="font-medium">
                                {e.actor.name ||
                                  e.actor.email ||
                                  (e.actor.type === 'system' ? 'Bridge' : e.actor.type)}
                              </span>{' '}
                              <span className="text-muted-foreground">
                                {ACTIONS[e.action] ?? e.action.replaceAll('_', ' ')}
                              </span>
                              {detail ? (
                                <div className="truncate font-mono text-xs text-muted-foreground">
                                  {detail}
                                </div>
                              ) : null}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell className="hidden text-muted-foreground lg:table-cell">
                          {e.project_name || '—'}
                        </TableCell>
                        <TableCell className="hidden font-mono text-xs text-muted-foreground md:table-cell">
                          {e.ip ?? '—'}
                        </TableCell>
                        <TableCell
                          className="pr-5 text-right text-muted-foreground"
                          title={formatDateTime(e.created_at)}
                        >
                          {formatRelative(e.created_at)}
                        </TableCell>
                      </TableRow>
                      {open === e.id ? (
                        <TableRow className="bg-muted/30 hover:bg-muted/30">
                          <TableCell colSpan={4} className="px-5 py-3 whitespace-normal">
                            <dl className="mb-3 grid gap-x-6 gap-y-1 text-xs sm:grid-cols-3">
                              <div>
                                <dt className="text-muted-foreground">Action</dt>
                                <dd className="font-mono">{e.action}</dd>
                              </div>
                              <div>
                                <dt className="text-muted-foreground">Actor</dt>
                                <dd className="font-mono">
                                  {e.actor.type} {e.actor.id ?? ''}
                                </dd>
                              </div>
                              <div>
                                <dt className="text-muted-foreground">Target</dt>
                                <dd className="font-mono">
                                  {e.target_type ? `${e.target_type} ${e.target_id ?? ''}` : '—'}
                                </dd>
                              </div>
                            </dl>
                            {Object.keys(e.metadata).length > 0 ? (
                              <CodeBlock
                                language="metadata"
                                code={JSON.stringify(e.metadata, null, 2)}
                              />
                            ) : null}
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  );
                })}
              </TableBody>
            </Table>
            {log.hasNextPage ? (
              <div className="flex justify-center border-t p-3">
                <Button
                  variant="ghost"
                  onClick={() => log.fetchNextPage()}
                  disabled={log.isFetchingNextPage}
                >
                  {log.isFetchingNextPage ? 'Loading…' : 'Load older entries'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
