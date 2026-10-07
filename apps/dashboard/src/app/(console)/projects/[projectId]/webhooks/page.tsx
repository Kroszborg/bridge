'use client';

import type { CreatedWebhookEndpoint } from '@bridge/api-types';
import { Alert02Icon, PlusSignIcon, WebhookIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyButton } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
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
  EVENT_TYPES,
  EventSummary,
  VerifyInstructions,
  WebhookFormDialog,
  WebhookHealth,
} from '@/components/webhooks';
import { showError } from '@/lib/errors';
import { formatRelative } from '@/lib/format';
import { useCreateWebhook, useWebhooks } from '@/lib/queries';

const samplePayload = `POST /webhooks/bridge
webhook-id: evt_01jb2c4x8m3n5p7q9r1s3t5v7w
webhook-timestamp: 1730118400
webhook-signature: v1,K5oZfzN95Z9UVu1EsfQmfVNQhnkZ2pj9o9NDN/H/pI4=

{
  "type": "message.delivered",
  "timestamp": "2026-10-05T09:20:00Z",
  "data": {
    "id": "msg_01jb2c4w2h6k8m0p2r4t6v8x0z",
    "status": "delivered",
    "direction": "outbound",
    "to": "+919876543210",
    "body": "Your order has shipped.",
    "device_id": "dev_01jb2b9q3s5u7w9y1a3c5e7g9j",
    "metadata": { "order_id": "ORD-2291" },
    "delivered_at": "2026-10-05T09:20:00Z"
  }
}`;

function SecretDialog({
  endpoint,
  onClose,
}: {
  endpoint: CreatedWebhookEndpoint | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={endpoint !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Endpoint added</DialogTitle>
          <DialogDescription>
            Verify every request with this signing secret before trusting it. You can reveal or
            rotate it later on the endpoint&apos;s page.
          </DialogDescription>
        </DialogHeader>
        {endpoint ? (
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 break-all font-mono text-xs">{endpoint.secret}</code>
              <CopyButton value={endpoint.secret} label="Copy secret" showLabel />
            </div>
            <p className="flex items-start gap-2 text-xs/relaxed text-muted-foreground">
              <HugeiconsIcon
                icon={Alert02Icon}
                strokeWidth={2}
                className="mt-0.5 size-3.5 shrink-0 text-warning"
              />
              Store it as BRIDGE_WEBHOOK_SECRET on the server that receives the webhooks.
            </p>
            <VerifyInstructions />
          </div>
        ) : null}
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function WebhooksPage() {
  const projectId = useProjectId() ?? '';
  const router = useRouter();
  const hooks = useWebhooks(projectId);
  const create = useCreateWebhook(projectId);
  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<CreatedWebhookEndpoint | null>(null);

  const canAdmin = useCan('admin');
  const addButton = !canAdmin ? undefined : (
    <Button size="lg" onClick={() => setCreateOpen(true)}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
      Add endpoint
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Webhooks"
        subtitle="Bridge sends signed events to your endpoints and retries failures for about 3 days."
        actions={hooks.data?.length ? addButton : undefined}
      />

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {hooks.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : hooks.isError ? (
          <EmptyState
            title="Could not load webhooks"
            description={hooks.error.message}
            action={
              <Button variant="outline" onClick={() => hooks.refetch()}>
                Try again
              </Button>
            }
          />
        ) : hooks.data.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={WebhookIcon} strokeWidth={1.8} />}
            title="No endpoints yet"
            description="Add an endpoint to hear about deliveries, failures and incoming SMS as they happen, instead of polling the API."
            action={addButton}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Endpoint</TableHead>
                <TableHead>Events</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="pr-5 text-right">Last delivery</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {hooks.data.map((e) => {
                const last =
                  [e.last_success_at, e.last_failure_at].filter(Boolean).sort().at(-1) ?? null;
                const href = `/projects/${projectId}/webhooks/${e.id}`;
                return (
                  <TableRow key={e.id} className="cursor-pointer" onClick={() => router.push(href)}>
                    <TableCell className="max-w-[24rem] pl-5">
                      <Link
                        href={href}
                        className="block truncate font-mono text-xs font-medium hover:text-primary"
                        onClick={(ev) => ev.stopPropagation()}
                      >
                        {e.url}
                      </Link>
                      {e.description ? (
                        <span className="block truncate text-xs text-muted-foreground">
                          {e.description}
                        </span>
                      ) : null}
                    </TableCell>
                    <TableCell className="max-w-[20rem]">
                      <EventSummary events={e.events} />
                    </TableCell>
                    <TableCell>
                      <WebhookHealth endpoint={e} />
                    </TableCell>
                    <TableCell
                      className="pr-5 text-right text-muted-foreground"
                      title={last ?? undefined}
                    >
                      {formatRelative(last)}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <SectionCard
          title="What Bridge sends"
          description="Standard Webhooks format: one JSON event per request, signed with HMAC-SHA256."
          contentClassName="flex flex-col gap-3"
        >
          <CodeBlock language="http" code={samplePayload} />
        </SectionCard>
        <SectionCard title="Events" contentClassName="p-0">
          <ul className="divide-y">
            {EVENT_TYPES.map((e) => (
              <li key={e.type} className="flex flex-col gap-0.5 px-5 py-3">
                <span className="font-mono text-xs font-medium">{e.type}</span>
                <span className="text-xs text-muted-foreground">{e.description}</span>
              </li>
            ))}
          </ul>
          <p className="border-t px-5 py-3 text-xs/relaxed text-muted-foreground">
            Respond with any 2xx within 15 seconds. Bridge retries other responses after 5 s, 5 min,
            30 min, 2 h, 5 h, then every 10 h, and disables an endpoint that has failed for 5 days.
            Events can arrive more than once or out of order: de-duplicate on{' '}
            <code className="font-mono">webhook-id</code>.
          </p>
        </SectionCard>
      </div>

      <WebhookFormDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        title="Add webhook endpoint"
        submitLabel="Add endpoint"
        pending={create.isPending}
        onSubmit={async (v) => {
          try {
            const e = await create.mutateAsync(v);
            setCreateOpen(false);
            setCreated(e);
          } catch (err) {
            showError(err);
          }
        }}
      />
      <SecretDialog
        endpoint={created}
        onClose={() => {
          const id = created?.id;
          setCreated(null);
          if (id) router.push(`/projects/${projectId}/webhooks/${id}`);
        }}
      />
    </div>
  );
}
