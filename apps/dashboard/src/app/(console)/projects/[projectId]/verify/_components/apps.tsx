'use client';

import type { CreatedVerifyApp, VerifyApp } from '@bridge/api-types';
import { Alert02Icon, Delete02Icon, PlusSignIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CopyButton } from '@/components/kit/copy-button';
import { useCan } from '@/components/layout/console-context';
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
import { fieldErrors, showError } from '@/lib/errors';
import { useCreateVerifyApp, useDeleteVerifyApp } from '@/lib/queries';
import { FieldNote } from './shared';

/** The app picker and its actions, above everything that belongs to one app. */
export function AppBar({
  projectId,
  apps,
  app,
  onSelect,
  onCreated,
}: {
  projectId: string;
  apps: VerifyApp[];
  app: VerifyApp;
  onSelect: (app: VerifyApp) => void;
  /** Called with the new app and its one-time secret. The page shows it. */
  onCreated: (app: CreatedVerifyApp) => void;
}) {
  const canAdmin = useCan('admin');
  const [creating, setCreating] = useState(0);
  const [deleting, setDeleting] = useState(false);

  return (
    <div className="flex flex-col gap-3 rounded-xl border bg-card px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <Select
          value={app.id}
          onValueChange={(id) => {
            const next = apps.find((a) => a.id === id);
            if (next) onSelect(next);
          }}
        >
          <SelectTrigger className="h-8 w-60 max-w-full" aria-label="Verify app">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {apps.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate">{a.name}</span>
                  <span className="font-mono text-[0.68rem] text-muted-foreground">{a.slug}</span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
          {app.is_default ? (
            <span>Default app: used when a request names no app.</span>
          ) : (
            <>
              <span>
                Send with{' '}
                <code className="font-mono text-foreground">app: &apos;{app.slug}&apos;</code>
              </span>
              <CopyButton value={app.slug} label="Copy app slug" />
            </>
          )}
        </div>
      </div>
      {canAdmin ? (
        <div className="flex shrink-0 items-center gap-1">
          {app.is_default ? null : (
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground hover:text-destructive"
              onClick={() => setDeleting(true)}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              Delete app
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={() => setCreating((n) => n + 1)}>
            <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
            New app
          </Button>
        </div>
      ) : null}

      <NewAppDialog
        key={creating}
        open={creating > 0}
        projectId={projectId}
        onClose={() => setCreating(0)}
        onCreated={(a) => {
          setCreating(0);
          onCreated(a);
        }}
      />
      <DeleteAppDialog
        projectId={projectId}
        app={deleting ? app : null}
        onClose={() => setDeleting(false)}
        onDeleted={() => {
          setDeleting(false);
          const fallback = apps.find((a) => a.is_default);
          if (fallback) onSelect(fallback);
        }}
      />
    </div>
  );
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40);
}

function NewAppDialog({
  open,
  projectId,
  onClose,
  onCreated,
}: {
  open: boolean;
  projectId: string;
  onClose: () => void;
  onCreated: (app: CreatedVerifyApp) => void;
}) {
  const create = useCreateVerifyApp(projectId);
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const suggested = slugify(name);
  const slugError =
    slug && !/^[a-z0-9][a-z0-9-]{0,39}$/.test(slug)
      ? 'Use lowercase letters, digits and hyphens, starting with a letter or digit.'
      : '';

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (slugError) return;
    setErrors({});
    try {
      const app = await create.mutateAsync({ name: name.trim(), slug: slug || undefined });
      toast.success(`${app.name} created`);
      onCreated(app);
    } catch (err) {
      const fe = fieldErrors(err);
      setErrors(fe);
      if (!fe.name && !fe.slug) showError(err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>New Verify app</DialogTitle>
            <DialogDescription>
              An app has its own message, limits, fraud protection and widget. Use one per product
              or customer. It starts with the default settings.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-app-name">Name</Label>
            <Input
              id="new-app-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Checkout"
              maxLength={60}
              required
              autoFocus
              aria-invalid={errors.name ? true : undefined}
            />
            <FieldNote error={errors.name}>
              Also shown in the SMS as <code className="font-mono">{'{app}'}</code>, unless you
              change it.
            </FieldNote>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="new-app-slug">
              Slug <span className="font-normal text-muted-foreground">(optional)</span>
            </Label>
            <Input
              id="new-app-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value.toLowerCase())}
              placeholder={suggested || 'checkout'}
              maxLength={40}
              className="font-mono"
              aria-invalid={slugError || errors.slug ? true : undefined}
              aria-describedby="new-app-slug-note"
            />
            <FieldNote id="new-app-slug-note" error={slugError || errors.slug}>
              What your server passes as <code className="font-mono">app</code>. It cannot be
              changed later.
            </FieldNote>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending || !name.trim() || slugError !== ''}>
              {create.isPending ? 'Creating…' : 'Create app'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** The new app's signing secret, shown once. Lives on the page so switching apps keeps it open. */
export function CreatedDialog({
  app,
  onClose,
}: {
  app: CreatedVerifyApp | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={app !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{app?.name} is ready</DialogTitle>
          <DialogDescription>
            {app?.secret
              ? 'Copy the token signing secret now. It is shown once here; later an admin can reveal it again, and every reveal is recorded in the audit log.'
              : 'Send codes with this app by passing its slug as app.'}
          </DialogDescription>
        </DialogHeader>
        {app ? (
          <div className="flex min-w-0 flex-col gap-4">
            {app.secret ? (
              <>
                <div className="flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 py-1 pr-1 pl-3">
                  <code className="min-w-0 flex-1 break-all font-mono text-xs text-foreground">
                    {app.secret}
                  </code>
                  <CopyButton value={app.secret} label="Copy secret" showLabel />
                </div>
                <p className="flex items-start gap-2 text-xs/relaxed text-muted-foreground">
                  <HugeiconsIcon
                    icon={Alert02Icon}
                    strokeWidth={2}
                    className="mt-0.5 size-3.5 shrink-0 text-warning"
                  />
                  Your server uses it to check the tokens the widget and hosted page return (HS256).
                  Store it with your server&apos;s secrets, for example as BRIDGE_VERIFY_SECRET.
                  Never ship it in a browser or mobile app.
                </p>
              </>
            ) : (
              <p className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/8 p-3 text-xs/relaxed">
                <HugeiconsIcon
                  icon={Alert02Icon}
                  strokeWidth={2}
                  className="mt-0.5 size-3.5 shrink-0 text-warning"
                />
                This server has no BRIDGE_SECRET_KEY, so the app has no token signing secret. Codes
                sent from your server work; the widget and hosted page need the key to issue tokens.
              </p>
            )}
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-xs">
              <dt className="text-muted-foreground">Slug</dt>
              <dd className="truncate font-mono">{app.slug}</dd>
              <dt className="text-muted-foreground">Publishable key</dt>
              <dd className="truncate font-mono">{app.publishable_key}</dd>
            </dl>
          </div>
        ) : null}
        <DialogFooter>
          <Button onClick={onClose}>{app?.secret ? 'I have saved the secret' : 'Done'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteAppDialog({
  projectId,
  app,
  onClose,
  onDeleted,
}: {
  projectId: string;
  app: VerifyApp | null;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const del = useDeleteVerifyApp(projectId);
  const [confirm, setConfirm] = useState('');

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!app || confirm !== app.slug) return;
    try {
      await del.mutateAsync(app.id);
      toast.success(`Deleted ${app.name}`);
      setConfirm('');
      onDeleted();
    } catch (err) {
      showError(err);
    }
  }

  return (
    <Dialog
      open={app !== null}
      onOpenChange={(o) => {
        if (!o) {
          setConfirm('');
          onClose();
        }
      }}
    >
      <DialogContent>
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>Delete {app?.name}?</DialogTitle>
            <DialogDescription>
              Its widget and hosted page stop working, tokens it signed stop verifying, and requests
              that name it fail. Past verifications are kept without an app. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="delete-app-confirm">
              Type <span className="font-mono">{app?.slug}</span> to confirm
            </Label>
            <Input
              id="delete-app-confirm"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="destructive"
              disabled={del.isPending || !app || confirm !== app.slug}
            >
              {del.isPending ? 'Deleting…' : 'Delete app'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
