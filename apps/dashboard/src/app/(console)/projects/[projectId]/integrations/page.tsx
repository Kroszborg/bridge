'use client';

import type { Integration } from '@bridge/api-types';
import { ArrowUpRight01Icon, Delete02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { Segmented } from '@/components/kit/segmented';
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
import { Skeleton } from '@/components/ui/skeleton';
import { fieldErrors, showError } from '@/lib/errors';
import { formatDate, formatRelative } from '@/lib/format';
import {
  useCreateIntegration,
  useDeleteIntegration,
  useIntegrations,
  useRouting,
  useUpdateIntegration,
} from '@/lib/queries';

type Environment = Integration['environment'];

const DOCS = 'https://github.com/kroszborg/bridge/blob/main/docs/integrations';

const GUIDES: { file: string; title: string; description: string }[] = [
  {
    file: 'supabase.md',
    title: 'Supabase',
    description: 'Phone sign-in and MFA through the Send SMS hook, step by step.',
  },
  {
    file: 'better-auth.md',
    title: 'Better Auth',
    description: 'Deliver the phone number plugin’s codes through Bridge.',
  },
  {
    file: 'auth0.md',
    title: 'Auth0',
    description: 'Send MFA and passwordless SMS through Bridge.',
  },
  {
    file: 'firebase-clerk.md',
    title: 'Firebase and Clerk',
    description: 'What each one allows, and how to use Bridge Verify alongside them.',
  },
  {
    file: 'no-code.md',
    title: 'No-code tools',
    description: 'Send SMS from automation tools with a plain HTTP request.',
  },
];

const ENVIRONMENTS: { value: Environment; label: string; hint: string }[] = [
  { value: 'live', label: 'Live', hint: 'Sends real SMS through your phones or providers.' },
  { value: 'test', label: 'Test', hint: 'Nothing is sent. Messages go to the simulator.' },
];

export default function IntegrationsPage() {
  const projectId = useProjectId() ?? '';
  const routing = useRouting(projectId);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Integrations"
        subtitle="Let auth services and other tools send SMS through this project."
      />
      {routing.data && !routing.data.secret_key_set ? (
        <SecretKeyWarning what="integration secrets" />
      ) : null}
      <SupabaseCard projectId={projectId} />
      <SectionCard
        title="Guides"
        description="Step-by-step setup for other services, in the Bridge repository on GitHub."
      >
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {GUIDES.map((g) => (
            <a
              key={g.file}
              href={`${DOCS}/${g.file}`}
              target="_blank"
              rel="noreferrer"
              className="group flex flex-col gap-1 rounded-lg border p-3 transition-colors hover:border-primary/50 hover:bg-primary/5"
            >
              <span className="flex items-center justify-between gap-2 text-sm font-medium">
                {g.title}
                <HugeiconsIcon
                  icon={ArrowUpRight01Icon}
                  strokeWidth={2}
                  className="size-3.5 text-muted-foreground group-hover:text-primary"
                />
              </span>
              <span className="text-xs/relaxed text-muted-foreground">{g.description}</span>
            </a>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}

function SupabaseHealth({ integration: i }: { integration: Integration }) {
  if (!i.secret_set) return <StatusBadge kind="warning">Needs the hook secret</StatusBadge>;
  if (i.last_error) return <StatusBadge kind="warning">Last request failed</StatusBadge>;
  if (i.last_used_at) return <StatusBadge kind="success">Working</StatusBadge>;
  return <StatusBadge kind="neutral">Waiting for the first request</StatusBadge>;
}

function SupabaseCard({ projectId }: { projectId: string }) {
  const list = useIntegrations(projectId);
  const supabase = list.data?.find((i) => i.kind === 'supabase_send_sms');

  return (
    <SectionCard
      title="Supabase Auth"
      description="Phone sign-in and MFA codes. Supabase generates and checks each code; Bridge delivers it."
      action={supabase ? <SupabaseHealth integration={supabase} /> : undefined}
    >
      {list.isPending ? (
        <Skeleton className="h-40" />
      ) : list.isError ? (
        <EmptyState
          title="Could not load integrations"
          description={list.error.message}
          action={
            <Button variant="outline" onClick={() => list.refetch()}>
              Try again
            </Button>
          }
        />
      ) : supabase ? (
        <SupabaseSetup projectId={projectId} integration={supabase} />
      ) : (
        <ConnectSupabase projectId={projectId} />
      )}
    </SectionCard>
  );
}

function HowItWorks({ projectId }: { projectId: string }) {
  return (
    <p className="text-xs/relaxed text-muted-foreground">
      Supabase creates the code and checks it when the user types it in. Bridge only delivers the
      SMS, worded by this project&apos;s{' '}
      <Link href={`/projects/${projectId}/verify`} className="text-primary hover:underline">
        Verify message template
      </Link>
      , and masks the code in its logs. Messages follow your routing, so phones or providers send
      them.
    </p>
  );
}

function ConnectSupabase({ projectId }: { projectId: string }) {
  const canAdmin = useCan('admin');
  const create = useCreateIntegration(projectId);
  const [environment, setEnvironment] = useState<Environment>('live');

  async function connect(e: FormEvent) {
    e.preventDefault();
    try {
      await create.mutateAsync({ kind: 'supabase_send_sms', environment });
      toast.success('Supabase connected. Finish the setup below.');
    } catch (err) {
      showError(err);
    }
  }

  return (
    <form onSubmit={connect} className="flex flex-col gap-5">
      <HowItWorks projectId={projectId} />
      <fieldset disabled={!canAdmin} className="grid gap-2 sm:grid-cols-2">
        <legend className="mb-2 text-sm font-medium">Environment</legend>
        {ENVIRONMENTS.map((env) => (
          <label
            key={env.value}
            className="flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5 has-disabled:cursor-default"
          >
            <input
              type="radio"
              name="supabase-environment"
              checked={environment === env.value}
              onChange={() => setEnvironment(env.value)}
              className="mt-0.5 accent-primary"
            />
            <span className="flex flex-col">
              <span className="text-sm font-medium">{env.label}</span>
              <span className="text-xs text-muted-foreground">{env.hint}</span>
            </span>
          </label>
        ))}
      </fieldset>
      {canAdmin ? (
        <Button type="submit" size="lg" className="self-start" disabled={create.isPending}>
          {create.isPending ? 'Connecting…' : 'Connect Supabase'}
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground">Only admins can connect Supabase.</p>
      )}
    </form>
  );
}

function Step({ n, title, children }: { n: number; title: string; children?: React.ReactNode }) {
  return (
    <li>
      <span className="font-medium text-foreground">
        {n}. {title}
      </span>
      {children ? (
        <>
          <br />
          {children}
        </>
      ) : null}
    </li>
  );
}

function SupabaseSetup({
  projectId,
  integration: i,
}: {
  projectId: string;
  integration: Integration;
}) {
  const canAdmin = useCan('admin');
  const update = useUpdateIntegration(projectId);
  const [secret, setSecret] = useState('');
  const [secretError, setSecretError] = useState('');
  const [removing, setRemoving] = useState(false);

  async function saveSecret(e: FormEvent) {
    e.preventDefault();
    setSecretError('');
    try {
      await update.mutateAsync({ integrationId: i.id, secret: secret.trim() });
      setSecret('');
      toast.success('Hook secret saved');
    } catch (err) {
      const fe = fieldErrors(err);
      if (fe.secret) setSecretError(fe.secret);
      else showError(err);
    }
  }

  async function switchEnvironment(environment: Environment) {
    if (environment === i.environment) return;
    try {
      await update.mutateAsync({ integrationId: i.id, environment });
      toast.success(
        environment === 'live'
          ? 'Supabase now sends real SMS'
          : 'Supabase codes now go to the simulator',
      );
    } catch (err) {
      showError(err);
    }
  }

  const localConfig = [
    '[auth.hook.send_sms]',
    'enabled = true',
    `uri = "${i.hook_url}"`,
    'secrets = "env(BRIDGE_SEND_SMS_HOOK_SECRET)"',
  ].join('\n');

  return (
    <div className="flex flex-col gap-6">
      <HowItWorks projectId={projectId} />

      <div className="flex min-w-0 flex-col gap-1.5">
        <span className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
          Hook URL
        </span>
        <CopyField value={i.hook_url} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="flex flex-col gap-3">
          <h3 className="text-sm font-medium">Set it up in Supabase</h3>
          <ol className="flex flex-col gap-3 text-xs/relaxed text-muted-foreground">
            <Step n={1} title="Open Authentication, then Hooks">
              In the Supabase Dashboard, choose your project, then Authentication and Hooks.
            </Step>
            <Step n={2} title="Add a Send SMS hook of type HTTPS">
              Paste the hook URL above as the URL.
            </Step>
            <Step n={3} title="Generate a secret and create the hook">
              Supabase generates a secret that starts with{' '}
              <code className="font-mono">v1,whsec_</code>. Copy it.
            </Step>
            <Step n={4} title="Paste the secret here and save">
              Bridge refuses Supabase&apos;s requests until it has the secret, and checks the
              signature of every request with it.
            </Step>
            <Step n={5} title="Turn on phone sign-in">
              Enable the Phone provider under Authentication. Its own SMS provider settings are not
              used while the hook is on.
            </Step>
          </ol>
        </div>

        <div className="flex flex-col gap-5">
          <form onSubmit={saveSecret} className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <Label htmlFor="supabase-secret">Hook secret</Label>
              {i.secret_set ? (
                <StatusBadge kind="success">Secret saved</StatusBadge>
              ) : (
                <StatusBadge kind="warning">Not set</StatusBadge>
              )}
            </div>
            <div className="flex gap-2">
              <Input
                id="supabase-secret"
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                value={secret}
                onChange={(e) => {
                  setSecret(e.target.value);
                  setSecretError('');
                }}
                placeholder={i.secret_set ? 'Paste a new secret to replace it' : 'v1,whsec_…'}
                className="font-mono"
                aria-invalid={secretError ? true : undefined}
                disabled={!canAdmin}
                required
              />
              {canAdmin ? (
                <Button type="submit" disabled={update.isPending || !secret.trim()}>
                  Save
                </Button>
              ) : null}
            </div>
            {secretError ? (
              <p className="text-xs text-destructive">{secretError}</p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {i.secret_set
                  ? 'Stored encrypted and never shown again. If Supabase reports a signature error, paste the secret again.'
                  : 'Paste it exactly as Supabase shows it.'}
              </p>
            )}
          </form>

          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">Environment</span>
            {canAdmin ? (
              <div>
                <Segmented
                  label="Environment"
                  value={i.environment}
                  onChange={switchEnvironment}
                  options={ENVIRONMENTS.map((env) => ({ value: env.value, label: env.label }))}
                />
              </div>
            ) : null}
            <p className="text-xs text-muted-foreground">
              {ENVIRONMENTS.find((env) => env.value === i.environment)?.hint}
              {i.environment === 'test' ? ' Find the codes under Messages, in Test.' : ''}
            </p>
          </div>

          <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
            <div className="flex min-w-0 flex-col gap-0.5">
              <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                Last used
              </dt>
              <dd className="text-sm" title={i.last_used_at ?? undefined}>
                {formatRelative(i.last_used_at)}
              </dd>
            </div>
            <div className="flex min-w-0 flex-col gap-0.5">
              <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                Connected
              </dt>
              <dd className="text-sm">{formatDate(i.created_at)}</dd>
            </div>
          </dl>
          {i.last_error ? (
            <div className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
              <p className="font-semibold text-destructive">Last error</p>
              <p className="mt-1 text-muted-foreground">{i.last_error}</p>
            </div>
          ) : null}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">Local development with the Supabase CLI</h3>
        <p className="text-xs/relaxed text-muted-foreground">
          Add the hook to <code className="font-mono">supabase/config.toml</code>. With the CLI you
          choose the secret yourself: set{' '}
          <code className="font-mono">BRIDGE_SEND_SMS_HOOK_SECRET</code> to a value like the one
          below, restart Supabase, and paste the same value into Hook secret above. Supabase runs in
          Docker, so if the hook URL points at localhost, use{' '}
          <code className="font-mono">host.docker.internal</code> instead.
        </p>
        <div className="grid gap-3 lg:grid-cols-2">
          <CodeBlock language="shell" code={'echo "v1,whsec_$(openssl rand -base64 32)"'} />
          <CodeBlock language="toml" code={localConfig} />
        </div>
      </div>

      {canAdmin ? (
        <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <p className="text-xs text-muted-foreground">
            Disconnecting stops the hook URL from working at once.
          </p>
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-destructive"
            onClick={() => setRemoving(true)}
          >
            <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
            Disconnect Supabase
          </Button>
        </div>
      ) : null}

      <DisconnectDialog
        projectId={projectId}
        integration={removing ? i : null}
        onClose={() => setRemoving(false)}
      />
    </div>
  );
}

function DisconnectDialog({
  projectId,
  integration,
  onClose,
}: {
  projectId: string;
  integration: Integration | null;
  onClose: () => void;
}) {
  const remove = useDeleteIntegration(projectId);
  async function confirm() {
    if (!integration) return;
    try {
      await remove.mutateAsync(integration.id);
      toast.success('Supabase disconnected');
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={integration !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Disconnect Supabase?</DialogTitle>
          <DialogDescription>
            The hook URL stops working at once and the stored secret is deleted. Turn off the Send
            SMS hook in Supabase first, or phone sign-in fails. Connecting again gives a new URL.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Disconnecting…' : 'Disconnect'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
