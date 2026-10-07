'use client';

import type { Broadcast, BroadcastCreateInput, BroadcastPreview } from '@bridge/api-types';
import {
  Alert02Icon,
  Cancel01Icon,
  Csv01Icon,
  SentIcon,
  ViewIcon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type DragEvent, useDeferredValue, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Segmented } from '@/components/kit/segmented';
import { useCan } from '@/components/layout/console-context';
import { DeviceSelect, EnvironmentChoice, FieldNote, SegmentLine } from '@/components/messaging';
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
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { BridgeApiError } from '@/lib/api';
import { guessPhoneColumn, placeholders, renderTemplate } from '@/lib/csv';
import { showError } from '@/lib/errors';
import { formatDateTime } from '@/lib/format';
import {
  type Environment,
  useBrowserTimeZone,
  useCreateBroadcast,
  usePreviewBroadcast,
} from '@/lib/queries';
import { segmentInfo } from '@/lib/sms';
import { cn } from '@/lib/utils';
import {
  buildRecipients,
  firstRowIsData,
  lineForLocation,
  MAX_RECIPIENTS,
  MAX_VARS,
  readTable,
  variablesOf,
} from './recipients';

const number = new Intl.NumberFormat('en');
const MAX_FILE_BYTES = 5 * 1024 * 1024;

const SAMPLE = 'phone,first_name,order\n+15550000001,Asha,ORD-1042\n+15550000005,Ben,ORD-1043';

function Step({
  n,
  title,
  children,
  aside,
}: {
  n: number;
  title: string;
  children: React.ReactNode;
  aside?: React.ReactNode;
}) {
  return (
    <section className="flex min-w-0 flex-col gap-3 border-b px-5 py-5 last:border-b-0">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="flex items-baseline gap-2 text-sm font-medium">
          <span className="font-mono text-xs text-faint">{n}</span>
          {title}
        </h3>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** The next full hour, in the browser's zone, as a starting point for the send-later fields. */
function defaultLater(): { date: string; time: string } {
  const d = new Date(Date.now() + 60 * 60 * 1000);
  d.setMinutes(0, 0, 0);
  const pad = (n: number) => String(n).padStart(2, '0');
  return {
    date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`,
    time: `${pad(d.getHours())}:${pad(d.getMinutes())}`,
  };
}

type ApiProblem = { message: string; line: number | null };

export function Composer({
  projectId,
  onSent,
}: {
  projectId: string;
  onSent: (b: Broadcast) => void;
}) {
  const canAdmin = useCan('admin');
  const tz = useBrowserTimeZone();
  const previewMut = usePreviewBroadcast(projectId);
  const create = useCreateBroadcast(projectId);

  const [environment, setEnvironment] = useState<Environment>('test');
  const [source, setSource] = useState<'upload' | 'paste'>('upload');
  const [fileName, setFileName] = useState('');
  const [fileText, setFileText] = useState('');
  const [pasteText, setPasteText] = useState('');
  const [header, setHeader] = useState(true);
  const [phoneColumn, setPhoneColumn] = useState(0);
  const [defaultCode, setDefaultCode] = useState('');
  const [template, setTemplate] = useState('');
  const [deviceId, setDeviceId] = useState('auto');
  const [later, setLater] = useState(false);
  const [laterDate, setLaterDate] = useState('');
  const [laterTime, setLaterTime] = useState('');
  const [name, setName] = useState('');
  const [dragging, setDragging] = useState(false);
  const [preview, setPreview] = useState<{ data: BroadcastPreview; key: string } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [confirming, setConfirming] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const templateRef = useRef<HTMLTextAreaElement>(null);

  const rawText = useDeferredValue(source === 'upload' ? fileText : pasteText);
  const deferredTemplate = useDeferredValue(template);
  const table = useMemo(
    () => (rawText.trim() ? readTable(rawText, header) : null),
    [rawText, header],
  );
  const column = table && phoneColumn < table.headers.length ? phoneColumn : 0;
  const variables = useMemo(() => (table ? variablesOf(table, column) : []), [table, column]);
  const used = useMemo(() => placeholders(deferredTemplate), [deferredTemplate]);
  const unknown = used.filter((u) => !variables.some((v) => v.name === u));
  const list = useMemo(
    () => (table ? buildRecipients(table, column, variables, used, defaultCode, header) : null),
    [table, column, variables, used, defaultCode, header],
  );

  // The longest message once each row's values are filled in.
  const longest = useMemo(() => {
    if (!list || list.recipients.length === 0 || !deferredTemplate) return null;
    let best = { text: '', line: 0, segments: 0, units: -1 };
    let total = 0;
    list.recipients.forEach((r, i) => {
      const text = renderTemplate(deferredTemplate, r.vars ?? {});
      const s = segmentInfo(text);
      total += s.segments;
      if (s.segments > best.segments || (s.segments === best.segments && s.units > best.units)) {
        best = { text, line: list.lines[i] ?? 0, segments: s.segments, units: s.units };
      }
    });
    return { ...best, total };
  }, [list, deferredTemplate]);

  function scheduledAt(): string | undefined {
    if (!later || !laterDate || !laterTime) return undefined;
    const d = new Date(`${laterDate}T${laterTime}`);
    return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
  }

  const body: BroadcastCreateInput | null = useMemo(() => {
    if (!list || list.recipients.length === 0 || !template.trim()) return null;
    const b: BroadcastCreateInput = { template, recipients: list.recipients };
    if (name.trim()) b.name = name.trim();
    if (environment === 'live' && deviceId !== 'auto') b.device_id = deviceId;
    const at = later && laterDate && laterTime ? new Date(`${laterDate}T${laterTime}`) : null;
    if (at && !Number.isNaN(at.getTime())) b.scheduled_at = at.toISOString();
    return b;
  }, [list, template, name, environment, deviceId, later, laterDate, laterTime]);
  const bodyKey = useMemo(
    () => (body ? `${environment}|${JSON.stringify(body)}` : ''),
    [body, environment],
  );
  const fresh = preview !== null && preview.key === bodyKey;

  const laterError = (() => {
    if (!later) return '';
    if (!laterDate || !laterTime) return 'Choose a date and time.';
    const at = new Date(`${laterDate}T${laterTime}`);
    if (Number.isNaN(at.getTime())) return 'Choose a valid date and time.';
    if (at.getTime() < Date.now()) return 'This time has already passed.';
    if (at.getTime() > Date.now() + 365 * 24 * 3600 * 1000) return 'Schedule at most a year ahead.';
    return '';
  })();

  const blockers: string[] = [];
  if (!table) blockers.push('Add recipients.');
  else if (list && list.recipients.length === 0) blockers.push('No row has a valid phone number.');
  if (list && list.recipients.length > MAX_RECIPIENTS) {
    blockers.push(
      `A broadcast can have at most ${number.format(MAX_RECIPIENTS)} recipients. Split the list.`,
    );
  }
  if (!template.trim()) blockers.push('Write the message.');
  if (unknown.length) {
    blockers.push(`No column for ${unknown.map((u) => `{${u}}`).join(', ')}.`);
  }
  if (used.length > MAX_VARS) blockers.push(`Use at most ${MAX_VARS} different placeholders.`);
  if (laterError) blockers.push(laterError);
  if (environment === 'live' && !canAdmin) blockers.push('Only admins can send live messages.');

  function loadFile(file: File | undefined) {
    if (!file) return;
    if (file.size > MAX_FILE_BYTES) {
      toast.error('This file is larger than 5 MB. Split the list into smaller files.');
      return;
    }
    file
      .text()
      .then((text) => {
        const noHeader = firstRowIsData(text);
        setHeader(!noHeader);
        const t = readTable(text, !noHeader);
        setPhoneColumn(guessPhoneColumn(t));
        setFileName(file.name);
        setFileText(text);
        setProblem(null);
      })
      .catch(() => toast.error('Could not read this file.'));
  }

  function onPaste(text: string) {
    setPasteText(text);
    setProblem(null);
    if (text.trim()) {
      const noHeader = firstRowIsData(text);
      setHeader(!noHeader);
      setPhoneColumn(guessPhoneColumn(readTable(text, !noHeader)));
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    setDragging(false);
    loadFile(e.dataTransfer.files[0]);
  }

  function insertVariable(v: string) {
    const el = templateRef.current;
    const token = `{${v}}`;
    if (!el) {
      setTemplate((t) => t + token);
      return;
    }
    const start = el.selectionStart ?? template.length;
    const end = el.selectionEnd ?? template.length;
    const next = template.slice(0, start) + token + template.slice(end);
    setTemplate(next);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(start + token.length, start + token.length);
    });
  }

  function explain(err: unknown) {
    if (err instanceof BridgeApiError && err.status === 422 && list) {
      const d = err.details?.[0];
      setProblem({
        message: d?.message ?? err.message,
        line: d?.location ? lineForLocation(d.location.replace(/^body\./, ''), list.lines) : null,
      });
      return;
    }
    if (err instanceof BridgeApiError && err.status !== 401) {
      setProblem({ message: err.message, line: null });
      return;
    }
    showError(err);
  }

  async function runPreview() {
    if (!body) return;
    setProblem(null);
    try {
      const data = await previewMut.mutateAsync({ environment, body });
      setPreview({ data, key: bodyKey });
    } catch (err) {
      setPreview(null);
      explain(err);
    }
  }

  async function send() {
    if (!body) return;
    try {
      const b = await create.mutateAsync({ environment, body });
      setConfirming(false);
      setPreview(null);
      toast.success(
        b.status === 'scheduled'
          ? `Broadcast scheduled for ${formatDateTime(b.scheduled_at)}`
          : `Sending to ${number.format(b.counts.recipients)} recipients`,
      );
      onSent(b);
    } catch (err) {
      setConfirming(false);
      explain(err);
    }
  }

  function clearRecipients() {
    setFileName('');
    setFileText('');
    setPasteText('');
    setPreview(null);
    setProblem(null);
    if (fileRef.current) fileRef.current.value = '';
  }

  const at = scheduledAt();
  const firstRendered =
    list?.recipients[0] && template ? renderTemplate(template, list.recipients[0].vars ?? {}) : '';

  return (
    <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
      <div className="flex min-w-0 flex-col rounded-xl border bg-card">
        <div className="border-b px-5 py-3.5">
          <h2 className="font-display text-sm font-semibold">New broadcast</h2>
          <p className="mt-0.5 text-xs/relaxed text-muted-foreground">
            One message to up to {number.format(MAX_RECIPIENTS)} numbers, personalised from your CSV
            columns.
          </p>
        </div>

        <Step n={1} title="Environment">
          <EnvironmentChoice
            name="broadcast-environment"
            value={environment}
            onChange={(v) => {
              setEnvironment(v);
              setPreview(null);
            }}
            canLive={canAdmin}
            hideLegend
          />
          {environment === 'test' ? (
            <p className="text-xs/relaxed text-muted-foreground">
              Test numbers such as +15550000001 (delivered) and +15550000005 (undelivered) show how
              each outcome looks.
            </p>
          ) : null}
        </Step>

        <Step
          n={2}
          title="Recipients"
          aside={
            <Segmented
              label="Recipients source"
              value={source}
              onChange={(v) => {
                setSource(v);
                setPreview(null);
                setProblem(null);
              }}
              options={[
                { value: 'upload', label: 'Upload CSV' },
                { value: 'paste', label: 'Paste' },
              ]}
            />
          }
        >
          {source === 'upload' ? (
            fileText ? (
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-background px-3 py-2">
                <span className="flex min-w-0 items-center gap-2 text-sm">
                  <HugeiconsIcon
                    icon={Csv01Icon}
                    strokeWidth={1.8}
                    className="size-4 shrink-0 text-muted-foreground"
                  />
                  <span className="truncate font-medium">{fileName}</span>
                </span>
                <span className="flex gap-1">
                  <Button variant="ghost" size="sm" onClick={() => fileRef.current?.click()}>
                    Replace
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={clearRecipients}
                    aria-label="Remove file"
                  >
                    <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
                  </Button>
                </span>
              </div>
            ) : (
              // biome-ignore lint/a11y/noStaticElementInteractions: drop target; the button inside is the keyboard path
              <div
                onDragOver={(e) => {
                  e.preventDefault();
                  setDragging(true);
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={onDrop}
                className={cn(
                  'flex flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-7 text-center transition-colors',
                  dragging && 'border-primary bg-primary/5',
                )}
              >
                <HugeiconsIcon
                  icon={Csv01Icon}
                  strokeWidth={1.6}
                  className="size-6 text-muted-foreground"
                />
                <span className="text-sm font-medium">Drop a CSV file here</span>
                <span className="max-w-sm text-xs/relaxed text-muted-foreground">
                  First row: column names. One column holds the phone numbers; the others become
                  placeholders like {'{first_name}'}.
                </span>
                <Button variant="outline" className="mt-1" onClick={() => fileRef.current?.click()}>
                  Choose file
                </Button>
              </div>
            )
          ) : (
            <div className="flex flex-col gap-2">
              <Textarea
                aria-label="Recipients"
                value={pasteText}
                onChange={(e) => onPaste(e.target.value)}
                placeholder={SAMPLE}
                rows={6}
                className="font-mono text-xs"
                spellCheck={false}
              />
              <p className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>Paste rows from a spreadsheet, or one number per line.</span>
                {!pasteText ? (
                  <button
                    type="button"
                    className="font-medium text-primary hover:underline"
                    onClick={() => onPaste(SAMPLE)}
                  >
                    Use test numbers
                  </button>
                ) : null}
              </p>
            </div>
          )}
          <input
            ref={fileRef}
            type="file"
            accept=".csv,.tsv,.txt,text/csv,text/plain"
            className="sr-only"
            tabIndex={-1}
            onChange={(e) => loadFile(e.target.files?.[0])}
            id="broadcast-file"
          />

          {table && list ? (
            <div className="flex flex-col gap-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="broadcast-phone-column">Phone number column</Label>
                  <Select value={String(column)} onValueChange={(v) => setPhoneColumn(Number(v))}>
                    <SelectTrigger id="broadcast-phone-column" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {table.headers.map((h, i) => (
                        // biome-ignore lint/suspicious/noArrayIndexKey: columns are positional
                        <SelectItem key={i} value={String(i)}>
                          {h}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="broadcast-country">
                    Country code{' '}
                    <span className="font-normal text-muted-foreground">(optional)</span>
                  </Label>
                  <Input
                    id="broadcast-country"
                    value={defaultCode}
                    onChange={(e) => setDefaultCode(e.target.value.replace(/[^\d+]/g, ''))}
                    placeholder="+91"
                    className="font-mono"
                    maxLength={5}
                  />
                </div>
              </div>
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={header}
                  onChange={(e) => setHeader(e.target.checked)}
                  className="size-3.5 accent-primary"
                />
                The first row holds column names
              </label>
              <p className="text-xs/relaxed text-muted-foreground">
                Numbers need a country code, like +919876543210. The country code above is added to
                numbers written without one.
              </p>

              <dl className="grid grid-cols-3 gap-3 rounded-lg border bg-background p-3">
                <div className="flex flex-col gap-0.5">
                  <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                    Rows
                  </dt>
                  <dd className="font-display text-lg font-semibold tabular-nums">
                    {number.format(list.total)}
                  </dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                    Valid numbers
                  </dt>
                  <dd className="font-display text-lg font-semibold tabular-nums">
                    {number.format(list.recipients.length)}
                  </dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
                    Left out
                  </dt>
                  <dd
                    className={cn(
                      'font-display text-lg font-semibold tabular-nums',
                      list.invalid.length > 0 && 'text-warning',
                    )}
                  >
                    {number.format(list.invalid.length)}
                  </dd>
                </div>
              </dl>

              {list.invalid.length > 0 ? (
                <div className="overflow-hidden rounded-lg border">
                  <p className="border-b bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
                    These rows are left out. Fix them in the file to include them.
                  </p>
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="text-left text-muted-foreground">
                        <th className="w-16 px-3 py-1.5 font-medium">Line</th>
                        <th className="px-3 py-1.5 font-medium">Value</th>
                        <th className="px-3 py-1.5 font-medium">Problem</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y border-t">
                      {list.invalid.slice(0, 8).map((r) => (
                        <tr key={r.line}>
                          <td className="px-3 py-1.5 tabular-nums text-muted-foreground">
                            {r.line}
                          </td>
                          <td className="max-w-[10rem] truncate px-3 py-1.5 font-mono">
                            {r.value || <span className="text-muted-foreground">empty</span>}
                          </td>
                          <td className="px-3 py-1.5">{r.reason}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {list.invalid.length > 8 ? (
                    <p className="border-t px-3 py-1.5 text-xs text-muted-foreground">
                      and {number.format(list.invalid.length - 8)} more
                    </p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </Step>

        <Step n={3} title="Message">
          {variables.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">Insert a column</span>
              <div className="flex flex-wrap gap-1.5">
                {variables.map((v) => (
                  <button
                    key={v.name}
                    type="button"
                    onClick={() => insertVariable(v.name)}
                    title={v.header !== v.name ? `Column “${v.header}”` : undefined}
                    className={cn(
                      'rounded-md border px-2 py-0.5 font-mono text-[0.7rem] transition-colors hover:border-primary/50 hover:bg-primary/5',
                      used.includes(v.name)
                        ? 'border-primary/40 bg-primary/5 text-foreground'
                        : 'bg-background text-muted-foreground',
                    )}
                  >
                    {`{${v.name}}`}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="broadcast-template" className="sr-only">
              Message template
            </Label>
            <Textarea
              id="broadcast-template"
              ref={templateRef}
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder={
                variables[0]
                  ? `Hi {${variables[0].name}}, your order has shipped.`
                  : 'Your order has shipped.'
              }
              maxLength={1600}
              rows={5}
              aria-invalid={unknown.length > 0 || undefined}
            />
            {unknown.length > 0 ? (
              <p className="text-xs text-destructive">
                {unknown.map((u) => `{${u}}`).join(', ')} {unknown.length > 1 ? 'are' : 'is'} not a
                column in the recipients. Every placeholder needs a value for every row.
              </p>
            ) : null}
            {longest ? (
              <>
                <SegmentLine
                  text={longest.text}
                  prefix={
                    list && list.recipients.length > 1
                      ? `Longest message (line ${longest.line})`
                      : 'Message'
                  }
                />
                <p className="text-xs text-muted-foreground">
                  Up to {number.format(longest.total)} segment{longest.total === 1 ? '' : 's'} in
                  total; Preview removes repeated and opted-out numbers. Carriers bill per segment.
                </p>
              </>
            ) : template ? (
              <SegmentLine text={template} />
            ) : null}
          </div>
        </Step>

        <Step n={4} title="Delivery">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="broadcast-device">Phone</Label>
              <DeviceSelect
                id="broadcast-device"
                projectId={projectId}
                value={deviceId}
                onChange={setDeviceId}
                disabled={environment === 'test'}
              />
              <FieldNote>
                {environment === 'test'
                  ? 'Test messages go to the simulator.'
                  : 'Bridge picks spreads messages across phones and follows your routing.'}
              </FieldNote>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="broadcast-name">
                Name <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="broadcast-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="October offers"
                maxLength={100}
              />
            </div>
          </div>
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-2.5">
              <Switch
                id="broadcast-later"
                checked={later}
                onCheckedChange={(v) => {
                  setLater(v);
                  if (v && !laterDate) {
                    const d = defaultLater();
                    setLaterDate(d.date);
                    setLaterTime(d.time);
                  }
                }}
              />
              <Label htmlFor="broadcast-later" className="text-sm font-normal">
                Send later
              </Label>
            </div>
            {later ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="broadcast-date">Date</Label>
                  <Input
                    id="broadcast-date"
                    type="date"
                    value={laterDate}
                    onChange={(e) => setLaterDate(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="broadcast-time">Time</Label>
                  <Input
                    id="broadcast-time"
                    type="time"
                    value={laterTime}
                    onChange={(e) => setLaterTime(e.target.value)}
                  />
                </div>
                <div className="sm:col-span-2">
                  <FieldNote error={laterError}>
                    In your time zone{tz ? ` (${tz})` : ''}.
                    {at ? ` Sends ${formatDateTime(at)}.` : ''}
                  </FieldNote>
                </div>
              </div>
            ) : null}
          </div>
        </Step>
      </div>

      <div className="flex min-w-0 flex-col gap-4 xl:sticky xl:top-20">
        <div className="flex min-w-0 flex-col rounded-xl border bg-card">
          <div className="flex items-center justify-between gap-3 border-b px-5 py-3.5">
            <h2 className="font-display text-sm font-semibold">Preview</h2>
            {preview && !fresh ? <span className="text-xs text-warning">Out of date</span> : null}
          </div>
          <div className="flex flex-col gap-4 p-5">
            {preview ? (
              <PreviewResult preview={preview.data} environment={environment} />
            ) : firstRendered ? (
              <div className="flex flex-col gap-2">
                <span className="text-xs text-muted-foreground">
                  The first recipient, {list?.recipients[0]?.to}, gets:
                </span>
                <p className="whitespace-pre-wrap break-words rounded-lg border bg-background px-3 py-2 text-sm">
                  {firstRendered}
                </p>
                <p className="text-xs/relaxed text-muted-foreground">
                  Preview checks every row with Bridge, removes repeated numbers and numbers that
                  opted out, and counts the segments. Nothing is sent.
                </p>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                Add recipients and a message, then preview to see exactly what goes out.
              </p>
            )}

            {problem ? (
              <div className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
                <p className="font-semibold text-destructive">
                  {problem.line ? `Line ${problem.line} of the recipients` : 'Bridge refused this'}
                </p>
                <p className="mt-1 text-muted-foreground">{problem.message}</p>
              </div>
            ) : null}

            {blockers.length > 0 && (table || template) ? (
              <ul className="flex list-disc flex-col gap-1 pl-4 text-xs text-muted-foreground">
                {blockers.map((b) => (
                  <li key={b}>{b}</li>
                ))}
              </ul>
            ) : null}

            <div className="flex flex-col gap-2 sm:flex-row xl:flex-col">
              <Button
                variant={fresh ? 'outline' : 'default'}
                size="lg"
                className="sm:flex-1"
                onClick={runPreview}
                disabled={blockers.length > 0 || !body || previewMut.isPending}
              >
                <HugeiconsIcon icon={ViewIcon} strokeWidth={2} />
                {previewMut.isPending ? 'Checking…' : preview ? 'Preview again' : 'Preview'}
              </Button>
              <Button
                size="lg"
                className="sm:flex-1"
                variant={fresh ? 'default' : 'outline'}
                onClick={() => setConfirming(true)}
                disabled={!fresh || blockers.length > 0 || (preview?.data.recipients ?? 0) === 0}
              >
                <HugeiconsIcon icon={SentIcon} strokeWidth={2} />
                {at ? 'Schedule…' : 'Send…'}
              </Button>
            </div>
            {preview && !fresh ? (
              <p className="text-xs text-muted-foreground">
                Something changed since the preview. Preview again before sending.
              </p>
            ) : null}
          </div>
        </div>
      </div>

      <Dialog open={confirming} onOpenChange={(o) => !create.isPending && setConfirming(o)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {at ? 'Schedule' : 'Send'} {number.format(preview?.data.recipients ?? 0)}{' '}
              {environment} message{preview?.data.recipients === 1 ? '' : 's'}?
            </DialogTitle>
            <DialogDescription>
              {environment === 'live'
                ? `Real SMS to ${number.format(preview?.data.recipients ?? 0)} people, about ${number.format(preview?.data.total_segments ?? 0)} segments. Carrier charges apply.`
                : `Simulated in Test: nothing leaves Bridge. About ${number.format(preview?.data.total_segments ?? 0)} segments.`}{' '}
              {at
                ? `Sending starts ${formatDateTime(at)}. You can cancel until then.`
                : 'Sending starts now. You can cancel to stop the messages not yet sent.'}
            </DialogDescription>
          </DialogHeader>
          {environment === 'live' ? (
            <p className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/8 p-3 text-xs/relaxed">
              <HugeiconsIcon
                icon={Alert02Icon}
                strokeWidth={2}
                className="mt-0.5 size-3.5 shrink-0 text-warning"
              />
              Only message people who agreed to hear from you. Replies such as STOP opt them out
              automatically.
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button onClick={send} disabled={create.isPending}>
              {create.isPending ? 'Sending…' : at ? 'Schedule broadcast' : 'Send broadcast'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function PreviewResult({
  preview: p,
  environment,
}: {
  preview: BroadcastPreview;
  environment: Environment;
}) {
  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-3">
        {(
          [
            ['Recipients', p.recipients, environment === 'live' ? 'Real SMS' : 'Simulated'],
            ['Segments', p.total_segments, 'Billed by carriers'],
            ['Opted out', p.skipped_opted_out, 'Left out'],
            ['Repeated', p.duplicates, 'Sent once'],
          ] as const
        ).map(([label, value, note]) => (
          <div key={label} className="flex flex-col gap-0.5 rounded-lg border bg-background p-3">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="font-display text-xl font-semibold tabular-nums">
              {number.format(value)}
            </dd>
            <dd className="text-[0.7rem] text-muted-foreground">{note}</dd>
          </div>
        ))}
      </dl>
      {p.samples.length > 0 ? (
        <div className="flex flex-col gap-2">
          <span className="text-xs text-muted-foreground">
            First {p.samples.length === 1 ? 'message' : `${p.samples.length} messages`}
          </span>
          <ul className="flex flex-col gap-2">
            {p.samples.map((s) => (
              <li key={s.to} className="flex flex-col gap-1 rounded-lg border bg-background p-3">
                <span className="flex items-center justify-between gap-2 text-[0.7rem] text-muted-foreground">
                  <span className="font-mono">{s.to}</span>
                  <span>
                    {s.encoding === 'gsm7' ? 'GSM-7' : 'Unicode'} · {s.segments} segment
                    {s.segments === 1 ? '' : 's'}
                  </span>
                </span>
                <span className="whitespace-pre-wrap break-words text-sm">{s.text}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">
          Every number in the list opted out. Nothing would be sent.
        </p>
      )}
    </div>
  );
}
