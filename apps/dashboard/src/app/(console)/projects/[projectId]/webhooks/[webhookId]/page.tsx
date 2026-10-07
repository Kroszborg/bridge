'use client';

import type { WebhookDelivery } from '@bridge/api-types';
import {
  Alert02Icon,
  ArrowLeft01Icon,
  Delete02Icon,
  MoreHorizontalIcon,
  PencilEdit02Icon,
  RefreshIcon,
  SentIcon,
  ViewIcon,
  ViewOffSlashIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { Fragment, useState } from 'react';
import { toast } from 'sonner';
import { CopyButton, CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { StatusBadge } from '@/components/kit/status-badge';
import { useProjectId } from '@/components/layout/console-context';
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
import {
  EventSummary,
  VerifyInstructions,
  WebhookFormDialog,
  WebhookHealth,
} from '@/components/webhooks';
import { showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  useDeleteWebhook,
  useTestWebhook,
  useUpdateWebhook,
  useWebhook,
  useWebhookDeliveries,
  useWebhookSecret,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

function DeliveryResult({ d }: { d: WebhookDelivery }) {
  if (d.succeeded) return <StatusBadge kind="success">{d.response_status}</StatusBadge>;
  if (d.response_status) return <StatusBadge kind="danger">{d.response_status}</StatusBadge>;
  return <StatusBadge kind="danger">No response</StatusBadge>;
}

function DeliveryLog({ projectId, webhookId }: { projectId: string; webhookId: string }) {
  const deliveries = useWebhookDeliveries(projectId, webhookId);
  const [open, setOpen] = useState<string | null>(null);

  if (deliveries.isPending) {
    return (
      <div className="flex flex-col gap-3 p-5">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    );
  }
  if (deliveries.isError) {
    return <EmptyState title="Could not load deliveries" description={deliveries.error.message} />;
  }
  if (deliveries.data.length === 0) {
    return (
      <EmptyState
        title="No deliveries yet"
        description="Send a test event, or wait for the next event this endpoint subscribes to."
      />
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead className="pl-5">Event</TableHead>
          <TableHead>Result</TableHead>
          <TableHead className="hidden text-right sm:table-cell">Attempt</TableHead>
          <TableHead className="hidden text-right sm:table-cell">Duration</TableHead>
          <TableHead className="pr-5 text-right">Time</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {deliveries.data.map((d) => (
          <Fragment key={d.id}>
            <TableRow
              className="cursor-pointer"
              aria-expanded={open === d.id}
              tabIndex={0}
              onClick={() => setOpen(open === d.id ? null : d.id)}
              onKeyDown={(e) => e.key === 'Enter' && setOpen(open === d.id ? null : d.id)}
            >
              <TableCell className="pl-5 font-mono text-xs">{d.event_type}</TableCell>
              <TableCell>
                <DeliveryResult d={d} />
              </TableCell>
              <TableCell className="hidden text-right tabular-nums text-muted-foreground sm:table-cell">
                {d.attempt}
              </TableCell>
              <TableCell className="hidden text-right tabular-nums text-muted-foreground sm:table-cell">
                {d.duration_ms} ms
              </TableCell>
              <TableCell
                className="pr-5 text-right text-muted-foreground"
                title={formatDateTime(d.created_at)}
              >
                {formatRelative(d.created_at)}
              </TableCell>
            </TableRow>
            {open === d.id ? (
              <TableRow className="bg-muted/30 hover:bg-muted/30">
                <TableCell colSpan={5} className="px-5 py-3 whitespace-normal">
                  <div className="flex min-w-0 flex-col gap-2 text-xs">
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-muted-foreground">
                      <span>
                        webhook-id <span className="font-mono text-foreground">{d.event_id}</span>
                      </span>
                      <span>{formatDateTime(d.created_at)}</span>
                    </div>
                    {d.error ? <p className="text-destructive">{d.error}</p> : null}
                    {d.response_body ? (
                      <pre className="scroll-slim max-h-40 overflow-auto rounded-md border bg-background p-2 font-mono text-[0.7rem] whitespace-pre-wrap break-all">
                        {d.response_body}
                      </pre>
                    ) : d.error ? null : (
                      <p className="text-muted-foreground">Empty response body.</p>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ) : null}
          </Fragment>
        ))}
      </TableBody>
    </Table>
  );
}

function SecretCard({ projectId, webhookId }: { projectId: string; webhookId: string }) {
  const secret = useWebhookSecret(projectId, webhookId);
  const [value, setValue] = useState<string | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);

  async function reveal() {
    if (value) return setValue(null);
    try {
      setValue((await secret.mutateAsync('reveal')).secret);
    } catch (err) {
      showError(err);
    }
  }
  async function rotate() {
    try {
      setValue((await secret.mutateAsync('rotate')).secret);
      setConfirmRotate(false);
      toast.success('Secret rotated. Update BRIDGE_WEBHOOK_SECRET on your server.');
    } catch (err) {
      showError(err);
    }
  }

  return (
    <SectionCard
      title="Signing secret"
      description="Every request carries a webhook-signature made with this secret."
      contentClassName="flex flex-col gap-4"
    >
      <div className="flex min-w-0 items-center gap-1 rounded-lg border bg-background py-1 pr-1 pl-3">
        <code className="min-w-0 flex-1 truncate font-mono text-xs">
          {value ?? 'whsec_••••••••••••••••••••••••••••••••'}
        </code>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={reveal}
          disabled={secret.isPending}
          aria-label={value ? 'Hide secret' : 'Reveal secret'}
          className="text-muted-foreground hover:text-foreground"
        >
          <HugeiconsIcon icon={value ? ViewOffSlashIcon : ViewIcon} strokeWidth={2} />
        </Button>
        {value ? <CopyButton value={value} label="Copy secret" /> : null}
      </div>
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">Reveals are recorded in the audit log.</p>
        <Button variant="outline" size="sm" onClick={() => setConfirmRotate(true)}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          Rotate
        </Button>
      </div>

      <Dialog open={confirmRotate} onOpenChange={setConfirmRotate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotate the signing secret?</DialogTitle>
            <DialogDescription>
              The current secret stops working at once. Requests your server receives from now on
              are signed with the new one, so update it right away.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmRotate(false)}>
              Cancel
            </Button>
            <Button onClick={rotate} disabled={secret.isPending}>
              {secret.isPending ? 'Rotating…' : 'Rotate secret'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SectionCard>
  );
}

export default function WebhookPage() {
  const projectId = useProjectId() ?? '';
  const { webhookId } = useParams<{ webhookId: string }>();
  const router = useRouter();
  const hook = useWebhook(projectId, webhookId);
  const update = useUpdateWebhook(projectId, webhookId);
  const remove = useDeleteWebhook(projectId);
  const test = useTestWebhook(projectId, webhookId);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const back = `/projects/${projectId}/webhooks`;

  async function setEnabled(enabled: boolean) {
    try {
      await update.mutateAsync({ enabled });
      toast.success(enabled ? 'Endpoint enabled' : 'Endpoint disabled');
    } catch (err) {
      showError(err);
    }
  }
  async function sendTest() {
    try {
      await test.mutateAsync();
      toast.success('Test event queued. It appears in the delivery log in a moment.');
    } catch (err) {
      showError(err);
    }
  }
  async function confirmDelete() {
    try {
      await remove.mutateAsync(webhookId);
      toast.success('Endpoint removed');
      router.push(back);
    } catch (err) {
      showError(err);
    }
  }

  if (hook.isPending) {
    return (
      <div className="flex flex-col gap-6">
        <Skeleton className="h-10 w-80" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (hook.isError) {
    return (
      <EmptyState
        title="Webhook not found"
        description={hook.error.message}
        action={
          <Button asChild variant="outline">
            <Link href={back}>Back to webhooks</Link>
          </Button>
        }
      />
    );
  }
  const e = hook.data;

  return (
    <div className="flex flex-col gap-6">
      <Link
        href={back}
        className="inline-flex w-fit items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} className="size-3.5" />
        Webhooks
      </Link>
      <PageHeader
        title={<span className="block truncate font-mono text-lg sm:text-xl">{e.url}</span>}
        subtitle={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <WebhookHealth endpoint={e} />
            {e.description ? <span>{e.description}</span> : null}
          </span>
        }
        className="min-w-0"
        actions={
          <>
            <Button onClick={sendTest} disabled={!e.enabled || test.isPending}>
              <HugeiconsIcon icon={SentIcon} strokeWidth={2} />
              {test.isPending ? 'Sending…' : 'Send test event'}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" aria-label="Endpoint actions">
                  <HugeiconsIcon icon={MoreHorizontalIcon} strokeWidth={2} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setEditOpen(true)}>
                  <HugeiconsIcon icon={PencilEdit02Icon} strokeWidth={2} className="size-4" />
                  Edit
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => void setEnabled(!e.enabled)}>
                  {e.enabled ? 'Disable' : 'Enable'}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={() => setDeleteOpen(true)}>
                  <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} className="size-4" />
                  Remove
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />

      {!e.enabled || e.failing_since ? (
        <div
          role="status"
          className={cn(
            'flex flex-col gap-3 rounded-xl border p-4 sm:flex-row sm:items-center sm:justify-between',
            e.enabled ? 'border-warning/40 bg-warning/8' : 'border-destructive/40 bg-destructive/8',
          )}
        >
          <p className="flex items-start gap-2 text-xs/relaxed">
            <HugeiconsIcon
              icon={Alert02Icon}
              strokeWidth={2}
              className={cn(
                'mt-0.5 size-4 shrink-0',
                e.enabled ? 'text-warning' : 'text-destructive',
              )}
            />
            {e.enabled
              ? `Every delivery has failed since ${formatDateTime(e.failing_since)}. Bridge keeps retrying and disables the endpoint after 5 days of failures.`
              : (e.disabled_reason ?? 'This endpoint is disabled. No events are sent to it.')}
          </p>
          {!e.enabled ? (
            <Button size="sm" onClick={() => setEnabled(true)} disabled={update.isPending}>
              Enable endpoint
            </Button>
          ) : null}
        </div>
      ) : null}

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]">
        <SectionCard
          title="Delivery attempts"
          description="Newest first. Click an attempt for the response."
          contentClassName="p-0"
        >
          <DeliveryLog projectId={projectId} webhookId={webhookId} />
        </SectionCard>
        <div className="flex min-w-0 flex-col gap-6">
          <SectionCard
            title="Details"
            action={
              <Button variant="ghost" size="sm" onClick={() => setEditOpen(true)}>
                Edit
              </Button>
            }
            contentClassName="flex flex-col gap-4 text-xs"
          >
            <div className="flex flex-col gap-1.5">
              <span className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                Events
              </span>
              <EventSummary events={e.events} />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                Endpoint ID
              </span>
              <CopyField value={e.id} />
            </div>
            <dl className="grid grid-cols-2 gap-3">
              <div>
                <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                  Last success
                </dt>
                <dd title={e.last_success_at ?? undefined}>{formatRelative(e.last_success_at)}</dd>
              </div>
              <div>
                <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                  Last failure
                </dt>
                <dd title={e.last_failure_at ?? undefined}>{formatRelative(e.last_failure_at)}</dd>
              </div>
            </dl>
          </SectionCard>
          <SecretCard projectId={projectId} webhookId={webhookId} />
        </div>
      </div>

      <SectionCard
        title="Verify requests"
        description="Reject any request whose signature does not match. Official Standard Webhooks libraries do it in one call."
      >
        <VerifyInstructions />
      </SectionCard>

      <WebhookFormDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        initial={e}
        title="Edit endpoint"
        submitLabel="Save"
        pending={update.isPending}
        onSubmit={async (v) => {
          try {
            await update.mutateAsync(v);
            setEditOpen(false);
            toast.success('Endpoint saved');
          } catch (err) {
            showError(err);
          }
        }}
      />
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove this endpoint?</DialogTitle>
            <DialogDescription>
              Bridge stops sending events to <span className="font-mono">{e.url}</span> and cancels
              pending retries. Its delivery log is deleted.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmDelete} disabled={remove.isPending}>
              {remove.isPending ? 'Removing…' : 'Remove endpoint'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
