'use client';

import type { WebhookEndpoint, WebhookEventType } from '@bridge/api-types';
import { type FormEvent, useEffect, useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { StatusBadge } from '@/components/kit/status-badge';
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatRelative } from '@/lib/format';
import type { WebhookInput } from '@/lib/queries';
import { cn } from '@/lib/utils';

export const EVENT_TYPES: { type: WebhookEventType; description: string }[] = [
  { type: 'message.sent', description: 'Android reported every segment sent.' },
  { type: 'message.delivered', description: 'The carrier confirmed delivery.' },
  { type: 'message.failed', description: 'Sending or delivery failed for good.' },
  { type: 'message.received', description: 'A phone with forwarding on received an SMS.' },
  { type: 'device.online', description: 'A phone connected.' },
  { type: 'device.offline', description: 'A phone has been offline for 2 minutes.' },
];

/** Health of an endpoint as a status badge. */
export function WebhookHealth({ endpoint: e }: { endpoint: WebhookEndpoint }) {
  if (!e.enabled) {
    return <StatusBadge kind={e.disabled_reason ? 'danger' : 'neutral'}>Disabled</StatusBadge>;
  }
  if (e.failing_since) {
    return (
      <StatusBadge kind="warning" title={e.failing_since}>
        Failing since {formatRelative(e.failing_since)}
      </StatusBadge>
    );
  }
  if (e.last_success_at) return <StatusBadge kind="success">Healthy</StatusBadge>;
  return <StatusBadge kind="neutral">No deliveries yet</StatusBadge>;
}

export function EventSummary({ events }: { events: WebhookEndpoint['events'] }) {
  if (events.length === 0) return <span className="text-muted-foreground">All events</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {events.map((e) => (
        <span
          key={e}
          className="rounded-md border bg-background px-1.5 py-0.5 font-mono text-[0.68rem] text-muted-foreground"
        >
          {e}
        </span>
      ))}
    </span>
  );
}

/** Choose every event, or a subset. An empty list means every event, as in the API. */
function EventPicker({
  value,
  onChange,
}: {
  value: WebhookEventType[];
  onChange: (v: WebhookEventType[]) => void;
}) {
  const [mode, setMode] = useState<'all' | 'some'>(value.length ? 'some' : 'all');
  useEffect(() => {
    if (value.length) setMode('some');
  }, [value.length]);
  const toggle = (t: WebhookEventType) =>
    onChange(value.includes(t) ? value.filter((x) => x !== t) : [...value, t]);
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-2 text-sm font-medium">Events</legend>
      <div className="grid grid-cols-2 gap-2">
        {(
          [
            ['all', 'All events', 'Including types added later.'],
            ['some', 'Selected events', 'Only the ones you tick.'],
          ] as const
        ).map(([m, label, hint]) => (
          <label
            key={m}
            className="flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5"
          >
            <input
              type="radio"
              name="event-mode"
              checked={mode === m}
              onChange={() => {
                setMode(m);
                if (m === 'all') onChange([]);
              }}
              className="mt-0.5 accent-primary"
            />
            <span className="flex flex-col">
              <span className="text-sm font-medium">{label}</span>
              <span className="text-xs text-muted-foreground">{hint}</span>
            </span>
          </label>
        ))}
      </div>
      {mode === 'some' ? (
        <div className="flex flex-col divide-y rounded-lg border">
          {EVENT_TYPES.map((e) => (
            <label
              key={e.type}
              className="flex cursor-pointer items-start gap-3 px-3 py-2.5 hover:bg-muted/40"
            >
              <input
                type="checkbox"
                checked={value.includes(e.type)}
                onChange={() => toggle(e.type)}
                className="mt-0.5 size-4 shrink-0 accent-primary"
              />
              <span className="flex min-w-0 flex-col">
                <span className="font-mono text-xs font-medium">{e.type}</span>
                <span className="text-xs text-muted-foreground">{e.description}</span>
              </span>
            </label>
          ))}
        </div>
      ) : null}
    </fieldset>
  );
}

/** Create or edit an endpoint. */
export function WebhookFormDialog({
  open,
  onOpenChange,
  initial,
  title,
  submitLabel,
  pending,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initial?: WebhookEndpoint;
  title: string;
  submitLabel: string;
  pending: boolean;
  onSubmit: (v: Required<Pick<WebhookInput, 'url' | 'description' | 'events'>>) => Promise<void>;
}) {
  const [url, setUrl] = useState('');
  const [description, setDescription] = useState('');
  const [events, setEvents] = useState<WebhookEventType[]>([]);
  useEffect(() => {
    if (open) {
      setUrl(initial?.url ?? '');
      setDescription(initial?.description ?? '');
      setEvents(initial?.events ?? []);
    }
  }, [open, initial]);
  const insecure = url.startsWith('http://');

  async function submit(e: FormEvent) {
    e.preventDefault();
    await onSubmit({ url: url.trim(), description: description.trim(), events });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>
              Bridge POSTs JSON to this URL and expects a 2xx response within 15 seconds.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="webhook-url">Endpoint URL</Label>
            <Input
              id="webhook-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/webhooks/bridge"
              className="font-mono"
              maxLength={2048}
              required
              autoFocus
            />
            {insecure ? (
              <p className="text-xs text-warning">
                Plain http:// sends events, including message text, unencrypted. Use https:// unless
                the endpoint is on your own network.
              </p>
            ) : null}
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="webhook-description">
              Description <span className="font-normal text-muted-foreground">(optional)</span>
            </Label>
            <Input
              id="webhook-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Order notifications service"
              maxLength={200}
            />
          </div>
          <EventPicker value={events} onChange={setEvents} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !url.trim()}>
              {pending ? 'Saving…' : submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const nodeSample = `import { Webhook } from "standardwebhooks"; // npm i standardwebhooks

const wh = new Webhook(process.env.BRIDGE_WEBHOOK_SECRET);

app.post("/webhooks/bridge", express.raw({ type: "application/json" }), (req, res) => {
  let event;
  try {
    // Checks webhook-id, webhook-timestamp and webhook-signature.
    event = wh.verify(req.body, req.headers);
  } catch {
    return res.status(400).end();
  }
  // Deliveries can repeat: de-duplicate on the webhook-id header.
  if (event.type === "message.delivered") markDelivered(event.data.id);
  res.status(204).end();
});`;

const pythonSample = `from standardwebhooks.webhooks import Webhook  # pip install standardwebhooks

wh = Webhook(os.environ["BRIDGE_WEBHOOK_SECRET"])

@app.post("/webhooks/bridge")
def bridge_webhook():
    try:
        event = wh.verify(request.get_data(), dict(request.headers))
    except Exception:
        abort(400)
    if event["type"] == "message.received":
        handle_sms(event["data"]["from"], event["data"]["body"])
    return "", 204`;

const goSample = `// import standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
wh, _ := standardwebhooks.NewWebhook(os.Getenv("BRIDGE_WEBHOOK_SECRET"))

http.HandleFunc("/webhooks/bridge", func(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := wh.Verify(body, r.Header); err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
})`;

const manualSample = `signed  = webhook-id + "." + webhook-timestamp + "." + raw body
key     = base64_decode(secret without "whsec_")
expected = "v1," + base64(hmac_sha256(key, signed))

Accept if expected equals one of the space-separated values in webhook-signature
(compare in constant time) and webhook-timestamp is within 5 minutes of now.`;

/** How to verify Bridge's signatures, with official Standard Webhooks libraries. */
export function VerifyInstructions({ className }: { className?: string }) {
  return (
    <Tabs defaultValue="node" className={cn('min-w-0', className)}>
      <TabsList>
        <TabsTrigger value="node">Node.js</TabsTrigger>
        <TabsTrigger value="python">Python</TabsTrigger>
        <TabsTrigger value="go">Go</TabsTrigger>
        <TabsTrigger value="manual">By hand</TabsTrigger>
      </TabsList>
      <TabsContent value="node">
        <CodeBlock language="javascript" code={nodeSample} />
      </TabsContent>
      <TabsContent value="python">
        <CodeBlock language="python" code={pythonSample} />
      </TabsContent>
      <TabsContent value="go">
        <CodeBlock language="go" code={goSample} />
      </TabsContent>
      <TabsContent value="manual">
        <CodeBlock language="algorithm" code={manualSample} />
      </TabsContent>
    </Tabs>
  );
}
