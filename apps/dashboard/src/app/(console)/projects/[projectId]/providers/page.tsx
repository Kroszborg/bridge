'use client';

import type { ProviderAccount, ProviderSpec, Routing } from '@bridge/api-types';
import {
  ArrowLeft01Icon,
  CloudServerIcon,
  Delete02Icon,
  Edit02Icon,
  PlusSignIcon,
  SecurityCheckIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { StatusBadge } from '@/components/kit/status-badge';
import { useCan, useProjectId } from '@/components/layout/console-context';
import { SecretKeyWarning } from '@/components/secret-key-warning';
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
import { fieldErrors, showError } from '@/lib/errors';
import { formatDate, formatRelative } from '@/lib/format';
import {
  useAddProvider,
  useCheckProvider,
  useProviderKinds,
  useProviders,
  useRemoveProvider,
  useRouting,
  useUpdateProvider,
  useUpdateRouting,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

type Mode = Routing['mode'];
type Kind = ProviderAccount['kind'];

const MODES: { value: Mode; label: string; hint: string }[] = [
  {
    value: 'phones',
    label: 'Phones only',
    hint: 'Paired phones send everything. Providers are never used.',
  },
  {
    value: 'phones_then_providers',
    label: 'Phones, then providers',
    hint: 'A phone tries first. A provider sends what no phone can.',
  },
  {
    value: 'providers',
    label: 'Providers only',
    hint: 'Providers send every live message. Phones are not used for sending.',
  },
];

const WAITS = [0, 15, 30, 60, 120, 300];

function waitLabel(seconds: number): string {
  if (seconds === 0) return 'Immediately';
  if (seconds < 60) return `After ${seconds} s`;
  return `After ${Math.round(seconds / 6) / 10} min`;
}

/** Trimmed values, without empty ones. */
function compact(values: Record<string, string>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(values)
      .map(([k, v]) => [k, v.trim()] as const)
      .filter(([, v]) => v !== ''),
  );
}

function sameValues(a: Record<string, string>, b: Record<string, string>): boolean {
  const ka = Object.keys(a);
  return ka.length === Object.keys(b).length && ka.every((k) => a[k] === b[k]);
}

export default function ProvidersPage() {
  const projectId = useProjectId() ?? '';
  const canAdmin = useCan('admin');
  const routing = useRouting(projectId);
  const providers = useProviders(projectId);
  const kinds = useProviderKinds();
  const [dialogOpen, setDialogOpen] = useState(false);
  // A new key per opening starts the dialog's form from scratch.
  const [session, setSession] = useState(0);
  const [editing, setEditing] = useState<ProviderAccount | null>(null);
  const [removing, setRemoving] = useState<ProviderAccount | null>(null);

  const secretKeySet = routing.data?.secret_key_set ?? true;
  const specs = kinds.data ?? [];
  const list = [...(providers.data ?? [])].sort(
    (a, b) => a.priority - b.priority || a.created_at.localeCompare(b.created_at),
  );
  const allAdded = specs.length > 0 && specs.every((s) => list.some((p) => p.kind === s.kind));

  const addButton =
    !canAdmin || allAdded ? undefined : (
      <Button
        size="lg"
        onClick={() => {
          setEditing(null);
          setSession((n) => n + 1);
          setDialogOpen(true);
        }}
        disabled={!secretKeySet || !specs.length}
      >
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
        Add provider
      </Button>
    );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Providers"
        subtitle="Send through Twilio, MSG91, Vonage or Plivo when your phones cannot, or instead of them."
        actions={list.length ? addButton : undefined}
      />

      {routing.data && !routing.data.secret_key_set ? (
        <SecretKeyWarning what="provider credentials" />
      ) : null}

      {routing.isPending ? (
        <SectionCard title="Routing">
          <Skeleton className="h-40" />
        </SectionCard>
      ) : routing.data ? (
        <RoutingForm
          key={`${routing.data.mode}-${routing.data.fallback_after_seconds}`}
          projectId={projectId}
          initial={routing.data}
          enabledProviders={list.filter((p) => p.enabled).length}
          providersLoaded={providers.isSuccess}
        />
      ) : null}

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {providers.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : providers.isError ? (
          <EmptyState
            title="Could not load providers"
            description={providers.error.message}
            action={
              <Button variant="outline" onClick={() => providers.refetch()}>
                Try again
              </Button>
            }
          />
        ) : list.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={CloudServerIcon} strokeWidth={1.8} />}
            title="No providers yet"
            description="Add an SMS provider account to send when no phone can, or to send everything. Bridge uses it only for live messages, as routing above allows."
            action={addButton}
          />
        ) : (
          <ul className="divide-y">
            {list.map((p) => (
              <ProviderRow
                key={p.id}
                projectId={projectId}
                provider={p}
                spec={specs.find((s) => s.kind === p.kind)}
                onEdit={() => {
                  setEditing(p);
                  setSession((n) => n + 1);
                  setDialogOpen(true);
                }}
                onRemove={() => setRemoving(p)}
              />
            ))}
          </ul>
        )}
      </div>

      <ProviderDialog
        key={session}
        projectId={projectId}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        specs={specs}
        existing={list}
        nextPriority={list.length ? Math.min(Math.max(...list.map((p) => p.priority)) + 1, 100) : 0}
        editing={editing}
      />
      <RemoveDialog projectId={projectId} provider={removing} onClose={() => setRemoving(null)} />
    </div>
  );
}

function RoutingForm({
  projectId,
  initial,
  enabledProviders,
  providersLoaded,
}: {
  projectId: string;
  initial: Routing;
  enabledProviders: number;
  providersLoaded: boolean;
}) {
  const canAdmin = useCan('admin');
  const update = useUpdateRouting(projectId);
  const [mode, setMode] = useState<Mode>(initial.mode);
  const [wait, setWait] = useState(String(initial.fallback_after_seconds));
  const unchanged = mode === initial.mode && Number(wait) === initial.fallback_after_seconds;

  async function save(e: FormEvent) {
    e.preventDefault();
    try {
      await update.mutateAsync({ mode, fallback_after_seconds: Number(wait) });
      toast.success('Routing saved');
    } catch (err) {
      showError(err);
    }
  }

  const waits = [...new Set([...WAITS, Number(wait)])].sort((a, b) => a - b);

  return (
    <SectionCard
      title="Routing"
      description={
        canAdmin
          ? 'Decides whether live messages go to your phones, your providers, or both.'
          : 'Decides whether live messages go to your phones, your providers, or both. Only admins can change it.'
      }
    >
      <form onSubmit={save} className="flex flex-col gap-5">
        <fieldset disabled={!canAdmin} className="contents">
          <fieldset className="grid gap-2 sm:grid-cols-3">
            <legend className="mb-2 text-sm font-medium">Who sends live messages</legend>
            {MODES.map((m) => (
              <label
                key={m.value}
                className="flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5 has-disabled:cursor-default"
              >
                <input
                  type="radio"
                  name="routing-mode"
                  checked={mode === m.value}
                  onChange={() => setMode(m.value)}
                  className="mt-0.5 accent-primary"
                />
                <span className="flex flex-col">
                  <span className="text-sm font-medium">{m.label}</span>
                  <span className="text-xs text-muted-foreground">{m.hint}</span>
                </span>
              </label>
            ))}
          </fieldset>

          {mode === 'phones_then_providers' ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="routing-wait">Hand a waiting message to a provider</Label>
              <Select value={wait} onValueChange={setWait}>
                <SelectTrigger id="routing-wait" className="w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {waits.map((v) => (
                    <SelectItem key={v} value={String(v)}>
                      {waitLabel(v)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs/relaxed text-muted-foreground">
                How long a message may wait for a phone to take it. A provider also takes it at once
                when no phone is paired, and when a phone does not accept the job or still fails
                after its retries.
              </p>
            </div>
          ) : null}
        </fieldset>

        {mode !== 'phones' && providersLoaded && enabledProviders === 0 ? (
          <p className="rounded-lg border border-warning/40 bg-warning/8 p-3 text-xs/relaxed">
            No provider is enabled, so your phones still send everything. Add or enable a provider
            below.
          </p>
        ) : null}

        <ul className="flex list-disc flex-col gap-1 pl-4 text-xs/relaxed text-muted-foreground">
          <li>
            Providers are tried by priority, lowest number first. If one refuses a message, the next
            one tries.
          </li>
          <li>Messages sent with a test key never use providers. They are always simulated.</li>
          <li>
            A message sent with a specific <code className="font-mono">device_id</code> waits for
            that phone and never falls back.
          </li>
          <li>
            A provider accepting a message counts as Sent. Delivered follows when the provider
            reports it.
          </li>
        </ul>

        {canAdmin ? (
          <Button type="submit" className="self-start" disabled={update.isPending || unchanged}>
            {update.isPending ? 'Saving…' : 'Save routing'}
          </Button>
        ) : null}
      </form>
    </SectionCard>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
        {label}
      </dt>
      <dd className="truncate text-sm">{children}</dd>
    </div>
  );
}

function ProviderHealth({ provider: p }: { provider: ProviderAccount }) {
  if (!p.enabled) return <StatusBadge kind="neutral">Disabled</StatusBadge>;
  if (p.last_error) return <StatusBadge kind="warning">Last send failed</StatusBadge>;
  if (p.last_used_at) return <StatusBadge kind="success">Healthy</StatusBadge>;
  return <StatusBadge kind="neutral">Not used yet</StatusBadge>;
}

function ProviderRow({
  projectId,
  provider: p,
  spec,
  onEdit,
  onRemove,
}: {
  projectId: string;
  provider: ProviderAccount;
  spec: ProviderSpec | undefined;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const canAdmin = useCan('admin');
  const update = useUpdateProvider(projectId);
  const check = useCheckProvider(projectId);
  const [result, setResult] = useState<{ ok: boolean; detail: string } | null>(null);
  const kindName = spec?.name ?? p.kind;
  const settings = (spec?.config ?? []).filter((f) => p.config[f.key]);

  async function toggle(enabled: boolean) {
    try {
      await update.mutateAsync({ providerId: p.id, enabled });
      toast.success(enabled ? `${p.name} enabled` : `${p.name} disabled`);
    } catch (err) {
      showError(err);
    }
  }

  async function runCheck() {
    setResult(null);
    try {
      setResult(await check.mutateAsync(p.id));
    } catch (err) {
      showError(err);
    }
  }

  return (
    <li className="flex flex-col gap-4 px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <Switch
            checked={p.enabled}
            onCheckedChange={toggle}
            disabled={!canAdmin || update.isPending}
            aria-label={`Send through ${p.name}`}
            className="mt-1"
          />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="text-sm font-medium">{p.name}</span>
              {p.name !== kindName ? (
                <span className="rounded-md border bg-background px-1.5 py-0.5 font-mono text-[0.68rem] text-muted-foreground">
                  {kindName}
                </span>
              ) : null}
              <ProviderHealth provider={p} />
            </div>
            <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
              {p.credential_hint || 'Credentials stored'}
            </p>
          </div>
        </div>
        {canAdmin ? (
          <div className="flex flex-wrap items-center gap-1">
            <Button variant="outline" size="sm" onClick={runCheck} disabled={check.isPending}>
              <HugeiconsIcon icon={SecurityCheckIcon} strokeWidth={2} />
              {check.isPending ? 'Checking…' : 'Check credentials'}
            </Button>
            <Button variant="ghost" size="sm" onClick={onEdit}>
              <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
              Edit
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground hover:text-destructive"
              onClick={onRemove}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              Remove
            </Button>
          </div>
        ) : null}
      </div>

      {result ? (
        <p
          role="status"
          className={cn(
            'rounded-md px-3 py-2 text-xs/relaxed',
            result.ok ? 'bg-success/10 text-success' : 'bg-destructive/8 text-destructive',
          )}
        >
          {result.ok ? 'Credentials work.' : 'Check failed.'} {result.detail}
        </p>
      ) : null}

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
        <Fact label="Priority">{p.priority}</Fact>
        <Fact label="Last used">
          <span title={p.last_used_at ?? undefined}>{formatRelative(p.last_used_at)}</span>
        </Fact>
        <Fact label="Added">{formatDate(p.created_at)}</Fact>
        <Fact label="Delivery reports">
          {p.callbacks === 'account' ? `Set up in ${kindName}` : 'Automatic'}
        </Fact>
        {settings.map((f) => (
          <Fact key={f.key} label={f.label}>
            <span className="font-mono text-xs" title={p.config[f.key]}>
              {p.config[f.key]}
            </span>
          </Fact>
        ))}
      </dl>

      {p.last_error ? (
        <div className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
          <p className="font-semibold text-destructive">Last error</p>
          <p className="mt-1 text-muted-foreground">{p.last_error}</p>
        </div>
      ) : null}

      <div className="flex min-w-0 flex-col gap-1.5">
        <span className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
          Callback URL
        </span>
        <CopyField value={p.callback_url} />
        <p className="text-xs/relaxed text-muted-foreground">
          {p.callbacks === 'account'
            ? `${kindName} does not take a callback per message. In your ${kindName} dashboard, set this URL as the delivery-report webhook with JSON as the format. Until then, messages stay at Sent.`
            : `Bridge passes this URL to ${kindName} with every message, so delivery reports arrive without any setup.`}
        </p>
      </div>
    </li>
  );
}

function ProviderDialog({
  projectId,
  open,
  onOpenChange,
  specs,
  existing,
  nextPriority,
  editing,
}: {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  specs: ProviderSpec[];
  existing: ProviderAccount[];
  nextPriority: number;
  editing: ProviderAccount | null;
}) {
  const add = useAddProvider(projectId);
  const update = useUpdateProvider(projectId);
  const [kind, setKind] = useState<Kind | null>(editing?.kind ?? null);
  const [name, setName] = useState(editing?.name ?? '');
  const [priority, setPriority] = useState(String(editing?.priority ?? nextPriority));
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [config, setConfig] = useState<Record<string, string>>({ ...editing?.config });
  const [errors, setErrors] = useState<Record<string, string>>({});

  const spec = specs.find((s) => s.kind === kind);
  const isEdit = editing !== null;
  const pending = add.isPending || update.isPending;
  const anyCredential = Object.values(credentials).some((v) => v.trim() !== '');

  function pick(k: Kind) {
    setKind(k);
    setName('');
    setCredentials({});
    setConfig({});
    setErrors({});
  }

  function setField(group: 'credentials' | 'config', key: string, value: string) {
    (group === 'credentials' ? setCredentials : setConfig)((v) => ({ ...v, [key]: value }));
    setErrors(({ [`${group}.${key}`]: _, ...rest }) => rest);
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!spec || !kind) return;
    setErrors({});
    try {
      if (editing) {
        const nextConfig = compact(config);
        await update.mutateAsync({
          providerId: editing.id,
          name: name.trim() && name.trim() !== editing.name ? name.trim() : undefined,
          priority: Number(priority) !== editing.priority ? Number(priority) : undefined,
          credentials: anyCredential ? compact(credentials) : undefined,
          config: sameValues(nextConfig, editing.config) ? undefined : nextConfig,
        });
        toast.success(`${name.trim() || editing.name} saved`);
      } else {
        await add.mutateAsync({
          kind,
          name: name.trim() || undefined,
          priority: Number(priority),
          credentials: compact(credentials),
          config: compact(config),
        });
        toast.success(`${name.trim() || spec.name} added`);
      }
      onOpenChange(false);
    } catch (err) {
      const fe = fieldErrors(err);
      const shown = Object.keys(fe).filter(
        (k) =>
          k === 'name' ||
          k === 'priority' ||
          spec.credentials.some((f) => k === `credentials.${f.key}`) ||
          spec.config.some((f) => k === `config.${f.key}`),
      );
      setErrors(fe);
      if (shown.length === 0) showError(err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        {!spec ? (
          <div className="flex min-w-0 flex-col gap-5">
            <DialogHeader>
              <DialogTitle>Add a provider</DialogTitle>
              <DialogDescription>
                One account per provider. Credentials are encrypted and never shown again.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-2">
              {specs.map((s) => {
                const added = existing.some((p) => p.kind === s.kind);
                return (
                  <button
                    key={s.kind}
                    type="button"
                    disabled={added}
                    onClick={() => pick(s.kind as Kind)}
                    className="flex flex-col items-start gap-0.5 rounded-lg border p-3 text-left transition-colors hover:border-primary/50 hover:bg-primary/5 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:border-border disabled:hover:bg-transparent"
                  >
                    <span className="flex w-full items-center justify-between gap-2 text-sm font-medium">
                      {s.name}
                      {added ? (
                        <span className="text-xs font-normal text-muted-foreground">
                          Already added
                        </span>
                      ) : null}
                    </span>
                    <span className="text-xs/relaxed text-muted-foreground">{s.description}</span>
                  </button>
                );
              })}
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
            <DialogHeader>
              <DialogTitle>{editing ? `Edit ${editing.name}` : `Add ${spec.name}`}</DialogTitle>
              <DialogDescription>{spec.description}</DialogDescription>
            </DialogHeader>

            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
              <div className="flex flex-col gap-2">
                <Label htmlFor="provider-name">
                  Name <span className="font-normal text-muted-foreground">(optional)</span>
                </Label>
                <Input
                  id="provider-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={spec.name}
                  maxLength={60}
                  aria-invalid={errors.name ? true : undefined}
                />
                {errors.name ? <p className="text-xs text-destructive">{errors.name}</p> : null}
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="provider-priority">Priority</Label>
                <Input
                  id="provider-priority"
                  type="number"
                  min={0}
                  max={100}
                  value={priority}
                  onChange={(e) => setPriority(e.target.value)}
                  aria-invalid={errors.priority ? true : undefined}
                  required
                />
                {errors.priority ? (
                  <p className="text-xs text-destructive">{errors.priority}</p>
                ) : null}
              </div>
            </div>
            <p className="-mt-3 text-xs text-muted-foreground">
              Lower numbers are tried first, from 0 to 100.
            </p>

            <fieldset className="flex flex-col gap-4">
              <legend className="mb-1 text-sm font-medium">Credentials</legend>
              {editing ? (
                <p className="-mt-1 text-xs/relaxed text-muted-foreground">
                  Stored for{' '}
                  <span className="font-mono text-foreground">
                    {editing.credential_hint || spec.name}
                  </span>
                  . Leave these empty to keep them. To change them, enter every required one.
                </p>
              ) : null}
              {spec.credentials.map((f) => (
                <Field
                  key={f.key}
                  id={`provider-credentials-${f.key}`}
                  field={f}
                  value={credentials[f.key] ?? ''}
                  onChange={(v) => setField('credentials', f.key, v)}
                  error={errors[`credentials.${f.key}`]}
                  required={f.required && (!isEdit || anyCredential)}
                  markOptional={!isEdit}
                  placeholder={isEdit ? 'Unchanged' : undefined}
                />
              ))}
            </fieldset>

            {spec.config.length ? (
              <fieldset className="flex flex-col gap-4">
                <legend className="mb-1 text-sm font-medium">Sending</legend>
                {spec.kind === 'msg91' ? (
                  <p className="-mt-1 text-xs/relaxed text-muted-foreground">
                    MSG91 sends DLT-registered templates. The OTP template&apos;s variable receives
                    the code; the message template&apos;s variable receives the whole text. Set at
                    least one template.
                  </p>
                ) : null}
                {spec.config.map((f) => (
                  <Field
                    key={f.key}
                    id={`provider-config-${f.key}`}
                    field={f}
                    value={config[f.key] ?? ''}
                    onChange={(v) => setField('config', f.key, v)}
                    error={errors[`config.${f.key}`]}
                    required={f.required}
                    markOptional
                  />
                ))}
              </fieldset>
            ) : null}

            {!editing && spec.callbacks === 'account' ? (
              <p className="rounded-lg border bg-background/60 p-3 text-xs/relaxed text-muted-foreground">
                After adding, copy the callback URL shown on this page into {spec.name} as the
                delivery-report webhook (JSON). Without it, messages stay at Sent.
              </p>
            ) : null}

            <DialogFooter>
              {editing ? (
                <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
              ) : (
                <Button type="button" variant="ghost" onClick={() => setKind(null)}>
                  <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
                  Back
                </Button>
              )}
              <Button type="submit" disabled={pending}>
                {pending ? 'Saving…' : editing ? 'Save changes' : `Add ${spec.name}`}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Field({
  id,
  field: f,
  value,
  onChange,
  error,
  required,
  markOptional,
  placeholder,
}: {
  id: string;
  field: ProviderSpec['credentials'][number];
  value: string;
  onChange: (v: string) => void;
  error?: string;
  required: boolean;
  markOptional: boolean;
  placeholder?: string;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>
        {f.label}
        {markOptional && !f.required ? (
          <span className="font-normal text-muted-foreground"> (optional)</span>
        ) : null}
      </Label>
      <Input
        id={id}
        type={f.secret ? 'password' : 'text'}
        autoComplete={f.secret ? 'new-password' : 'off'}
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        required={required}
        aria-invalid={error ? true : undefined}
        aria-describedby={error || f.help ? `${id}-note` : undefined}
        className="font-mono"
      />
      {error ? (
        <p id={`${id}-note`} className="text-xs text-destructive">
          {error}
        </p>
      ) : f.help ? (
        <p id={`${id}-note`} className="text-xs/relaxed text-muted-foreground">
          {f.help}
        </p>
      ) : null}
    </div>
  );
}

function RemoveDialog({
  projectId,
  provider,
  onClose,
}: {
  projectId: string;
  provider: ProviderAccount | null;
  onClose: () => void;
}) {
  const remove = useRemoveProvider(projectId);
  async function confirm() {
    if (!provider) return;
    try {
      await remove.mutateAsync(provider.id);
      toast.success(`Removed ${provider.name}`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={provider !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove {provider?.name}?</DialogTitle>
          <DialogDescription>
            Bridge stops sending through it at once and deletes the stored credentials. Delivery
            reports for messages it already sent are no longer applied. This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Removing…' : 'Remove provider'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
