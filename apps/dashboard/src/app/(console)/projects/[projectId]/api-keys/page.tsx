'use client';

import type { ApiKey, CreatedApiKey } from '@bridge/api-types';
import { Alert02Icon, Delete02Icon, Key01Icon, PlusSignIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyButton } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan, useConsole, useProjectId } from '@/components/layout/console-context';
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
import { type NewApiKey, useApiKeys, useCreateApiKey, useRevokeApiKey } from '@/lib/queries';
import { cn } from '@/lib/utils';

const statusKind: Record<ApiKey['status'], StatusKind> = {
  active: 'success',
  revoked: 'neutral',
  expired: 'warning',
};

function EnvironmentBadge({ env }: { env: ApiKey['environment'] }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-[0.68rem] font-medium',
        env === 'live' ? 'border-primary/30 bg-primary/10 text-primary' : 'text-muted-foreground',
      )}
    >
      {env}
    </span>
  );
}

function CreateKeyDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (key: CreatedApiKey) => void;
}) {
  const projectId = useProjectId() ?? '';
  const create = useCreateApiKey(projectId);
  const [name, setName] = useState('');
  const [environment, setEnvironment] = useState<NewApiKey['environment']>('test');
  const [expiry, setExpiry] = useState('never');

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      const key = await create.mutateAsync({
        name,
        environment,
        expires_in_days: expiry === 'never' ? undefined : Number(expiry),
      });
      setName('');
      onOpenChange(false);
      onCreated(key);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Create API key</DialogTitle>
            <DialogDescription>
              Keys authenticate your server to Bridge. Keep them out of browsers and apps.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="key-name">Name</Label>
            <Input
              id="key-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Production server"
              maxLength={80}
              required
              autoFocus
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="key-env">Environment</Label>
              <Select
                value={environment}
                onValueChange={(v) => setEnvironment(v as NewApiKey['environment'])}
              >
                <SelectTrigger id="key-env" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="test">Test: simulated, free</SelectItem>
                  <SelectItem value="live">Live: sends real SMS</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="key-expiry">Expires</Label>
              <Select value={expiry} onValueChange={setExpiry}>
                <SelectTrigger id="key-expiry" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="never">Never</SelectItem>
                  <SelectItem value="30">In 30 days</SelectItem>
                  <SelectItem value="90">In 90 days</SelectItem>
                  <SelectItem value="365">In 1 year</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending || !name.trim()}>
              {create.isPending ? 'Creating…' : 'Create key'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RevealKeyDialog({
  apiKey,
  onClose,
}: {
  apiKey: CreatedApiKey | null;
  onClose: () => void;
}) {
  const { apiUrl } = useConsole();
  return (
    <Dialog open={apiKey !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Copy your new key</DialogTitle>
          <DialogDescription>
            This is the only time Bridge shows the full key. It stores a hash and cannot recover it.
          </DialogDescription>
        </DialogHeader>
        {apiKey ? (
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 break-all font-mono text-xs text-foreground">
                {apiKey.secret}
              </code>
              <CopyButton value={apiKey.secret} label="Copy key" showLabel />
            </div>
            <p className="flex items-start gap-2 text-xs/relaxed text-muted-foreground">
              <HugeiconsIcon
                icon={Alert02Icon}
                strokeWidth={2}
                className="mt-0.5 size-3.5 shrink-0 text-warning"
              />
              Store it in your server&apos;s secrets, for example as BRIDGE_API_KEY. Never commit it
              or ship it in a browser or mobile app.
            </p>
            <CodeBlock
              language="shell"
              code={`export BRIDGE_API_KEY="${apiKey.secret}"\ncurl ${apiUrl}/v1/whoami -H "Authorization: Bearer $BRIDGE_API_KEY"`}
            />
          </div>
        ) : null}
        <DialogFooter>
          <Button onClick={onClose}>I have saved the key</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RevokeDialog({ apiKey, onClose }: { apiKey: ApiKey | null; onClose: () => void }) {
  const projectId = useProjectId() ?? '';
  const revoke = useRevokeApiKey(projectId);
  async function confirm() {
    if (!apiKey) return;
    try {
      await revoke.mutateAsync(apiKey.id);
      toast.success(`Revoked ${apiKey.name}`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={apiKey !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Revoke {apiKey?.name}?</DialogTitle>
          <DialogDescription>
            Requests using <span className="font-mono">{apiKey?.prefix}…</span> start failing
            immediately. This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={revoke.isPending}>
            {revoke.isPending ? 'Revoking…' : 'Revoke key'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function ApiKeysPage() {
  const projectId = useProjectId() ?? '';
  const keys = useApiKeys(projectId);
  const [createOpen, setCreateOpen] = useState(false);
  const [revealed, setRevealed] = useState<CreatedApiKey | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const canAdmin = useCan('admin');
  const createButton = !canAdmin ? undefined : (
    <Button size="lg" onClick={() => setCreateOpen(true)}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
      Create API key
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="API keys"
        subtitle="Authenticate server-side requests. Bridge stores only a hash of each key."
        actions={keys.data?.length ? createButton : undefined}
      />

      <div data-slot="section-card" className="overflow-hidden rounded-xl border bg-card">
        {keys.isPending ? (
          <div className="flex flex-col gap-3 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : keys.isError ? (
          <EmptyState
            title="Could not load API keys"
            description={keys.error.message}
            action={
              <Button variant="outline" onClick={() => keys.refetch()}>
                Try again
              </Button>
            }
          />
        ) : keys.data.length === 0 ? (
          <EmptyState
            icon={<HugeiconsIcon icon={Key01Icon} strokeWidth={1.8} />}
            title="No API keys yet"
            description="Create a test key to start building. Test keys use the full API but never send real SMS."
            action={createButton}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-5">Name</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>Environment</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="pr-5 text-right">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys.data.map((k) => (
                <TableRow
                  key={k.id}
                  className={cn(k.status !== 'active' && 'text-muted-foreground')}
                >
                  <TableCell className="pl-5 text-sm font-medium">{k.name}</TableCell>
                  <TableCell className="font-mono text-muted-foreground">{k.prefix}…</TableCell>
                  <TableCell>
                    <EnvironmentBadge env={k.environment} />
                  </TableCell>
                  <TableCell>
                    <StatusBadge kind={statusKind[k.status]} className="capitalize">
                      {k.status}
                    </StatusBadge>
                  </TableCell>
                  <TableCell title={k.last_used_at ?? undefined}>
                    {formatRelative(k.last_used_at)}
                  </TableCell>
                  <TableCell>{formatDate(k.created_at)}</TableCell>
                  <TableCell>{k.expires_at ? formatDate(k.expires_at) : 'Never'}</TableCell>
                  <TableCell className="pr-5 text-right">
                    {k.status === 'active' && canAdmin ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="row-action text-muted-foreground hover:text-destructive"
                        onClick={() => setRevoking(k)}
                      >
                        <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                        Revoke
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <CreateKeyDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={setRevealed} />
      <RevealKeyDialog apiKey={revealed} onClose={() => setRevealed(null)} />
      <RevokeDialog apiKey={revoking} onClose={() => setRevoking(null)} />
    </div>
  );
}
