'use client';

import type {
  CreatedForwardingRule,
  ForwardingDelivery,
  ForwardingDestination,
  ForwardingDestinationInput,
  ForwardingDestinationType,
  ForwardingRule,
} from '@bridge/api-types';
import {
  Alert02Icon,
  ArrowDown01Icon,
  ArrowUp01Icon,
  Delete02Icon,
  Key01Icon,
  Mail01Icon,
  PencilEdit02Icon,
  PlusSignIcon,
  SmartPhone01Icon,
  WebhookIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { CopyButton } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { SectionCard } from '@/components/kit/section-card';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan } from '@/components/layout/console-context';
import { FieldNote } from '@/components/messaging';
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
import { Switch } from '@/components/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { VerifyInstructions } from '@/components/webhooks';
import { BridgeApiError } from '@/lib/api';
import { fieldErrors, showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  useCreateForwardingRule,
  useDeleteForwardingRule,
  useForwardingDeliveries,
  useForwardingRules,
  useForwardingSecret,
  useUpdateForwardingRule,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

const TYPES: { value: ForwardingDestinationType; label: string }[] = [
  { value: 'phone', label: 'Phone number' },
  { value: 'telegram', label: 'Telegram' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'email', label: 'Email' },
];

const FORMATS = [
  { value: 'json', label: 'JSON (Bridge format)' },
  { value: 'slack', label: 'Slack incoming webhook' },
  { value: 'discord', label: 'Discord webhook' },
] as const;

const DELIVERY: Record<ForwardingDelivery['status'], { kind: StatusKind; label: string }> = {
  pending: { kind: 'neutral', label: 'Pending' },
  retrying: { kind: 'warning', label: 'Retrying' },
  succeeded: { kind: 'success', label: 'Delivered' },
  failed: { kind: 'danger', label: 'Failed' },
  skipped: { kind: 'neutral', label: 'Skipped' },
};

function TelegramGlyph({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden className={className}>
      <path
        d="M21 4.5 2.9 11.4c-.9.4-.9 1.3 0 1.6l4.6 1.4 1.8 5.4c.2.6.9.7 1.4.3l2.6-2.3 4.6 3.4c.6.4 1.4.1 1.6-.7L22.4 5.6c.2-.9-.5-1.5-1.4-1.1Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path d="m7.5 14.4 9.8-6.3-6.8 7.5" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  );
}

function DestinationIcon({ type }: { type: ForwardingDestinationType }) {
  if (type === 'telegram') return <TelegramGlyph className="size-3.5 shrink-0" />;
  const icon = type === 'phone' ? SmartPhone01Icon : type === 'email' ? Mail01Icon : WebhookIcon;
  return <HugeiconsIcon icon={icon} strokeWidth={2} className="size-3.5 shrink-0" />;
}

function hostOf(url: string | null): string {
  if (!url) return '';
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/** One line per destination, for lists. */
export function destinationLabel(d: ForwardingDestination): string {
  switch (d.type) {
    case 'phone':
      return d.to ?? '';
    case 'email':
      return d.to ?? '';
    case 'telegram':
      return `Telegram ${d.chat_id ?? ''}`;
    case 'webhook':
      return `${hostOf(d.url)}${d.format && d.format !== 'json' ? ` (${d.format === 'slack' ? 'Slack' : 'Discord'})` : ''}`;
  }
}

function matchSummary(r: ForwardingRule): string {
  const from = r.match.senders.length ? `From ${r.match.senders.join(', ')}` : 'Every sender';
  return r.match.contains ? `${from}, containing “${r.match.contains}”` : from;
}

export function Forwarding({ projectId }: { projectId: string }) {
  const canAdmin = useCan('admin');
  const rules = useForwardingRules(projectId);
  const [editing, setEditing] = useState<ForwardingRule | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [created, setCreated] = useState<CreatedForwardingRule | null>(null);
  const [removing, setRemoving] = useState<ForwardingRule | null>(null);
  const data = rules.data;

  return (
    <SectionCard
      title="Forwarding"
      description="Copy incoming SMS to another phone, a Telegram chat, a webhook (JSON, Slack or Discord) or an email address."
      contentClassName="p-0"
      action={
        canAdmin ? (
          <Button
            onClick={() => {
              setEditing(null);
              setFormOpen(true);
            }}
          >
            <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
            Add rule
          </Button>
        ) : undefined
      }
    >
      <p className="border-b px-5 py-3 text-xs/relaxed text-muted-foreground">
        Rules see the SMS your phones receive, so turn on Forward incoming SMS in each phone&apos;s
        settings on{' '}
        <Link href={`/projects/${projectId}/devices`} className="text-primary hover:underline">
          Devices
        </Link>
        . Messages Bridge forwarded to a phone are not forwarded again.
      </p>
      {rules.isPending ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : rules.isError || !data ? (
        <EmptyState
          title="Could not load forwarding rules"
          description={rules.error?.message}
          action={
            <Button variant="outline" onClick={() => rules.refetch()}>
              Try again
            </Button>
          }
        />
      ) : data.data.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={WebhookIcon} strokeWidth={1.8} />}
          title="No forwarding rules"
          description="Forward bank alerts to Telegram, delivery codes to a colleague's phone, or every SMS to your own service."
        />
      ) : (
        <ul className="divide-y">
          {data.data.map((r) => (
            <RuleRow
              key={r.id}
              projectId={projectId}
              rule={r}
              onEdit={() => {
                setEditing(r);
                setFormOpen(true);
              }}
              onRemove={() => setRemoving(r)}
            />
          ))}
        </ul>
      )}

      <RuleDialog
        projectId={projectId}
        open={formOpen}
        rule={editing}
        emailAvailable={data?.email_available ?? false}
        telegramAvailable={data?.telegram_available ?? false}
        onOpenChange={setFormOpen}
        onCreated={(c) => {
          if (c.destinations.some((d) => d.type === 'webhook')) setCreated(c);
        }}
      />
      <SecretDialog
        secret={created?.signing_secret ?? null}
        title="Rule added"
        onClose={() => setCreated(null)}
      />
      <DeleteDialog projectId={projectId} rule={removing} onClose={() => setRemoving(null)} />
    </SectionCard>
  );
}

function LastDelivery({ projectId, ruleId }: { projectId: string; ruleId: string }) {
  const q = useForwardingDeliveries(projectId, ruleId, 50);
  const last = q.data?.[0];
  if (q.isPending) return <Skeleton className="h-4 w-24" />;
  if (!last) return <span className="text-xs text-muted-foreground">No deliveries yet</span>;
  const s = DELIVERY[last.status];
  return (
    <span className="flex flex-wrap items-center gap-x-2 text-xs">
      <StatusBadge kind={s.kind}>{s.label}</StatusBadge>
      <span className="text-muted-foreground" title={formatDateTime(last.updated_at)}>
        {formatRelative(last.updated_at)}
      </span>
    </span>
  );
}

function RuleRow({
  projectId,
  rule: r,
  onEdit,
  onRemove,
}: {
  projectId: string;
  rule: ForwardingRule;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const canAdmin = useCan('admin');
  const update = useUpdateForwardingRule(projectId);
  const reveal = useForwardingSecret(projectId);
  const [expanded, setExpanded] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const hasWebhook = r.destinations.some((d) => d.type === 'webhook');

  async function toggle(enabled: boolean) {
    try {
      await update.mutateAsync({ ruleId: r.id, enabled });
      toast.success(enabled ? `${r.name} turned on` : `${r.name} turned off`);
    } catch (err) {
      showError(err);
    }
  }

  async function showSecret() {
    try {
      setSecret((await reveal.mutateAsync(r.id)).signing_secret);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <li className="flex flex-col gap-3 px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <Switch
            checked={r.enabled}
            onCheckedChange={toggle}
            disabled={!canAdmin || update.isPending}
            aria-label={`Forwarding rule ${r.name}`}
            className="mt-1"
          />
          <div className="flex min-w-0 flex-col gap-1.5">
            <span className={cn('text-sm font-medium', !r.enabled && 'text-muted-foreground')}>
              {r.name}
            </span>
            <span className="text-xs text-muted-foreground">{matchSummary(r)}</span>
            <span className="flex flex-wrap gap-1.5">
              {r.destinations.map((d) => (
                <span
                  key={d.id}
                  className="inline-flex max-w-full items-center gap-1.5 rounded-md border bg-background px-2 py-0.5 text-xs text-muted-foreground"
                >
                  <DestinationIcon type={d.type} />
                  <span className="truncate font-mono text-[0.7rem]">{destinationLabel(d)}</span>
                </span>
              ))}
            </span>
          </div>
        </div>
        <div className="flex flex-col items-end gap-2">
          <LastDelivery projectId={projectId} ruleId={r.id} />
          <div className="flex flex-wrap justify-end gap-1">
            <Button variant="ghost" size="sm" onClick={() => setExpanded((x) => !x)}>
              <HugeiconsIcon icon={expanded ? ArrowUp01Icon : ArrowDown01Icon} strokeWidth={2} />
              Deliveries
            </Button>
            {canAdmin && hasWebhook ? (
              <Button variant="ghost" size="sm" onClick={showSecret} disabled={reveal.isPending}>
                <HugeiconsIcon icon={Key01Icon} strokeWidth={2} />
                Signing secret
              </Button>
            ) : null}
            {canAdmin ? (
              <>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Edit ${r.name}`}
                  onClick={onEdit}
                >
                  <HugeiconsIcon icon={PencilEdit02Icon} strokeWidth={2} />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Delete ${r.name}`}
                  className="text-muted-foreground hover:text-destructive"
                  onClick={onRemove}
                >
                  <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                </Button>
              </>
            ) : null}
          </div>
        </div>
      </div>
      {expanded ? <Deliveries projectId={projectId} rule={r} /> : null}
      <SecretDialog secret={secret} title="Signing secret" onClose={() => setSecret(null)} />
    </li>
  );
}

function Deliveries({ projectId, rule }: { projectId: string; rule: ForwardingRule }) {
  const q = useForwardingDeliveries(projectId, rule.id, 50);
  const byId = new Map(rule.destinations.map((d) => [d.id, d]));
  if (q.isPending) return <Skeleton className="h-20 w-full" />;
  if (q.isError) return <p className="text-xs text-destructive">{q.error.message}</p>;
  if (!q.data.length) {
    return (
      <p className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        Nothing forwarded yet. Deliveries appear here when a matching SMS arrives.
      </p>
    );
  }
  return (
    <div className="overflow-hidden rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-3">When</TableHead>
            <TableHead>Destination</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">Attempts</TableHead>
            <TableHead className="pr-3">Detail</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {q.data.map((d) => {
            const dest = byId.get(d.destination_id);
            const s = DELIVERY[d.status];
            return (
              <TableRow key={d.id}>
                <TableCell
                  className="pl-3 text-muted-foreground"
                  title={formatDateTime(d.created_at)}
                >
                  {formatRelative(d.created_at)}
                </TableCell>
                <TableCell className="max-w-[14rem]">
                  <span className="flex items-center gap-1.5 text-xs">
                    <DestinationIcon type={d.destination_type} />
                    <span className="truncate font-mono text-[0.7rem]">
                      {dest ? destinationLabel(dest) : 'Removed destination'}
                    </span>
                  </span>
                </TableCell>
                <TableCell>
                  <StatusBadge kind={s.kind}>{s.label}</StatusBadge>
                </TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">
                  {d.attempts}
                </TableCell>
                <TableCell className="max-w-[18rem] pr-3 text-xs">
                  {d.error ? (
                    <span className="block truncate text-destructive" title={d.error}>
                      {d.error}
                    </span>
                  ) : d.response_status ? (
                    <span className="font-mono text-muted-foreground">
                      HTTP {d.response_status}
                    </span>
                  ) : d.forwarded_message_id ? (
                    <span className="font-mono text-muted-foreground">
                      {d.forwarded_message_id}
                    </span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      <p className="border-t px-3 py-2 text-xs text-muted-foreground">
        Failed deliveries are retried for about four hours.
      </p>
    </div>
  );
}

// ---- create and edit ---------------------------------------------------------

type DestDraft = {
  key: string;
  id?: string;
  type: ForwardingDestinationType;
  to: string;
  chatId: string;
  botToken: string;
  botTokenSet: boolean;
  url: string;
  format: 'json' | 'slack' | 'discord';
};

let draftSeq = 0;
function nextKey(): string {
  draftSeq += 1;
  return `d${draftSeq}`;
}
const newDraft = (type: ForwardingDestinationType = 'webhook'): DestDraft => ({
  key: nextKey(),
  type,
  to: '',
  chatId: '',
  botToken: '',
  botTokenSet: false,
  url: '',
  format: 'json',
});

function fromDestination(d: ForwardingDestination): DestDraft {
  return {
    ...newDraft(d.type),
    id: d.id,
    to: d.to ?? '',
    chatId: d.chat_id ?? '',
    botTokenSet: d.bot_token_set,
    url: d.url ?? '',
    format: d.format ?? 'json',
  };
}

function toInput(d: DestDraft): ForwardingDestinationInput {
  const out: ForwardingDestinationInput = { type: d.type };
  if (d.id) out.id = d.id;
  if (d.type === 'phone' || d.type === 'email') out.to = d.to.trim();
  if (d.type === 'telegram') {
    out.chat_id = d.chatId.trim();
    if (d.botToken.trim()) out.bot_token = d.botToken.trim();
  }
  if (d.type === 'webhook') {
    out.url = d.url.trim();
    out.format = d.format;
  }
  return out;
}

function RuleDialog({
  projectId,
  open,
  rule,
  emailAvailable,
  telegramAvailable,
  onOpenChange,
  onCreated,
}: {
  projectId: string;
  open: boolean;
  rule: ForwardingRule | null;
  emailAvailable: boolean;
  telegramAvailable: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (r: CreatedForwardingRule) => void;
}) {
  const create = useCreateForwardingRule(projectId);
  const update = useUpdateForwardingRule(projectId);
  const [name, setName] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [senders, setSenders] = useState('');
  const [contains, setContains] = useState('');
  const [dests, setDests] = useState<DestDraft[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState('');

  useEffect(() => {
    if (!open) return;
    setName(rule?.name ?? '');
    setEnabled(rule?.enabled ?? true);
    setSenders(rule?.match.senders.join(', ') ?? '');
    setContains(rule?.match.contains ?? '');
    setDests(rule ? rule.destinations.map(fromDestination) : [newDraft()]);
    setErrors({});
    setFormError('');
  }, [open, rule]);

  const pending = create.isPending || update.isPending;
  const patch = (key: string, change: Partial<DestDraft>) => {
    setDests((list) => list.map((d) => (d.key === key ? { ...d, ...change } : d)));
    setErrors({});
    setFormError('');
  };

  async function submit(e: FormEvent) {
    e.preventDefault();
    setErrors({});
    setFormError('');
    const match = {
      senders: senders
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      ...(contains.trim() ? { contains: contains.trim() } : {}),
    };
    const destinations = dests.map(toInput);
    try {
      if (rule) {
        await update.mutateAsync({
          ruleId: rule.id,
          name: name.trim(),
          enabled,
          match,
          destinations,
        });
        toast.success('Rule saved');
      } else {
        const c = await create.mutateAsync({ name: name.trim(), enabled, match, destinations });
        toast.success('Forwarding rule added');
        onCreated(c);
      }
      onOpenChange(false);
    } catch (err) {
      const fe = fieldErrors(err);
      if (Object.keys(fe).length) setErrors(fe);
      else if (err instanceof BridgeApiError && err.status === 409) setFormError(err.message);
      else showError(err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !pending && onOpenChange(o)}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{rule ? 'Edit forwarding rule' : 'Add forwarding rule'}</DialogTitle>
            <DialogDescription>
              Each incoming SMS that matches goes to every destination. Leave the filters empty to
              forward everything.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-2">
            <Label htmlFor="fwd-name">Name</Label>
            <Input
              id="fwd-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Bank alerts to Telegram"
              maxLength={60}
              required
              aria-invalid={errors.name ? true : undefined}
            />
            <FieldNote error={errors.name} />
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="fwd-senders">
                From <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="fwd-senders"
                value={senders}
                onChange={(e) => setSenders(e.target.value)}
                placeholder="+9198*, AX-HDFCBK"
                className="font-mono"
                aria-invalid={errors['match.senders'] ? true : undefined}
              />
              <FieldNote error={errors['match.senders']}>
                Separate with commas. End with * to match a prefix.
              </FieldNote>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="fwd-contains">
                Containing <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="fwd-contains"
                value={contains}
                onChange={(e) => setContains(e.target.value)}
                placeholder="OTP"
                maxLength={100}
                aria-invalid={errors['match.contains'] ? true : undefined}
              />
              <FieldNote error={errors['match.contains']}>Ignoring case.</FieldNote>
            </div>
          </div>

          <fieldset className="flex flex-col gap-3">
            <legend className="mb-2 text-sm font-medium">Destinations</legend>
            {dests.map((d, i) => (
              <DestinationEditor
                key={d.key}
                index={i}
                draft={d}
                errors={errors}
                emailAvailable={emailAvailable}
                telegramAvailable={telegramAvailable}
                canRemove={dests.length > 1}
                onChange={(change) => patch(d.key, change)}
                onRemove={() => setDests((list) => list.filter((x) => x.key !== d.key))}
              />
            ))}
            {errors.destinations ? <FieldNote error={errors.destinations} /> : null}
            {!emailAvailable ? (
              <p className="text-xs/relaxed text-muted-foreground">
                Email is not set up on this server. To use it, set{' '}
                <code className="font-mono">BRIDGE_SMTP_HOST</code> and{' '}
                <code className="font-mono">BRIDGE_SMTP_FROM</code> on the Bridge server and restart
                it.
              </p>
            ) : null}
            {dests.length < 5 ? (
              <Button
                type="button"
                variant="outline"
                className="self-start"
                onClick={() => setDests((list) => [...list, newDraft('phone')])}
              >
                <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
                Add destination
              </Button>
            ) : (
              <p className="text-xs text-muted-foreground">A rule can have up to 5 destinations.</p>
            )}
          </fieldset>

          <div className="flex items-center gap-2.5">
            <Switch id="fwd-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="fwd-enabled" className="text-sm font-normal">
              Enabled
            </Label>
          </div>

          {formError ? (
            <p className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
              <HugeiconsIcon
                icon={Alert02Icon}
                strokeWidth={2}
                className="mt-0.5 size-3.5 shrink-0 text-destructive"
              />
              {formError}
            </p>
          ) : null}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !name.trim() || dests.length === 0}>
              {pending ? 'Saving…' : rule ? 'Save rule' : 'Add rule'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DestinationEditor({
  index,
  draft: d,
  errors,
  emailAvailable,
  telegramAvailable,
  canRemove,
  onChange,
  onRemove,
}: {
  index: number;
  draft: DestDraft;
  errors: Record<string, string>;
  emailAvailable: boolean;
  telegramAvailable: boolean;
  canRemove: boolean;
  onChange: (change: Partial<DestDraft>) => void;
  onRemove: () => void;
}) {
  const err = (field: string) => errors[`destinations[${index}].${field}`];
  const id = `fwd-dest-${index}`;
  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-background p-3">
      <div className="flex items-end gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <Label htmlFor={`${id}-type`} className="text-xs text-muted-foreground">
            Destination {index + 1}
          </Label>
          <Select
            value={d.type}
            onValueChange={(v) =>
              onChange({ type: v as ForwardingDestinationType, id: undefined, botTokenSet: false })
            }
          >
            <SelectTrigger id={`${id}-type`} className="w-full sm:w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TYPES.map((t) => (
                <SelectItem
                  key={t.value}
                  value={t.value}
                  disabled={
                    (t.value === 'email' && !emailAvailable && d.type !== 'email') ||
                    (t.value === 'telegram' && !telegramAvailable && d.type !== 'telegram')
                  }
                >
                  {t.label}
                  {t.value === 'email' && !emailAvailable ? ' (not set up)' : ''}
                  {t.value === 'telegram' && !telegramAvailable ? ' (not set up)' : ''}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {canRemove ? (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={`Remove destination ${index + 1}`}
            className="text-muted-foreground hover:text-destructive"
            onClick={onRemove}
          >
            <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
          </Button>
        ) : null}
      </div>

      {d.type === 'phone' ? (
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${id}-to`}>Phone number</Label>
          <Input
            id={`${id}-to`}
            value={d.to}
            onChange={(e) => onChange({ to: e.target.value })}
            placeholder="+919876543210"
            className="font-mono"
            required
            aria-invalid={err('to') ? true : undefined}
          />
          <FieldNote error={err('to')}>
            Bridge sends an SMS with the sender and text, through your live routing.
          </FieldNote>
        </div>
      ) : null}

      {d.type === 'email' ? (
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${id}-to`}>Email address</Label>
          <Input
            id={`${id}-to`}
            type="email"
            value={d.to}
            onChange={(e) => onChange({ to: e.target.value })}
            placeholder="alerts@example.com"
            disabled={!emailAvailable}
            required
            aria-invalid={err('to') ? true : undefined}
          />
          <FieldNote error={err('to')} />
        </div>
      ) : null}

      {d.type === 'telegram' ? (
        <div className="flex flex-col gap-3">
          {!telegramAvailable ? (
            <p className="text-xs/relaxed text-warning">
              This server cannot store bot tokens yet. Set{' '}
              <code className="font-mono">BRIDGE_SECRET_KEY</code> and restart Bridge.
            </p>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor={`${id}-token`}>Bot token</Label>
              <Input
                id={`${id}-token`}
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={d.botToken}
                onChange={(e) => onChange({ botToken: e.target.value })}
                placeholder={
                  d.botTokenSet ? 'Stored. Paste a new one to replace it.' : '123456:ABC-DEF…'
                }
                className="font-mono"
                required={!d.botTokenSet}
                aria-invalid={err('bot_token') ? true : undefined}
              />
              <FieldNote error={err('bot_token')}>From @BotFather. Stored encrypted.</FieldNote>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor={`${id}-chat`}>Chat ID</Label>
              <Input
                id={`${id}-chat`}
                value={d.chatId}
                onChange={(e) => onChange({ chatId: e.target.value })}
                placeholder="-1001234567890"
                className="font-mono"
                required
                aria-invalid={err('chat_id') ? true : undefined}
              />
              <FieldNote error={err('chat_id')} />
            </div>
          </div>
          <p className="text-xs/relaxed text-muted-foreground">
            To find the chat ID, send your bot a message (or add it to the group), then open{' '}
            <code className="break-all font-mono">
              api.telegram.org/bot&lt;token&gt;/getUpdates
            </code>{' '}
            and copy <code className="font-mono">chat.id</code>. For a public channel, add the bot
            as an admin and use the channel&apos;s @username.
          </p>
        </div>
      ) : null}

      {d.type === 'webhook' ? (
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]">
          <div className="flex flex-col gap-2">
            <Label htmlFor={`${id}-url`}>URL</Label>
            <Input
              id={`${id}-url`}
              type="url"
              value={d.url}
              onChange={(e) => onChange({ url: e.target.value })}
              placeholder="https://example.com/sms"
              className="font-mono"
              required
              aria-invalid={err('url') ? true : undefined}
            />
            <FieldNote error={err('url')} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor={`${id}-format`}>Format</Label>
            <Select
              value={d.format}
              onValueChange={(v) => onChange({ format: v as DestDraft['format'] })}
            >
              <SelectTrigger id={`${id}-format`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {FORMATS.map((f) => (
                  <SelectItem key={f.value} value={f.value}>
                    {f.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <p className="text-xs/relaxed text-muted-foreground sm:col-span-2">
            {d.format === 'json'
              ? 'Posts the message as JSON, the same shape as message.received webhook data. '
              : `Posts the sender and text as a ${d.format === 'slack' ? 'Slack' : 'Discord'} message. `}
            Every request is signed with the rule&apos;s signing secret.
          </p>
        </div>
      ) : null}
    </div>
  );
}

function SecretDialog({
  secret,
  title,
  onClose,
}: {
  secret: string | null;
  title: string;
  onClose: () => void;
}) {
  return (
    <Dialog open={secret !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Webhook destinations in JSON format are signed with this secret, the same way as Bridge
            webhooks. Check the signature before trusting a request.
          </DialogDescription>
        </DialogHeader>
        {secret ? (
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 break-all font-mono text-xs">{secret}</code>
              <CopyButton value={secret} label="Copy secret" showLabel />
            </div>
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

function DeleteDialog({
  projectId,
  rule,
  onClose,
}: {
  projectId: string;
  rule: ForwardingRule | null;
  onClose: () => void;
}) {
  const remove = useDeleteForwardingRule(projectId);
  async function confirm() {
    if (!rule) return;
    try {
      await remove.mutateAsync(rule.id);
      toast.success('Rule deleted');
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={rule !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete “{rule?.name}”?</DialogTitle>
          <DialogDescription>
            Incoming SMS stop being forwarded at once, and deliveries still waiting are dropped.
            Turning the rule off keeps it for later.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Deleting…' : 'Delete rule'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
