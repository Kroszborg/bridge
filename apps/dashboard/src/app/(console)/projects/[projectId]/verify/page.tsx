'use client';

import type { OtpSettings, Verification, VerifyResult } from '@bridge/api-types';
import { PasswordValidationIcon, SentIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { Segmented } from '@/components/kit/segmented';
import { StatTile } from '@/components/kit/stat-tile';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan, useConsole, useProjectId } from '@/components/layout/console-context';
import { MessageStatus } from '@/components/messages';
import { Button } from '@/components/ui/button';
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { BridgeApiError } from '@/lib/api';
import { showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  useOtpPlayground,
  useOtpSettings,
  useUpdateOtpSettings,
  useVerificationStats,
  useVerifications,
  type VerificationFilters,
  type VerifyEnvironment,
} from '@/lib/queries';
import { segmentInfo } from '@/lib/sms';
import { cn } from '@/lib/utils';

const pct = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 });
const number = new Intl.NumberFormat('en');

const STATUS: Record<Verification['status'], { kind: StatusKind; label: string; live?: boolean }> =
  {
    pending: { kind: 'info', label: 'Pending', live: true },
    verified: { kind: 'success', label: 'Verified' },
    expired: { kind: 'neutral', label: 'Expired' },
    failed: { kind: 'danger', label: 'Too many attempts' },
    canceled: { kind: 'neutral', label: 'Replaced' },
  };

function VerificationStatus({ status }: { status: Verification['status'] }) {
  const s = STATUS[status];
  return (
    <StatusBadge kind={s.kind} live={s.live}>
      {s.label}
    </StatusBadge>
  );
}

/** Mirrors the server's rendering so the preview matches the SMS exactly. */
function renderMessage(
  template: string,
  app: string,
  code: string,
  ttlSeconds: number,
  domain: string,
): string {
  const minutes = Math.ceil(ttlSeconds / 60);
  let body = template
    .replaceAll('{code}', code)
    .replaceAll('{app}', app)
    .replaceAll('{minutes}', String(minutes));
  if (domain) body += `\n\n@${domain} #${code}`;
  return body;
}

function exampleCode(length: number): string {
  return '4829130123'.slice(0, length);
}

function duration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)} s`;
  return `${Math.round(seconds / 6) / 10} min`;
}

export default function VerifyPage() {
  const projectId = useProjectId() ?? '';
  const [environment, setEnvironment] = useState<VerifyEnvironment>('test');

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Verify"
        subtitle="One-time passwords that Bridge generates, sends and checks. Your app makes two calls and never stores a code."
        actions={
          <Segmented
            label="Environment"
            value={environment}
            onChange={setEnvironment}
            options={[
              { value: 'test', label: 'Test' },
              { value: 'live', label: 'Live' },
            ]}
          />
        }
      />
      <Stats projectId={projectId} environment={environment} />
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,26rem)_minmax(0,1fr)]">
        {/* Codes belong to one environment, so switching starts over. */}
        <TryIt key={environment} projectId={projectId} environment={environment} />
        <SettingsCard projectId={projectId} />
      </div>
      <Recent projectId={projectId} environment={environment} />
      <Integrate />
    </div>
  );
}

function Stats({ projectId, environment }: { projectId: string; environment: VerifyEnvironment }) {
  const stats = useVerificationStats(projectId, environment);
  if (stats.isPending) {
    return (
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
    );
  }
  const s = stats.data;
  if (!s) return null;
  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <StatTile
        label="Codes sent, last 30 days"
        value={number.format(s.total)}
        note={`${s.pending} pending`}
      />
      <StatTile
        label="Verified"
        value={number.format(s.verified)}
        note={`${s.failed} ran out of attempts`}
      />
      <StatTile
        label="Conversion"
        value={s.conversion_rate == null ? '—' : pct.format(s.conversion_rate)}
        note="Verified out of finished codes"
      />
      <StatTile
        label="Median time to verify"
        value={s.verified ? duration(s.median_seconds_to_verify) : '—'}
        note={`${s.expired} expired unused`}
      />
    </div>
  );
}

function TryIt({ projectId, environment }: { projectId: string; environment: VerifyEnvironment }) {
  const canAdmin = useCan('admin');
  const { send, verify } = useOtpPlayground(projectId);
  const [to, setTo] = useState(environment === 'test' ? '+15550000001' : '');
  const [sent, setSent] = useState<Verification | null>(null);
  const [code, setCode] = useState('');
  const [result, setResult] = useState<VerifyResult | null>(null);
  const liveBlocked = environment === 'live' && !canAdmin;

  async function onSend(e: FormEvent) {
    e.preventDefault();
    try {
      const v = await send.mutateAsync({ environment, to });
      setSent(v);
      setResult(null);
      setCode(v.code ?? '');
    } catch (err) {
      if (err instanceof BridgeApiError && err.code === 'rate_limited') toast.error(err.message);
      else showError(err);
    }
  }

  async function onVerify(e: FormEvent) {
    e.preventDefault();
    if (!sent) return;
    try {
      setResult(await verify.mutateAsync({ environment, id: sent.id, code }));
    } catch (err) {
      showError(err);
    }
  }

  return (
    <SectionCard
      title="Try it"
      description={
        environment === 'test'
          ? 'Simulated: nothing is sent, and the code is shown so you can finish the flow.'
          : 'Sends a real SMS from one of your phones. Enter the code you receive.'
      }
    >
      <div className="flex flex-col gap-5">
        <form onSubmit={onSend} className="flex flex-col gap-2">
          <Label htmlFor="otp-to">Phone number</Label>
          <div className="flex gap-2">
            <Input
              id="otp-to"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder="+919876543210"
              className="font-mono"
              required
            />
            <Button type="submit" disabled={send.isPending || !to.trim() || liveBlocked}>
              <HugeiconsIcon icon={SentIcon} strokeWidth={2} />
              {send.isPending ? 'Sending…' : sent ? 'Resend' : 'Send code'}
            </Button>
          </div>
          {liveBlocked ? (
            <p className="text-xs text-muted-foreground">Only admins can send live codes.</p>
          ) : null}
        </form>

        {sent ? (
          <form
            onSubmit={onVerify}
            className="flex flex-col gap-3 rounded-lg border bg-background/60 p-4"
          >
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="font-mono text-muted-foreground">{sent.id}</span>
              <span className="text-muted-foreground">
                expires {formatRelative(sent.expires_at)}
              </span>
            </div>
            {sent.code ? (
              <p className="text-sm">
                Test code{' '}
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono font-semibold tracking-widest">
                  {sent.code}
                </span>
              </p>
            ) : null}
            <Label htmlFor="otp-code">Code</Label>
            <div className="flex gap-2">
              <Input
                id="otp-code"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={10}
                className="font-mono tracking-widest"
                required
              />
              <Button
                type="submit"
                variant="outline"
                disabled={verify.isPending || code.length < 4}
              >
                Check
              </Button>
            </div>
            {result ? (
              <p
                role="status"
                className={cn(
                  'rounded-md px-3 py-2 text-sm',
                  result.valid ? 'bg-success/10 text-success' : 'bg-destructive/8 text-destructive',
                )}
              >
                {result.valid
                  ? 'Valid. The verification is complete.'
                  : result.verification.status === 'pending'
                    ? `Wrong code. ${result.verification.attempts_remaining} attempt${result.verification.attempts_remaining === 1 ? '' : 's'} left.`
                    : `Not valid: ${STATUS[result.verification.status].label.toLowerCase()}. Send a new code.`}
              </p>
            ) : null}
          </form>
        ) : null}
      </div>
    </SectionCard>
  );
}

function SettingsCard({ projectId }: { projectId: string }) {
  const settings = useOtpSettings(projectId);
  if (settings.isPending) {
    return (
      <SectionCard title="Message and limits">
        <Skeleton className="h-64" />
      </SectionCard>
    );
  }
  if (!settings.data) return null;
  return <SettingsForm projectId={projectId} initial={settings.data} />;
}

function SettingsForm({ projectId, initial }: { projectId: string; initial: OtpSettings }) {
  const canAdmin = useCan('admin');
  const update = useUpdateOtpSettings(projectId);
  const [appName, setAppName] = useState(initial.app_name ?? '');
  const [template, setTemplate] = useState(initial.template ?? '');
  const [codeLength, setCodeLength] = useState(String(initial.code_length));
  const [ttl, setTtl] = useState(String(initial.ttl_seconds));
  const [attempts, setAttempts] = useState(String(initial.max_attempts));
  const [domain, setDomain] = useState(initial.web_otp_domain ?? '');

  const effectiveTemplate = template.trim() || initial.default_template;
  const app = appName.trim() || initial.effective_app_name;
  const preview = renderMessage(
    effectiveTemplate,
    app,
    exampleCode(Number(codeLength)),
    Number(ttl),
    domain.trim().toLowerCase(),
  );
  const seg = segmentInfo(preview);
  const codeCount = effectiveTemplate.split('{code}').length - 1;
  const unknown = [...effectiveTemplate.matchAll(/\{[^{}]*\}/g)]
    .map((m) => m[0])
    .filter((p) => !['{code}', '{app}', '{minutes}'].includes(p));
  const templateError =
    codeCount !== 1
      ? 'The message must contain {code} exactly once.'
      : unknown.length
        ? `Unknown placeholder ${unknown[0]}. Use {code}, {app} and {minutes}.`
        : '';

  async function save(e: FormEvent) {
    e.preventDefault();
    if (templateError) return;
    try {
      await update.mutateAsync({
        app_name: appName.trim() || null,
        template: template.trim() || null,
        code_length: Number(codeLength),
        ttl_seconds: Number(ttl),
        max_attempts: Number(attempts),
        web_otp_domain: domain.trim() || null,
      });
      toast.success('Verification settings saved');
    } catch (err) {
      showError(err);
    }
  }

  const options = (values: number[], current: string, label: (v: number) => string) =>
    [...new Set([...values, Number(current)])]
      .sort((a, b) => a - b)
      .map((v) => (
        <SelectItem key={v} value={String(v)}>
          {label(v)}
        </SelectItem>
      ));

  return (
    <SectionCard
      title="Message and limits"
      description={
        canAdmin ? 'Applies to every code this project sends.' : 'Only admins can change these.'
      }
    >
      <form onSubmit={save} className="flex flex-col gap-5">
        <fieldset disabled={!canAdmin} className="contents">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-app">App name</Label>
              <Input
                id="otp-app"
                value={appName}
                onChange={(e) => setAppName(e.target.value)}
                placeholder={initial.effective_app_name}
                maxLength={40}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-domain">
                Browser autofill domain{' '}
                <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="otp-domain"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="example.com"
                className="font-mono"
              />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="otp-template">Message</Label>
            <Textarea
              id="otp-template"
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder={initial.default_template}
              rows={3}
              maxLength={300}
              aria-invalid={templateError !== ''}
            />
            <p className="text-xs text-muted-foreground">
              Use <code className="font-mono">{'{code}'}</code>,{' '}
              <code className="font-mono">{'{app}'}</code> and{' '}
              <code className="font-mono">{'{minutes}'}</code>. Leave empty for the default.
            </p>
            {templateError ? <p className="text-xs text-destructive">{templateError}</p> : null}
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-length">Code</Label>
              <Select value={codeLength} onValueChange={setCodeLength}>
                <SelectTrigger id="otp-length" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {options([4, 6, 8], codeLength, (v) => `${v} digits`)}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-ttl">Valid for</Label>
              <Select value={ttl} onValueChange={setTtl}>
                <SelectTrigger id="otp-ttl" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {options(
                    [120, 300, 600, 900, 1800, 3600],
                    ttl,
                    (v) => `${Math.round(v / 60)} min`,
                  )}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="otp-attempts">Attempts</Label>
              <Select value={attempts} onValueChange={setAttempts}>
                <SelectTrigger id="otp-attempts" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>{options([3, 5, 10], attempts, (v) => String(v))}</SelectContent>
              </Select>
            </div>
          </div>
        </fieldset>

        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">Preview</span>
          <div className="max-w-sm self-start whitespace-pre-wrap rounded-2xl rounded-bl-md bg-muted px-4 py-3 text-sm/relaxed">
            {preview}
          </div>
          <p className="text-xs text-muted-foreground">
            {seg.segments} SMS segment{seg.segments > 1 ? 's' : ''} ·{' '}
            {seg.encoding === 'gsm7' ? 'GSM-7' : 'Unicode'}. Passing an Android app hash adds one
            line of 12 characters.
          </p>
        </div>

        {canAdmin ? (
          <Button
            type="submit"
            className="self-start"
            disabled={update.isPending || templateError !== ''}
          >
            {update.isPending ? 'Saving…' : 'Save settings'}
          </Button>
        ) : null}
      </form>
    </SectionCard>
  );
}

function Recent({ projectId, environment }: { projectId: string; environment: VerifyEnvironment }) {
  const [status, setStatus] = useState<VerificationFilters['status'] | 'all'>('all');
  const list = useVerifications(projectId, {
    environment,
    status: status === 'all' ? undefined : status,
  });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <SectionCard
      title="Recent verifications"
      description="Kept for 30 days. Codes are never stored in a readable form."
      action={
        <Select value={status} onValueChange={(v) => setStatus(v as typeof status)}>
          <SelectTrigger size="sm" className="w-40" aria-label="Filter by status">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {(Object.keys(STATUS) as Verification['status'][]).map((s) => (
              <SelectItem key={s} value={s}>
                {STATUS[s].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
      contentClassName="p-0"
    >
      {list.isPending ? (
        <div className="p-5">
          <Skeleton className="h-32 w-full" />
        </div>
      ) : rows.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={PasswordValidationIcon} strokeWidth={1.8} />}
          title="No verifications yet"
          description={
            environment === 'test'
              ? 'Send a code with Try it above, or call POST /v1/otp with a test key.'
              : 'Codes sent with a live key appear here.'
          }
        />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Number</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Attempts</TableHead>
                <TableHead>SMS</TableHead>
                <TableHead className="text-right">Sent</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((v) => (
                <TableRow key={v.id}>
                  <TableCell>
                    <div className="font-mono text-xs">{v.to}</div>
                    <div className="font-mono text-[0.7rem] text-muted-foreground">{v.id}</div>
                  </TableCell>
                  <TableCell>
                    <VerificationStatus status={v.status} />
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {v.attempts}
                    {v.status === 'pending' ? ` · ${v.attempts_remaining} left` : ''}
                  </TableCell>
                  <TableCell>
                    {v.message_status ? <MessageStatus status={v.message_status} /> : '—'}
                  </TableCell>
                  <TableCell
                    className="text-right text-xs text-muted-foreground"
                    title={formatDateTime(v.created_at)}
                  >
                    {formatRelative(v.created_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {list.hasNextPage ? (
            <div className="border-t p-3 text-center">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </SectionCard>
  );
}

function Integrate() {
  const { apiUrl } = useConsole();
  const code = {
    curl: [
      `# 1. Send a code`,
      `curl ${apiUrl}/v1/otp \\`,
      `  -H "Authorization: Bearer $BRIDGE_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '{"to": "+919876543210"}'`,
      ``,
      `# 2. Check what the user typed`,
      `curl ${apiUrl}/v1/otp/verify \\`,
      `  -H "Authorization: Bearer $BRIDGE_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '{"to": "+919876543210", "code": "482913"}'`,
    ].join('\n'),
    ts: [
      `import { Bridge } from '@kroszborg/bridge';`,
      ``,
      `const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: '${apiUrl}' });`,
      ``,
      `// When the user asks for a code`,
      `await bridge.otp.send({ to: phone });`,
      ``,
      `// When they type it in`,
      `const { valid } = await bridge.otp.verify({ to: phone, code });`,
      `if (!valid) throw new Error('Wrong or expired code');`,
    ].join('\n'),
    cli: [`bridgectl otp send +919876543210`, `bridgectl otp verify +919876543210 482913`].join(
      '\n',
    ),
  };
  return (
    <SectionCard
      title="Add it to your app"
      description="Test keys return the code in the response, so automated tests need no phone."
    >
      <Tabs defaultValue="ts" className="min-w-0">
        <TabsList>
          <TabsTrigger value="ts">TypeScript</TabsTrigger>
          <TabsTrigger value="curl">curl</TabsTrigger>
          <TabsTrigger value="cli">CLI</TabsTrigger>
        </TabsList>
        <TabsContent value="ts">
          <CodeBlock language="typescript" code={code.ts} />
        </TabsContent>
        <TabsContent value="curl">
          <CodeBlock language="shell" code={code.curl} />
        </TabsContent>
        <TabsContent value="cli">
          <CodeBlock language="shell" code={code.cli} />
        </TabsContent>
      </Tabs>
    </SectionCard>
  );
}
