'use client';

import type { Schedule } from '@bridge/api-types';
import {
  Delete02Icon,
  MoreHorizontalIcon,
  PauseIcon,
  PencilEdit02Icon,
  PlayIcon,
  PlusSignIcon,
  SentIcon,
  TimeScheduleIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useState } from 'react';
import { toast } from 'sonner';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { Segmented } from '@/components/kit/segmented';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan, useProjectId } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
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
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  type Environment,
  type ScheduleAction,
  useDeleteSchedule,
  useScheduleAction,
  useSchedules,
} from '@/lib/queries';
import { ScheduleDialog } from './_components/schedule-dialog';

const STATUS: Record<Schedule['status'], { kind: StatusKind; label: string }> = {
  active: { kind: 'success', label: 'Active' },
  paused: { kind: 'warning', label: 'Paused' },
  completed: { kind: 'neutral', label: 'Finished' },
};

function excerpt(text: string, n = 70): string {
  const t = text.replace(/\s+/g, ' ').trim();
  return t.length > n ? `${t.slice(0, n)}…` : t;
}

export default function SchedulesPage() {
  const projectId = useProjectId() ?? '';
  const canAdmin = useCan('admin');
  const [environment, setEnvironment] = useState<Environment>('test');
  const [editing, setEditing] = useState<Schedule | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [removing, setRemoving] = useState<Schedule | null>(null);
  const list = useSchedules(projectId, environment);
  const action = useScheduleAction(projectId);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  // Members can manage test schedules; live ones need an admin.
  const canWrite = environment === 'test' || canAdmin;

  function openCreate() {
    setEditing(null);
    setFormOpen(true);
  }

  async function run(s: Schedule, a: ScheduleAction) {
    try {
      const updated = await action.mutateAsync({ scheduleId: s.id, action: a });
      if (a === 'run') {
        toast.success(updated.last_error ? `Not sent: ${updated.last_error}` : 'Sent once now');
      } else {
        toast.success(a === 'pause' ? 'Schedule paused' : 'Schedule resumed');
      }
    } catch (err) {
      showError(err);
    }
  }

  const addButton = (
    <Button size="lg" onClick={openCreate}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
      Schedule a message
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Schedules"
        subtitle="Messages sent at a set time: once, or every day, week or month. Times follow the chosen time zone, including daylight saving."
        actions={
          <>
            <Segmented
              label="Environment"
              value={environment}
              onChange={setEnvironment}
              options={[
                { value: 'test', label: 'Test' },
                { value: 'live', label: 'Live' },
              ]}
            />
            {rows.length > 0 ? addButton : null}
          </>
        }
      />

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {list.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : list.isError ? (
          <EmptyState
            title="Could not load schedules"
            description={list.error.message}
            action={
              <Button variant="outline" onClick={() => list.refetch()}>
                Try again
              </Button>
            }
          />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={TimeScheduleIcon} strokeWidth={1.8} />}
            title={`No ${environment} schedules yet`}
            description="Send reminders, reports or check-ins on a timetable. Bridge creates each message when it is due."
            action={addButton}
          />
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-5">Message</TableHead>
                  <TableHead>When</TableHead>
                  <TableHead>Next run</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last run</TableHead>
                  <TableHead className="w-12 pr-5">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((s) => {
                  const live = s.environment === 'live';
                  const writable = !live || canAdmin;
                  return (
                    <TableRow key={s.id}>
                      <TableCell className="max-w-[18rem] pl-5">
                        <span className="block truncate font-medium">
                          {s.name || excerpt(s.message, 50)}
                        </span>
                        <span className="block truncate text-xs text-muted-foreground">
                          <span className="font-mono">{s.to}</span>
                          {s.name ? ` · ${excerpt(s.message, 50)}` : ''}
                        </span>
                      </TableCell>
                      <TableCell className="max-w-[16rem] whitespace-normal text-xs/relaxed">
                        {s.description}
                        {s.ends_at ? (
                          <span className="block text-muted-foreground">
                            Until {formatDateTime(s.ends_at)}
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        {s.next_run_at && s.status === 'active' ? (
                          <>
                            <span className="block">{formatRelative(s.next_run_at)}</span>
                            <span className="block text-xs text-muted-foreground">
                              {formatDateTime(s.next_run_at)}
                            </span>
                          </>
                        ) : (
                          <span className="text-muted-foreground">
                            {s.status === 'paused' ? 'Paused' : '—'}
                          </span>
                        )}
                      </TableCell>
                      <TableCell>
                        <StatusBadge kind={STATUS[s.status].kind}>
                          {STATUS[s.status].label}
                        </StatusBadge>
                      </TableCell>
                      <TableCell className="max-w-[16rem]">
                        <LastRun projectId={projectId} s={s} />
                      </TableCell>
                      <TableCell className="pr-5 text-right">
                        {writable ? (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={`Actions for ${s.name || s.to}`}
                              >
                                <HugeiconsIcon icon={MoreHorizontalIcon} strokeWidth={2} />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-44">
                              <DropdownMenuItem
                                onSelect={() => {
                                  setEditing(s);
                                  setFormOpen(true);
                                }}
                              >
                                <HugeiconsIcon icon={PencilEdit02Icon} strokeWidth={2} />
                                Edit
                              </DropdownMenuItem>
                              <DropdownMenuItem onSelect={() => void run(s, 'run')}>
                                <HugeiconsIcon icon={SentIcon} strokeWidth={2} />
                                Send now
                              </DropdownMenuItem>
                              {s.status === 'paused' ? (
                                <DropdownMenuItem onSelect={() => void run(s, 'resume')}>
                                  <HugeiconsIcon icon={PlayIcon} strokeWidth={2} />
                                  Resume
                                </DropdownMenuItem>
                              ) : s.status === 'active' ? (
                                <DropdownMenuItem onSelect={() => void run(s, 'pause')}>
                                  <HugeiconsIcon icon={PauseIcon} strokeWidth={2} />
                                  Pause
                                </DropdownMenuItem>
                              ) : null}
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                variant="destructive"
                                onSelect={() => setRemoving(s)}
                              >
                                <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
            {list.hasNextPage ? (
              <div className="flex justify-center border-t p-3">
                <Button
                  variant="ghost"
                  onClick={() => list.fetchNextPage()}
                  disabled={list.isFetchingNextPage}
                >
                  {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>

      <ul className="flex list-disc flex-col gap-1 pl-4 text-xs/relaxed text-muted-foreground">
        <li>
          Each run creates an ordinary message, listed under Messages with its own timeline and
          webhooks.
        </li>
        <li>
          Runs missed while a schedule is paused are skipped. Send now sends once without moving the
          next run.
        </li>
        <li>
          A number that opted out is skipped and the reason is shown under Last run.
          {!canWrite ? ' Only admins can change live schedules.' : ''}
        </li>
      </ul>

      <ScheduleDialog
        projectId={projectId}
        open={formOpen}
        schedule={editing}
        environment={environment}
        onOpenChange={setFormOpen}
        onSaved={(s) => {
          setFormOpen(false);
          setEnvironment(s.environment);
        }}
      />
      <DeleteDialog projectId={projectId} schedule={removing} onClose={() => setRemoving(null)} />
    </div>
  );
}

function LastRun({ projectId, s }: { projectId: string; s: Schedule }) {
  if (!s.last_run_at) return <span className="text-muted-foreground">Not run yet</span>;
  return (
    <div className="flex min-w-0 flex-col">
      <span title={formatDateTime(s.last_run_at)}>
        {formatRelative(s.last_run_at)}
        <span className="text-muted-foreground">
          {' '}
          · {s.run_count} run{s.run_count === 1 ? '' : 's'}
        </span>
      </span>
      {s.last_error ? (
        <span className="truncate text-xs text-destructive" title={s.last_error}>
          {s.last_error}
        </span>
      ) : s.last_message_id ? (
        <Link
          href={`/projects/${projectId}/messages`}
          className="truncate font-mono text-xs text-muted-foreground hover:text-primary"
          title="Open Messages"
        >
          {s.last_message_id}
        </Link>
      ) : null}
    </div>
  );
}

function DeleteDialog({
  projectId,
  schedule,
  onClose,
}: {
  projectId: string;
  schedule: Schedule | null;
  onClose: () => void;
}) {
  const remove = useDeleteSchedule(projectId);
  async function confirm() {
    if (!schedule) return;
    try {
      await remove.mutateAsync(schedule.id);
      toast.success('Schedule deleted');
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={schedule !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete this schedule?</DialogTitle>
          <DialogDescription>
            {schedule
              ? `“${schedule.name || excerpt(schedule.message, 40)}” to ${schedule.to} stops for good. Messages it already sent are kept.`
              : null}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Deleting…' : 'Delete schedule'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
