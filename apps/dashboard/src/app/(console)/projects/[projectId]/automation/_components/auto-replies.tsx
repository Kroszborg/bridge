'use client';

import type { AutoReplyRule } from '@bridge/api-types';
import {
  Delete02Icon,
  MessageMultiple01Icon,
  PencilEdit02Icon,
  PlusSignIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { EmptyState } from '@/components/kit/empty-state';
import { SectionCard } from '@/components/kit/section-card';
import { useCan } from '@/components/layout/console-context';
import { FieldNote, SegmentLine } from '@/components/messaging';
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
import { Textarea } from '@/components/ui/textarea';
import { BridgeApiError } from '@/lib/api';
import { fieldErrors, showError } from '@/lib/errors';
import {
  useAutoReplies,
  useCreateAutoReply,
  useDeleteAutoReply,
  useUpdateAutoReply,
} from '@/lib/queries';

type Match = AutoReplyRule['match'];
type Action = AutoReplyRule['action'];

const MATCHES: { value: Match; label: string; hint: string }[] = [
  { value: 'exact', label: 'Whole message', hint: 'The message is exactly a keyword.' },
  { value: 'starts_with', label: 'Starts with', hint: 'The message begins with a keyword.' },
  { value: 'contains', label: 'Contains', hint: 'A keyword appears anywhere in it.' },
];

const ACTIONS: { value: Action; label: string; hint: string }[] = [
  { value: 'none', label: 'Reply only', hint: 'Send the reply, change nothing else.' },
  {
    value: 'opt_out',
    label: 'Opt the sender out',
    hint: 'Add them to the opt-out list. Messages to them are refused.',
  },
  {
    value: 'opt_in',
    label: 'Opt the sender back in',
    hint: 'Remove them from the opt-out list.',
  },
];

const ACTION_LABEL: Record<Action, string> = {
  none: 'Reply only',
  opt_out: 'Opt out',
  opt_in: 'Opt back in',
};

export function AutoReplies({ projectId }: { projectId: string }) {
  const canAdmin = useCan('admin');
  const rules = useAutoReplies(projectId);
  const update = useUpdateAutoReply(projectId);
  const [editing, setEditing] = useState<AutoReplyRule | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [removing, setRemoving] = useState<AutoReplyRule | null>(null);
  const sorted = [...(rules.data ?? [])].sort((a, b) => a.priority - b.priority);
  const nextPriority = sorted.length ? Math.min(10_000, (sorted.at(-1)?.priority ?? 0) + 10) : 100;

  async function toggle(r: AutoReplyRule, enabled: boolean) {
    try {
      await update.mutateAsync({ ruleId: r.id, enabled });
      toast.success(enabled ? `${r.name} turned on` : `${r.name} turned off`);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <SectionCard
      title="Auto-replies"
      description="Rules that answer incoming SMS by keyword. Bridge tries enabled rules in order, lowest number first, and runs only the first match."
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
      {rules.isPending ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : rules.isError ? (
        <EmptyState
          title="Could not load auto-reply rules"
          description={rules.error.message}
          action={
            <Button variant="outline" onClick={() => rules.refetch()}>
              Try again
            </Button>
          }
        />
      ) : sorted.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={MessageMultiple01Icon} strokeWidth={1.8} />}
          title="No rules"
          description="Without rules, STOP and other keywords are not handled. Add a rule to opt senders out when they ask."
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-16 pl-5">Order</TableHead>
              <TableHead>Rule</TableHead>
              <TableHead>Match</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Reply</TableHead>
              <TableHead className="w-20 pr-5">
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.map((r) => (
              <TableRow key={r.id} className={r.enabled ? undefined : 'text-muted-foreground'}>
                <TableCell className="pl-5 tabular-nums text-muted-foreground">
                  {r.priority}
                </TableCell>
                <TableCell className="max-w-[16rem]">
                  <div className="flex items-start gap-3">
                    <Switch
                      checked={r.enabled}
                      onCheckedChange={(v) => toggle(r, v)}
                      disabled={!canAdmin || update.isPending}
                      aria-label={`Rule ${r.name}`}
                      className="mt-0.5"
                    />
                    <div className="min-w-0">
                      <span className="block truncate font-medium text-foreground">{r.name}</span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {r.keywords.join(', ')}
                      </span>
                    </div>
                  </div>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {MATCHES.find((m) => m.value === r.match)?.label}
                </TableCell>
                <TableCell>{ACTION_LABEL[r.action]}</TableCell>
                <TableCell className="max-w-[20rem] truncate text-muted-foreground">
                  {r.reply ?? <span className="italic">No reply</span>}
                </TableCell>
                <TableCell className="pr-5 text-right">
                  {canAdmin ? (
                    <span className="inline-flex gap-0.5">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Edit ${r.name}`}
                        onClick={() => {
                          setEditing(r);
                          setFormOpen(true);
                        }}
                      >
                        <HugeiconsIcon icon={PencilEdit02Icon} strokeWidth={2} />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Delete ${r.name}`}
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setRemoving(r)}
                      >
                        <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                      </Button>
                    </span>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <ul className="flex list-disc flex-col gap-1 border-t py-3 pr-5 pl-9 text-xs/relaxed text-muted-foreground">
        <li>Keywords are compared with the trimmed message, ignoring case.</li>
        <li>
          Replies go back through the phone that received the SMS. To prevent loops, each rule
          answers a number at most once every 10 minutes, and never answers sender IDs or short
          codes.
        </li>
        <li>Opt-out and opt-in actions still run when the reply is held back.</li>
      </ul>

      <RuleDialog
        projectId={projectId}
        open={formOpen}
        rule={editing}
        nextPriority={nextPriority}
        onOpenChange={setFormOpen}
      />
      <DeleteRuleDialog projectId={projectId} rule={removing} onClose={() => setRemoving(null)} />
    </SectionCard>
  );
}

function RuleDialog({
  projectId,
  open,
  rule,
  nextPriority,
  onOpenChange,
}: {
  projectId: string;
  open: boolean;
  rule: AutoReplyRule | null;
  nextPriority: number;
  onOpenChange: (open: boolean) => void;
}) {
  const create = useCreateAutoReply(projectId);
  const update = useUpdateAutoReply(projectId);
  const [name, setName] = useState('');
  const [keywords, setKeywords] = useState('');
  const [match, setMatch] = useState<Match>('exact');
  const [action, setAction] = useState<Action>('none');
  const [reply, setReply] = useState('');
  const [priority, setPriority] = useState('100');
  const [enabled, setEnabled] = useState(true);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState('');

  useEffect(() => {
    if (!open) return;
    setName(rule?.name ?? '');
    setKeywords(rule?.keywords.join(', ') ?? '');
    setMatch(rule?.match ?? 'exact');
    setAction(rule?.action ?? 'none');
    setReply(rule?.reply ?? '');
    setPriority(String(rule?.priority ?? nextPriority));
    setEnabled(rule?.enabled ?? true);
    setErrors({});
    setFormError('');
  }, [open, rule, nextPriority]);

  const list = keywords
    .split(',')
    .map((k) => k.trim())
    .filter(Boolean);
  const pending = create.isPending || update.isPending;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setErrors({});
    setFormError('');
    if (list.length === 0 || list.length > 20) {
      setErrors({ keywords: 'Give 1 to 20 keywords, separated by commas.' });
      return;
    }
    if (!reply.trim() && action === 'none') {
      setErrors({ reply: 'A rule needs a reply, an action, or both.' });
      return;
    }
    const body = {
      name: name.trim(),
      keywords: list,
      match,
      action,
      reply: reply.trim(),
      priority: Number(priority),
      enabled,
    };
    try {
      if (rule) {
        await update.mutateAsync({ ruleId: rule.id, ...body });
        toast.success('Rule saved');
      } else {
        await create.mutateAsync({ ...body, reply: body.reply || undefined });
        toast.success('Rule added');
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
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{rule ? 'Edit auto-reply rule' : 'Add auto-reply rule'}</DialogTitle>
            <DialogDescription>
              When an incoming SMS matches a keyword, Bridge runs the action and sends the reply.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
            <div className="flex flex-col gap-2">
              <Label htmlFor="rule-name">Name</Label>
              <Input
                id="rule-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Opening hours"
                maxLength={60}
                required
                aria-invalid={errors.name ? true : undefined}
              />
              <FieldNote error={errors.name} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="rule-priority">Order</Label>
              <Input
                id="rule-priority"
                type="number"
                min={0}
                max={10000}
                value={priority}
                onChange={(e) => setPriority(e.target.value)}
                required
                aria-invalid={errors.priority ? true : undefined}
              />
              <FieldNote error={errors.priority} />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="rule-keywords">Keywords</Label>
            <Input
              id="rule-keywords"
              value={keywords}
              onChange={(e) => setKeywords(e.target.value)}
              placeholder="HOURS, OPEN, TIMINGS"
              required
              aria-invalid={errors.keywords ? true : undefined}
            />
            <FieldNote error={errors.keywords}>
              Separate with commas. Up to 20. Case does not matter.
            </FieldNote>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="rule-match">Match when</Label>
            <Select value={match} onValueChange={(v) => setMatch(v as Match)}>
              <SelectTrigger id="rule-match" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MATCHES.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldNote>{MATCHES.find((m) => m.value === match)?.hint}</FieldNote>
          </div>

          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 text-sm font-medium">Action</legend>
            <div className="flex flex-col gap-2">
              {ACTIONS.map((a) => (
                <label
                  key={a.value}
                  className="flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5"
                >
                  <input
                    type="radio"
                    name="rule-action"
                    checked={action === a.value}
                    onChange={() => setAction(a.value)}
                    className="mt-0.5 accent-primary"
                  />
                  <span className="flex flex-col">
                    <span className="text-sm font-medium">{a.label}</span>
                    <span className="text-xs text-muted-foreground">{a.hint}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>

          <div className="flex flex-col gap-2">
            <Label htmlFor="rule-reply">
              Reply{' '}
              {action !== 'none' ? (
                <span className="font-normal text-muted-foreground">(optional)</span>
              ) : null}
            </Label>
            <Textarea
              id="rule-reply"
              value={reply}
              onChange={(e) => setReply(e.target.value)}
              maxLength={480}
              rows={3}
              placeholder="We are open 9:00 to 18:00, Monday to Saturday."
              aria-invalid={errors.reply ? true : undefined}
            />
            {errors.reply ? (
              <FieldNote error={errors.reply} />
            ) : reply ? (
              <SegmentLine text={reply} />
            ) : (
              <FieldNote>Leave empty to run the action without answering.</FieldNote>
            )}
          </div>

          <div className="flex items-center gap-2.5">
            <Switch id="rule-enabled" checked={enabled} onCheckedChange={setEnabled} />
            <Label htmlFor="rule-enabled" className="text-sm font-normal">
              Enabled
            </Label>
          </div>

          {formError ? (
            <p className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed text-destructive">
              {formError}
            </p>
          ) : null}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !name.trim()}>
              {pending ? 'Saving…' : rule ? 'Save rule' : 'Add rule'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeleteRuleDialog({
  projectId,
  rule,
  onClose,
}: {
  projectId: string;
  rule: AutoReplyRule | null;
  onClose: () => void;
}) {
  const remove = useDeleteAutoReply(projectId);
  const optOut = rule?.action === 'opt_out';
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
            {optOut
              ? 'Senders who text these keywords will no longer be opted out. Turning the rule off keeps it for later. Default rules are not created again.'
              : 'Incoming SMS with these keywords will no longer be answered. Default rules are not created again.'}
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
