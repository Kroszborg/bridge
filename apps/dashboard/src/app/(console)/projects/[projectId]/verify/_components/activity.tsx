'use client';

import type { Verification, VerifyApp, VerifyBlockReason, VerifyResult } from '@bridge/api-types';
import { PasswordValidationIcon, SecurityCheckIcon, SentIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { EmptyState } from '@/components/kit/empty-state';
import { SectionCard } from '@/components/kit/section-card';
import { StatTile } from '@/components/kit/stat-tile';
import { useCan, useConsole } from '@/components/layout/console-context';
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
import { BridgeApiError } from '@/lib/api';
import { countryName } from '@/lib/countries';
import { showError } from '@/lib/errors';
import { formatDateTime, formatRelative } from '@/lib/format';
import {
  useOtpPlayground,
  useVerifications,
  useVerifyAppBlocks,
  useVerifyAppStats,
  type VerificationFilters,
  type VerifyEnvironment,
} from '@/lib/queries';
import { cn } from '@/lib/utils';
import { BLOCK_REASONS, duration, number, pct, STATUS, VerificationStatus } from './shared';

type Props = { projectId: string; app: VerifyApp; environment: VerifyEnvironment };

export function Stats({ projectId, app, environment }: Props) {
  const stats = useVerifyAppStats(projectId, app.id, environment);
  if (stats.isPending) {
    return (
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 2xl:grid-cols-6">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
    );
  }
  const s = stats.data;
  if (!s) return null;
  const reasons = (Object.keys(BLOCK_REASONS) as VerifyBlockReason[])
    .filter((r) => s.blocked[r] > 0)
    .sort((a, b) => s.blocked[b] - s.blocked[a]);
  const top = reasons[0];
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-3 2xl:grid-cols-6">
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
      <StatTile
        label="Failovers"
        value={number.format(s.failovers)}
        note="Resent through another route"
      />
      <StatTile
        label="Blocked attempts"
        value={number.format(s.blocked.total)}
        note={
          top
            ? `Most: ${BLOCK_REASONS[top].label.toLowerCase()} (${s.blocked[top]})`
            : 'None refused'
        }
      />
    </div>
  );
}

export function TryIt({ projectId, app, environment }: Props) {
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
      const v = await send.mutateAsync({ environment, to, app: app.id });
      setSent(v);
      setResult(null);
      setCode(v.code ?? '');
    } catch (err) {
      if (
        err instanceof BridgeApiError &&
        (err.code === 'rate_limited' || err.code === 'otp_blocked')
      ) {
        toast.error(err.message);
      } else showError(err);
    }
  }

  async function onVerify(e: FormEvent) {
    e.preventDefault();
    if (!sent) return;
    try {
      setResult(await verify.mutateAsync({ environment, id: sent.id, code, app: app.id }));
    } catch (err) {
      showError(err);
    }
  }

  return (
    <SectionCard
      title="Try it"
      description={
        environment === 'test'
          ? `Simulated with ${app.name}'s settings: nothing is sent, and the code is shown.`
          : `Sends a real SMS with ${app.name}'s settings. Enter the code you receive.`
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
          ) : (
            <p className="text-xs text-muted-foreground">
              The app&apos;s fraud protection applies, except the per-IP limit.
            </p>
          )}
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

export function Blocks({ projectId, app, environment }: Props) {
  const [reason, setReason] = useState<VerifyBlockReason | 'all'>('all');
  const list = useVerifyAppBlocks(projectId, app.id, {
    environment,
    reason: reason === 'all' ? undefined : reason,
  });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <SectionCard
      title="Blocked attempts"
      description="Codes fraud protection refused to send. Kept for 30 days."
      action={
        <Select value={reason} onValueChange={(v) => setReason(v as typeof reason)}>
          <SelectTrigger size="sm" className="w-52" aria-label="Filter by reason">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All reasons</SelectItem>
            {(Object.keys(BLOCK_REASONS) as VerifyBlockReason[]).map((r) => (
              <SelectItem key={r} value={r}>
                {BLOCK_REASONS[r].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
      contentClassName="p-0"
    >
      {list.isPending ? (
        <div className="p-5">
          <Skeleton className="h-24 w-full" />
        </div>
      ) : rows.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={SecurityCheckIcon} strokeWidth={1.8} />}
          title={reason === 'all' ? 'Nothing blocked' : 'Nothing blocked for this reason'}
          description={`No ${environment} send attempts to ${app.name} were refused in the last 30 days.`}
        />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Number</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Country</TableHead>
                <TableHead>IP address</TableHead>
                <TableHead className="text-right">When</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((b) => (
                <TableRow key={b.id}>
                  <TableCell className="font-mono text-xs">{b.to}</TableCell>
                  <TableCell className="text-xs" title={BLOCK_REASONS[b.reason].hint}>
                    {BLOCK_REASONS[b.reason].label}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {b.country ? countryName(b.country) : '—'}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {b.client_ip ?? '—'}
                  </TableCell>
                  <TableCell
                    className="text-right text-xs text-muted-foreground"
                    title={formatDateTime(b.created_at)}
                  >
                    {formatRelative(b.created_at)}
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

export function Recent({ projectId, app, environment }: Props) {
  const [status, setStatus] = useState<VerificationFilters['status'] | 'all'>('all');
  const list = useVerifications(projectId, {
    environment,
    app: app.id,
    status: status === 'all' ? undefined : status,
  });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];

  return (
    <SectionCard
      title="Recent verifications"
      description={`${app.name}'s codes, kept for 30 days. Codes are never stored in a readable form.`}
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
                    <div className="flex flex-col gap-0.5">
                      {v.message_status ? <MessageStatus status={v.message_status} /> : '—'}
                      {v.failover_message_status ? (
                        <span className="inline-flex items-center gap-1.5 text-[0.7rem] text-muted-foreground">
                          Failover
                          <MessageStatus status={v.failover_message_status} />
                        </span>
                      ) : null}
                    </div>
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

export function Integrate({ app }: { app: VerifyApp }) {
  const { apiUrl } = useConsole();
  const slug = app.slug;
  const appJson = app.is_default ? '' : `, "app": "${slug}"`;
  const appTs = app.is_default ? '' : `, app: '${slug}'`;
  const code = {
    curl: [
      `# 1. Send a code${app.is_default ? '' : ` with the ${app.name} app`}`,
      `curl ${apiUrl}/v1/otp \\`,
      `  -H "Authorization: Bearer $BRIDGE_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '{"to": "+919876543210"${appJson}, "client_ip": "203.0.113.7"}'`,
      ``,
      `# 2. Check what the user typed`,
      `curl ${apiUrl}/v1/otp/verify \\`,
      `  -H "Authorization: Bearer $BRIDGE_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '{"to": "+919876543210", "code": "482913"${appJson}}'`,
    ].join('\n'),
    ts: [
      `import { Bridge } from '@kroszborg/bridge';`,
      ``,
      `const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: '${apiUrl}' });`,
      ``,
      `// When the user asks for a code. client_ip enables the per-IP limit.`,
      `await bridge.otp.send({ to: phone${appTs}, client_ip: req.ip });`,
      ``,
      `// When they type it in`,
      `const { valid } = await bridge.otp.verify({ to: phone, code${appTs} });`,
      `if (!valid) throw new Error('Wrong or expired code');`,
    ].join('\n'),
    cli: [
      `bridgectl otp send +919876543210${app.is_default ? '' : ` --app ${slug}`}`,
      `bridgectl otp verify +919876543210 482913${app.is_default ? '' : ` --app ${slug}`}`,
    ].join('\n'),
  };
  return (
    <SectionCard
      title="Add it to your app"
      description={
        app.is_default
          ? 'Requests that name no app use this one. Test keys return the code, so automated tests need no phone.'
          : `Pass app: '${slug}' (or its ID) to use this app. Test keys return the code, so automated tests need no phone.`
      }
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
