'use client';

import type { Message } from '@bridge/api-types';
import { Alert02Icon, SentIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { type FormEvent, useMemo, useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { useConsole, useProjectId } from '@/components/layout/console-context';
import { MessageStatus, MessageTimeline } from '@/components/messages';
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { BridgeApiError } from '@/lib/api';
import { showError } from '@/lib/errors';
import { useDevices, useMessage, usePlaygroundSend } from '@/lib/queries';
import { segmentInfo } from '@/lib/sms';
import { cn } from '@/lib/utils';

const TEST_NUMBERS = [
  { to: '+15550000001', label: 'Delivered' },
  { to: '+15550000002', label: 'Invalid number' },
  { to: '+15550000003', label: 'No delivery report' },
  { to: '+15550000004', label: 'Send timeout' },
  { to: '+15550000005', label: 'Undelivered' },
];

type Result = { message: Message; status: number; replayed: boolean } | { error: BridgeApiError };

function shellQuote(s: string): string {
  return `'${s.replaceAll("'", "'\\''")}'`;
}

export default function PlaygroundPage() {
  const projectId = useProjectId() ?? '';
  const { apiUrl } = useConsole();
  const devices = useDevices(projectId);
  const send = usePlaygroundSend(projectId);
  const [environment, setEnvironment] = useState<'test' | 'live'>('test');
  const [to, setTo] = useState('+15550000001');
  const [text, setText] = useState('Your verification code is 482913.');
  const [deviceId, setDeviceId] = useState('auto');
  const [sim, setSim] = useState('default');
  const [metadata, setMetadata] = useState('');
  const [idem, setIdem] = useState('');
  const [result, setResult] = useState<Result | null>(null);
  const sentId = result && 'message' in result ? result.message.id : null;
  const live = useMessage(projectId, sentId);

  const seg = segmentInfo(text);
  let metaObj: Record<string, unknown> | undefined;
  let metaError = '';
  if (metadata.trim()) {
    try {
      const parsed = JSON.parse(metadata);
      if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed))
        metaError = 'Metadata must be a JSON object.';
      else metaObj = parsed;
    } catch {
      metaError = 'Not valid JSON.';
    }
  }

  const body = useMemo(() => {
    const b: Record<string, unknown> = { to, message: text };
    if (environment === 'live' && deviceId !== 'auto') b.device_id = deviceId;
    if (sim !== 'default') b.sim_slot = Number(sim);
    if (metaObj) b.metadata = metaObj;
    return b;
  }, [to, text, deviceId, sim, metaObj, environment]);

  const keyVar = environment === 'test' ? 'bk_test_…' : 'bk_live_…';
  const code = {
    curl: [
      `curl ${apiUrl}/v1/messages \\`,
      `  -H "Authorization: Bearer $BRIDGE_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      ...(idem ? [`  -H "Idempotency-Key: ${idem}" \\`] : []),
      `  -d ${shellQuote(JSON.stringify(body))}`,
    ].join('\n'),
    ts: [
      `import { Bridge } from '@kroszborg/bridge';`,
      ``,
      `const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: '${apiUrl}' });`,
      ``,
      `const message = await bridge.messages.send(${JSON.stringify(body, null, 2).replaceAll('\n', '\n')}${idem ? `, {\n  idempotencyKey: '${idem}',\n}` : ''});`,
      `const final = await bridge.messages.waitFor(message.id);`,
    ].join('\n'),
    cli: [
      `bridgectl send ${to} ${shellQuote(text)}${body.device_id ? ` --device ${body.device_id}` : ''}${body.sim_slot ? ` --sim ${body.sim_slot}` : ''}${idem ? ` --idempotency-key ${shellQuote(idem)}` : ''} --wait`,
    ].join('\n'),
  };

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (metaError) return;
    try {
      const r = await send.mutateAsync({
        environment,
        idempotencyKey: idem.trim() || undefined,
        body: body as { to: string; message: string },
      });
      setResult(r);
    } catch (err) {
      if (err instanceof BridgeApiError) setResult({ error: err });
      else showError(err);
    }
  }

  const shown = live.data ?? (result && 'message' in result ? result.message : null);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Playground"
        subtitle="Send a message from the dashboard and watch it move. Test mode is simulated and free; live mode sends a real SMS from your phones."
      />

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,26rem)_minmax(0,1fr)]">
        <SectionCard title="Request" contentClassName="p-0">
          <form onSubmit={submit} className="flex flex-col gap-5 p-5">
            <fieldset className="grid grid-cols-2 gap-2">
              <legend className="mb-2 text-sm font-medium">Environment</legend>
              {(
                [
                  ['test', 'Test', 'Simulated. Nothing is sent.'],
                  ['live', 'Live', 'A real SMS through your phones.'],
                ] as const
              ).map(([v, label, hint]) => (
                <label
                  key={v}
                  className="flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5"
                >
                  <input
                    type="radio"
                    name="environment"
                    checked={environment === v}
                    onChange={() => {
                      setEnvironment(v);
                      if (v === 'live' && to.startsWith('+1555000000')) setTo('');
                      if (v === 'test' && !to) setTo('+15550000001');
                    }}
                    className="mt-0.5 accent-primary"
                  />
                  <span className="flex flex-col">
                    <span className="text-sm font-medium">{label}</span>
                    <span className="text-xs text-muted-foreground">{hint}</span>
                  </span>
                </label>
              ))}
            </fieldset>

            <div className="flex flex-col gap-2">
              <Label htmlFor="pg-to">To</Label>
              <Input
                id="pg-to"
                value={to}
                onChange={(e) => setTo(e.target.value)}
                placeholder="+919876543210"
                className="font-mono"
                required
              />
              {environment === 'test' ? (
                <fieldset className="flex flex-wrap gap-1.5">
                  <legend className="sr-only">Test numbers</legend>
                  {TEST_NUMBERS.map((n) => (
                    <button
                      key={n.to}
                      type="button"
                      onClick={() => setTo(n.to)}
                      aria-pressed={to === n.to}
                      className={cn(
                        'rounded-full border px-2.5 py-0.5 text-[0.7rem] font-medium transition-colors',
                        to === n.to
                          ? 'border-primary bg-primary/10 text-foreground'
                          : 'text-muted-foreground hover:bg-muted',
                      )}
                    >
                      {n.label}
                    </button>
                  ))}
                </fieldset>
              ) : null}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="pg-text">Message</Label>
              <Textarea
                id="pg-text"
                value={text}
                onChange={(e) => setText(e.target.value)}
                maxLength={1600}
                rows={4}
                required
              />
              <p className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground">
                <span>
                  {seg.encoding === 'gsm7' ? 'GSM-7' : 'Unicode (UCS-2)'} · {seg.units}/
                  {seg.perSegment * seg.segments} {seg.encoding === 'gsm7' ? 'characters' : 'units'}
                </span>
                <span className={cn(seg.segments > 1 && 'font-medium text-foreground')}>
                  {seg.segments} segment{seg.segments > 1 ? 's' : ''}
                </span>
              </p>
              {seg.encoding === 'ucs2' && text.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                  {seg.unicodeChars.map((c) => `“${c}”`).join(' ')} switch the message to Unicode,
                  which fits 70 characters per SMS instead of 160.
                </p>
              ) : null}
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="pg-device">Phone</Label>
                <Select
                  value={deviceId}
                  onValueChange={setDeviceId}
                  disabled={environment === 'test'}
                >
                  <SelectTrigger id="pg-device" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">Bridge picks</SelectItem>
                    {(devices.data ?? [])
                      .filter((d) => d.status !== 'disabled')
                      .map((d) => (
                        <SelectItem key={d.id} value={d.id}>
                          {d.name} {d.status === 'online' ? '' : '(offline)'}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="pg-sim">SIM</Label>
                <Select value={sim} onValueChange={setSim}>
                  <SelectTrigger id="pg-sim" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="default">Phone&apos;s setting</SelectItem>
                    <SelectItem value="1">SIM 1</SelectItem>
                    <SelectItem value="2">SIM 2</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="pg-meta">
                Metadata <span className="font-normal text-muted-foreground">(optional JSON)</span>
              </Label>
              <Textarea
                id="pg-meta"
                value={metadata}
                onChange={(e) => setMetadata(e.target.value)}
                placeholder='{"order_id": "ORD-2291"}'
                className="font-mono"
                rows={2}
                aria-invalid={metaError !== ''}
              />
              {metaError ? <p className="text-xs text-destructive">{metaError}</p> : null}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="pg-idem">
                Idempotency key{' '}
                <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <div className="flex gap-2">
                <Input
                  id="pg-idem"
                  value={idem}
                  onChange={(e) => setIdem(e.target.value)}
                  placeholder="order-2291-shipped"
                  className="font-mono"
                  maxLength={255}
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setIdem(`pg-${crypto.randomUUID().slice(0, 8)}`)}
                >
                  Generate
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                Send twice with the same key: the second request returns the first message.
              </p>
            </div>

            {environment === 'live' ? (
              <p className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/8 p-3 text-xs/relaxed">
                <HugeiconsIcon
                  icon={Alert02Icon}
                  strokeWidth={2}
                  className="mt-0.5 size-3.5 shrink-0 text-warning"
                />
                This sends a real SMS from one of your phones. Carrier charges apply.
              </p>
            ) : null}

            <Button
              type="submit"
              size="lg"
              disabled={send.isPending || !to.trim() || !text || metaError !== ''}
            >
              <HugeiconsIcon icon={SentIcon} strokeWidth={2} />
              {send.isPending
                ? 'Sending…'
                : environment === 'test'
                  ? 'Send test message'
                  : 'Send live SMS'}
            </Button>
          </form>
        </SectionCard>

        <div className="flex min-w-0 flex-col gap-6">
          <SectionCard
            title="Same request in your code"
            description={`Use a ${keyVar} key from API keys.`}
          >
            <Tabs defaultValue="curl" className="min-w-0">
              <TabsList>
                <TabsTrigger value="curl">curl</TabsTrigger>
                <TabsTrigger value="ts">TypeScript</TabsTrigger>
                <TabsTrigger value="cli">CLI</TabsTrigger>
              </TabsList>
              <TabsContent value="curl">
                <CodeBlock language="shell" code={code.curl} />
              </TabsContent>
              <TabsContent value="ts">
                <CodeBlock language="typescript" code={code.ts} />
              </TabsContent>
              <TabsContent value="cli">
                <CodeBlock language="shell" code={code.cli} />
              </TabsContent>
            </Tabs>
          </SectionCard>

          <SectionCard
            title="Response"
            action={
              shown ? (
                <Link
                  href={`/projects/${projectId}/messages`}
                  className="text-xs font-medium text-primary hover:underline"
                >
                  Open Messages
                </Link>
              ) : undefined
            }
          >
            {!result ? (
              <p className="text-sm text-muted-foreground">
                Send a message to see the API response and its live timeline here.
              </p>
            ) : 'error' in result ? (
              <div className="flex flex-col gap-3">
                <p className="font-mono text-xs">
                  <span className="font-semibold text-destructive">{result.error.status}</span>{' '}
                  {result.error.code}
                </p>
                {result.error.code === 'opted_out' ? (
                  <div className="rounded-lg border border-warning/40 bg-warning/8 p-3 text-xs/relaxed">
                    <p className="font-semibold text-foreground">This number opted out</p>
                    <p className="mt-1 text-muted-foreground">
                      The person asked not to receive messages from this project, for example by
                      replying STOP. Bridge refuses ordinary messages, broadcasts and schedules to
                      it; one-time passwords from Verify still go. If they asked to hear from you
                      again, remove the number on the{' '}
                      <Link
                        href={`/projects/${projectId}/automation?tab=opt-outs`}
                        className="font-medium text-primary hover:underline"
                      >
                        opt-out list
                      </Link>
                      .
                    </p>
                  </div>
                ) : null}
                <CodeBlock
                  language="json"
                  code={JSON.stringify(
                    {
                      error: {
                        code: result.error.code,
                        message: result.error.message,
                        details: result.error.details,
                        request_id: result.error.requestId,
                      },
                    },
                    null,
                    2,
                  )}
                />
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                <div className="flex flex-wrap items-center gap-3 text-xs">
                  <span className="font-mono">
                    <span className="font-semibold text-success">{result.status}</span>{' '}
                    {result.status === 200 ? 'OK (replayed)' : 'Accepted'}
                  </span>
                  {shown ? <MessageStatus status={shown.status} /> : null}
                  <span className="font-mono text-muted-foreground">{result.message.id}</span>
                </div>
                {live.data ? <MessageTimeline events={live.data.events} /> : null}
                {shown?.status === 'failed' ? (
                  <div className="rounded-lg border border-destructive/30 bg-destructive/8 p-3 text-xs/relaxed">
                    <p className="font-mono font-semibold text-destructive">{shown.error_code}</p>
                    <p className="mt-1 text-muted-foreground">{shown.error_message}</p>
                  </div>
                ) : null}
                <CodeBlock language="json" code={JSON.stringify(result.message, null, 2)} />
              </div>
            )}
          </SectionCard>
        </div>
      </div>
    </div>
  );
}
