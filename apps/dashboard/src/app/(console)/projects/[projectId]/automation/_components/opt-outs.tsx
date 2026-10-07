'use client';

import type { OptOut } from '@bridge/api-types';
import {
  Delete02Icon,
  Download01Icon,
  Search01Icon,
  UserBlock01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { EmptyState } from '@/components/kit/empty-state';
import { SectionCard } from '@/components/kit/section-card';
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
import { normalisePhone } from '@/lib/csv';
import { fieldErrors, showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import { downloadOptOuts, useAddOptOut, useOptOuts, useRemoveOptOut } from '@/lib/queries';

type Source = OptOut['source'];

const SOURCES: { value: Source | 'all'; label: string }[] = [
  { value: 'all', label: 'All sources' },
  { value: 'keyword', label: 'Replied with a keyword' },
  { value: 'manual', label: 'Added in the dashboard' },
  { value: 'api', label: 'Added with the API' },
];

function SourceLabel({ o }: { o: OptOut }) {
  if (o.source === 'keyword') {
    return (
      <span>
        Replied <span className="font-mono text-xs">{o.keyword ?? 'STOP'}</span>
      </span>
    );
  }
  return <span>{o.source === 'manual' ? 'Dashboard' : 'API'}</span>;
}

export function OptOuts({ projectId }: { projectId: string }) {
  const canAdmin = useCan('admin');
  const [source, setSource] = useState<Source | 'all'>('all');
  const [search, setSearch] = useState('');
  const [number, setNumber] = useState('');
  const [numberError, setNumberError] = useState('');
  const [removing, setRemoving] = useState<OptOut | null>(null);
  const [exporting, setExporting] = useState(false);
  const list = useOptOuts(projectId, source === 'all' ? undefined : source);
  const add = useAddOptOut(projectId);
  const all = list.data?.pages.flatMap((p) => p.data) ?? [];
  const q = search.replace(/[\s\-().]/g, '');
  const rows = q ? all.filter((o) => o.number.includes(q)) : all;
  const normalised = number.trim() ? normalisePhone(number) : null;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setNumberError('');
    try {
      const o = await add.mutateAsync(number.trim());
      toast.success(`${o.number} opted out`);
      setNumber('');
    } catch (err) {
      const fe = fieldErrors(err);
      if (fe.number) setNumberError(fe.number);
      else showError(err);
    }
  }

  async function exportCsv() {
    setExporting(true);
    try {
      await downloadOptOuts(projectId);
    } catch (err) {
      showError(err);
    } finally {
      setExporting(false);
    }
  }

  return (
    <SectionCard
      title="Opt-out list"
      description="Numbers that asked not to hear from this project, shared by Live and Test. Messages, broadcasts and schedules to them are refused with opted_out."
      contentClassName="p-0"
      action={
        <Button variant="outline" onClick={exportCsv} disabled={exporting}>
          <HugeiconsIcon icon={Download01Icon} strokeWidth={2} />
          {exporting ? 'Exporting…' : 'Export CSV'}
        </Button>
      }
    >
      <div className="flex flex-col gap-4 border-b p-5">
        <p className="text-xs/relaxed text-muted-foreground">
          One-time passwords from Verify still go to these numbers, so people can always sign in.
          Remove a number only when the person asked to receive messages again, for example by
          texting START.
        </p>
        {canAdmin ? (
          <form onSubmit={submit} className="flex flex-col gap-1.5">
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                aria-label="Number to opt out"
                value={number}
                onChange={(e) => {
                  setNumber(e.target.value);
                  setNumberError('');
                }}
                placeholder="+919876543210"
                className="font-mono sm:max-w-xs"
                aria-invalid={numberError ? true : undefined}
              />
              <Button type="submit" variant="outline" disabled={add.isPending || !number.trim()}>
                {add.isPending ? 'Adding…' : 'Add number'}
              </Button>
            </div>
            {numberError ? (
              <p className="text-xs text-destructive">{numberError}</p>
            ) : normalised && !normalised.ok ? (
              <p className="text-xs text-muted-foreground">
                Use E.164: a + and the country code, then the number, like +919876543210.
              </p>
            ) : normalised?.ok && normalised.value !== number.trim() ? (
              <p className="text-xs text-muted-foreground">
                Saved as <span className="font-mono">{normalised.value}</span>.
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                With the country code. Spaces and dashes are removed.
              </p>
            )}
          </form>
        ) : null}
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="relative sm:max-w-xs sm:flex-1">
            <HugeiconsIcon
              icon={Search01Icon}
              strokeWidth={2}
              className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              aria-label="Search numbers"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search numbers"
              className="pl-8 font-mono"
            />
          </div>
          <Select value={source} onValueChange={(v) => setSource(v as Source | 'all')}>
            <SelectTrigger className="w-full sm:w-52" aria-label="Source">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SOURCES.map((s) => (
                <SelectItem key={s.value} value={s.value}>
                  {s.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      ) : list.isError ? (
        <EmptyState
          title="Could not load the opt-out list"
          description={list.error.message}
          action={
            <Button variant="outline" onClick={() => list.refetch()}>
              Try again
            </Button>
          }
        />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={UserBlock01Icon} strokeWidth={1.8} />}
          title={q || source !== 'all' ? 'No numbers match' : 'Nobody has opted out'}
          description={
            q && list.hasNextPage
              ? 'Search covers the numbers loaded so far. Load more to search further.'
              : q || source !== 'all'
                ? undefined
                : 'Numbers appear here when someone replies STOP or a similar keyword, or when you add them.'
          }
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-5">Number</TableHead>
              <TableHead>Source</TableHead>
              <TableHead>Added</TableHead>
              <TableHead className="w-24 pr-5">
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((o) => (
              <TableRow key={o.id}>
                <TableCell className="pl-5 font-mono text-xs">{o.number}</TableCell>
                <TableCell>
                  <SourceLabel o={o} />
                </TableCell>
                <TableCell className="text-muted-foreground" title={formatDateTime(o.created_at)}>
                  {formatRelative(o.created_at)}
                </TableCell>
                <TableCell className="pr-5 text-right">
                  {canAdmin ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setRemoving(o)}
                    >
                      <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                      Remove
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
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

      <RemoveDialog projectId={projectId} optOut={removing} onClose={() => setRemoving(null)} />
    </SectionCard>
  );
}

function RemoveDialog({
  projectId,
  optOut,
  onClose,
}: {
  projectId: string;
  optOut: OptOut | null;
  onClose: () => void;
}) {
  const remove = useRemoveOptOut(projectId);
  async function confirm() {
    if (!optOut) return;
    try {
      await remove.mutateAsync(optOut.number);
      toast.success(`${optOut.number} can receive messages again`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={optOut !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Opt {optOut?.number} back in?</DialogTitle>
          <DialogDescription>
            Messages, broadcasts and schedules can reach this number again. Only do this if the
            person asked to receive messages again.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Removing…' : 'Remove from list'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
