'use client';

import { useRouter } from 'next/navigation';
import { type FormEvent, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { CopyField } from '@/components/kit/copy-button';
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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { showError } from '@/lib/errors';
import { formatDateTime } from '@/lib/format';
import { useDeleteProject, useProject, useRenameProject } from '@/lib/queries';

export default function ProjectSettingsPage() {
  const projectId = useProjectId() ?? '';
  const project = useProject(projectId);
  const rename = useRenameProject(projectId);
  const canDelete = useCan('owner');
  const canEdit = useCan('admin');
  const [name, setName] = useState('');

  useEffect(() => {
    if (project.data) setName(project.data.name);
  }, [project.data]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await rename.mutateAsync(name);
      toast.success('Project renamed');
    } catch (err) {
      showError(err);
    }
  }

  const unchanged = name.trim() === (project.data?.name ?? '');

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <PageHeader title="Settings" subtitle="Project name and identifiers." />
      <SectionCard title="General">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="project-name">Project name</Label>
            <div className="flex gap-2">
              <Input
                id="project-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={80}
                required
                disabled={!canEdit}
                className="max-w-sm"
              />
              <Button
                type="submit"
                size="lg"
                disabled={!canEdit || rename.isPending || unchanged || !name.trim()}
              >
                {rename.isPending ? 'Saving…' : 'Save'}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Renaming does not change the project ID or slug.
            </p>
          </div>
        </form>
      </SectionCard>
      <SectionCard title="Identifiers">
        <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="flex min-w-0 flex-col gap-1.5">
            <dt className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
              Project ID
            </dt>
            <dd>
              <CopyField value={projectId} />
            </dd>
          </div>
          <div className="flex min-w-0 flex-col gap-1.5">
            <dt className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
              Slug
            </dt>
            <dd>
              <CopyField value={project.data?.slug ?? ''} />
            </dd>
          </div>
          <div className="flex flex-col gap-1.5">
            <dt className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
              Created
            </dt>
            <dd className="text-sm">{formatDateTime(project.data?.created_at)}</dd>
          </div>
          <div className="flex flex-col gap-1.5">
            <dt className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
              Updated
            </dt>
            <dd className="text-sm">{formatDateTime(project.data?.updated_at)}</dd>
          </div>
        </dl>
      </SectionCard>
      {canDelete ? <DangerZone projectId={projectId} name={project.data?.name ?? ''} /> : null}
    </div>
  );
}

function DangerZone({ projectId, name }: { projectId: string; name: string }) {
  const del = useDeleteProject(projectId);
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [confirm, setConfirm] = useState('');
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await del.mutateAsync(confirm);
      toast.success(`Deleted ${name}`);
      router.replace('/');
      router.refresh();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <section className="rounded-xl border border-destructive/40 bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
        <div className="min-w-0">
          <h2 className="font-display text-sm font-semibold">Delete project</h2>
          <p className="mt-0.5 text-xs/relaxed text-muted-foreground">
            Permanently deletes its messages, API keys, phones, webhooks and logs. Paired phones are
            disconnected.
          </p>
        </div>
        <Button variant="destructive" onClick={() => setOpen(true)}>
          Delete project
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          setOpen(o);
          setConfirm('');
        }}
      >
        <DialogContent>
          <form onSubmit={submit} className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Delete {name}?</DialogTitle>
              <DialogDescription>
                This cannot be undone. API keys stop working at once and every phone paired to this
                project is disconnected.
              </DialogDescription>
            </DialogHeader>
            <div className="flex flex-col gap-2">
              <Label htmlFor="delete-confirm">
                Type <span className="font-mono">{name}</span> to confirm
              </Label>
              <Input
                id="delete-confirm"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="off"
                required
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="destructive"
                disabled={del.isPending || confirm !== name}
              >
                {del.isPending ? 'Deleting…' : 'Delete project'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}
