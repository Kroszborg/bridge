'use client';

import type { Device, PairingToken } from '@bridge/api-types';
import {
  Alert02Icon,
  BatteryCharging01Icon,
  BatteryFullIcon,
  BatteryLowIcon,
  Delete02Icon,
  MoreHorizontalIcon,
  Notification03Icon,
  PencilEdit02Icon,
  PlusSignIcon,
  QrCodeIcon,
  SentIcon,
  Settings02Icon,
  SmartPhone01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyField } from '@/components/kit/copy-button';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { StatusBadge } from '@/components/kit/status-badge';
import { useCan, useProjectId } from '@/components/layout/console-context';
import { PairingQR } from '@/components/pairing-qr';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
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
import { showError } from '@/lib/errors';
import { formatDate, formatRelative } from '@/lib/format';
import {
  useCreatePairingToken,
  useDevices,
  useRemoveDevice,
  useRenameDevice,
  useTestSend,
  useUpdateDevice,
  useWakeDevice,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

type Presence = 'online' | 'stale' | 'offline' | 'disabled';

/** The server sweeps vanished connections every couple of minutes; flag them sooner here. */
function presenceOf(d: Device, now: number): Presence {
  if (d.status === 'disabled') return 'disabled';
  if (d.status === 'offline') return 'offline';
  const lastSeen = d.last_seen_at ? new Date(d.last_seen_at).getTime() : 0;
  const allowance = (d.heartbeat_interval_seconds * 2 + 30) * 1000;
  return now - lastSeen > allowance ? 'stale' : 'online';
}

const NETWORK: Record<string, string> = {
  wifi: 'Wi-Fi',
  cellular: 'Mobile data',
  ethernet: 'Ethernet',
  none: 'No network',
};

function useNow(intervalMs = 15_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-faint">
        {label}
      </dt>
      <dd className="truncate text-xs font-medium tabular-nums">{children}</dd>
    </div>
  );
}

function Battery({ d }: { d: Device }) {
  if (d.battery_level == null) return <>—</>;
  const icon = d.is_charging
    ? BatteryCharging01Icon
    : d.battery_level <= 20
      ? BatteryLowIcon
      : BatteryFullIcon;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1',
        d.battery_level <= 15 && !d.is_charging && 'text-warning',
      )}
    >
      <HugeiconsIcon icon={icon} strokeWidth={2} className="size-3.5" />
      {d.battery_level}%{d.is_charging ? ' · charging' : ''}
    </span>
  );
}

function StatusFor({ presence }: { presence: Presence }) {
  switch (presence) {
    case 'online':
      return (
        <StatusBadge kind="success" live>
          Online
        </StatusBadge>
      );
    case 'stale':
      return <StatusBadge kind="warning">Not responding</StatusBadge>;
    case 'offline':
      return <StatusBadge kind="danger">Offline</StatusBadge>;
    default:
      return <StatusBadge kind="neutral">Removed</StatusBadge>;
  }
}

function OfflineNotice({
  d,
  presence,
  onWake,
  waking,
  onTroubleshoot,
}: {
  d: Device;
  presence: Presence;
  onWake: () => void;
  waking: boolean;
  onTroubleshoot: () => void;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-warning/30 bg-warning/8 p-3 text-xs/relaxed">
      <p className="flex items-center gap-1.5 font-semibold text-warning">
        <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} className="size-3.5" />
        {presence === 'stale' ? 'Device stopped checking in' : 'Device offline'}
        <span className="font-normal text-muted-foreground">
          · last seen {formatRelative(d.last_seen_at)}
        </span>
      </p>
      <ul className="list-disc pl-4 text-muted-foreground">
        <li>The phone has no network connection</li>
        <li>Android battery optimisation stopped the Bridge app</li>
        <li>The phone is switched off or the app was force-stopped</li>
      </ul>
      <div className="flex flex-wrap gap-2 pt-1">
        <Button size="sm" variant="outline" onClick={onWake} disabled={waking}>
          <HugeiconsIcon icon={Notification03Icon} strokeWidth={2} />
          {waking ? 'Waking…' : 'Wake device'}
        </Button>
        <Button size="sm" variant="ghost" onClick={onTroubleshoot}>
          Troubleshooting
        </Button>
      </div>
    </div>
  );
}

function simLabel(d: Device): string {
  if (!d.preferred_sim_slot) return 'Phone default';
  const sim = d.sims.find((x) => x.slot === d.preferred_sim_slot);
  return `SIM ${d.preferred_sim_slot}${sim?.carrier ? ` · ${sim.carrier}` : ''}`;
}

/** How much of Android's per-app SMS allowance this phone has used. */
function SendWindow({ d }: { d: Device }) {
  const used = Math.min(1, d.recent_sends / d.send_limit_count);
  const minutes = Math.round(d.send_limit_window_seconds / 60);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-1 text-xs">
        <span className="text-muted-foreground">
          Sent in the last {minutes} min:{' '}
          <span className="font-medium tabular-nums text-foreground">
            {d.recent_sends} of {d.send_limit_count}
          </span>
        </span>
        <span className="tabular-nums text-faint">
          {d.total_sent} sent · {d.total_failed} failed · paired {formatDate(d.created_at)}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
        <div
          className={cn('h-full rounded-full', used >= 1 ? 'bg-warning' : 'bg-primary')}
          style={{ width: `${Math.max(used * 100, d.recent_sends > 0 ? 3 : 0)}%` }}
        />
      </div>
      {used >= 1 ? (
        <p className="text-xs text-warning">
          At its limit. New messages wait for capacity or go to another phone.
        </p>
      ) : null}
    </div>
  );
}

function DeviceCard({
  d,
  now,
  onRename,
  onRemove,
  onTroubleshoot,
  onTestSend,
  onSettings,
}: {
  d: Device;
  now: number;
  onRename: () => void;
  onRemove: () => void;
  onTroubleshoot: () => void;
  onTestSend: () => void;
  onSettings: () => void;
}) {
  const canAdmin = useCan('admin');
  const projectId = useProjectId() ?? '';
  const wake = useWakeDevice(projectId);
  const presence = presenceOf(d, now);

  async function doWake() {
    try {
      const res = await wake.mutateAsync(d.id);
      if (res.via === 'none') toast.warning(res.message);
      else toast.success(res.message);
    } catch (err) {
      showError(err);
    }
  }

  return (
    <article
      data-slot="section-card"
      className={cn(
        'flex min-w-0 flex-col gap-4 rounded-xl border bg-card p-5',
        presence === 'disabled' && 'opacity-60',
      )}
    >
      <header className="flex items-start gap-3">
        <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
          <HugeiconsIcon icon={SmartPhone01Icon} strokeWidth={1.8} className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="truncate font-display text-sm font-semibold">{d.name}</h3>
          <p className="truncate font-mono text-[0.68rem] text-muted-foreground">{d.id}</p>
        </div>
        <div className="flex items-center gap-1">
          <StatusFor presence={presence} />
          {presence !== 'disabled' && canAdmin ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${d.name}`}>
                  <HugeiconsIcon icon={MoreHorizontalIcon} strokeWidth={2} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuItem onSelect={() => void doWake()}>
                  <HugeiconsIcon icon={Notification03Icon} strokeWidth={2} className="size-4" />
                  Wake device
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={onTestSend}>
                  <HugeiconsIcon icon={SentIcon} strokeWidth={2} className="size-4" />
                  Send test SMS
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={onSettings}>
                  <HugeiconsIcon icon={Settings02Icon} strokeWidth={2} className="size-4" />
                  Settings
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={onRename}>
                  <HugeiconsIcon icon={PencilEdit02Icon} strokeWidth={2} className="size-4" />
                  Rename
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={onRemove}>
                  <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} className="size-4" />
                  Remove
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
      </header>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
        <Fact label="Battery">
          <Battery d={d} />
        </Fact>
        <Fact label="Network">
          {d.network_type ? (NETWORK[d.network_type] ?? d.network_type) : '—'}
        </Fact>
        <Fact label="Carrier">{d.carrier_name || '—'}</Fact>
        <Fact label="Last heartbeat">
          <span title={d.last_heartbeat_at ?? undefined}>
            {formatRelative(d.last_heartbeat_at)}
          </span>
        </Fact>
        <Fact label="Phone">
          <span title={d.device_model ?? undefined}>
            {[d.device_model, d.android_version && `Android ${d.android_version}`]
              .filter(Boolean)
              .join(' · ') || '—'}
          </span>
        </Fact>
        <Fact label="Gateway">
          {d.app_version ? `${d.app_version}${d.app_flavor ? ` · ${d.app_flavor}` : ''}` : '—'}
        </Fact>
        <Fact label="Wake-up">
          {d.push_provider === 'fcm'
            ? 'Firebase'
            : d.push_provider === 'unifiedpush'
              ? 'UnifiedPush'
              : 'None'}
        </Fact>
        <Fact label="Sending SIM">{simLabel(d)}</Fact>
        <Fact label="Incoming SMS">
          {d.forward_inbound ? <span className="text-primary">Forwarded</span> : 'Not forwarded'}
        </Fact>
      </dl>

      <SendWindow d={d} />

      {presence === 'offline' || presence === 'stale' ? (
        <OfflineNotice
          d={d}
          presence={presence}
          onWake={() => void doWake()}
          waking={wake.isPending}
          onTroubleshoot={onTroubleshoot}
        />
      ) : null}
      {presence === 'disabled' ? (
        <p className="text-xs text-muted-foreground">
          Removed {formatRelative(d.revoked_at)}. Pair the phone again to bring it back.
        </p>
      ) : null}
    </article>
  );
}

function useCountdown(expiresAt: string | undefined) {
  const [left, setLeft] = useState(0);
  useEffect(() => {
    if (!expiresAt) return;
    const tick = () =>
      setLeft(Math.max(0, Math.round((new Date(expiresAt).getTime() - Date.now()) / 1000)));
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [expiresAt]);
  return left;
}

function PairDialog({
  open,
  onOpenChange,
  devices,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  devices: Device[];
}) {
  const projectId = useProjectId() ?? '';
  const create = useCreatePairingToken(projectId);
  const [token, setToken] = useState<PairingToken | null>(null);
  const [manual, setManual] = useState(false);
  const openedAt = useRef(0);
  const left = useCountdown(token?.expires_at);
  const expired = token !== null && left === 0;

  async function newCode() {
    try {
      setToken(await create.mutateAsync());
    } catch (err) {
      showError(err);
      onOpenChange(false);
    }
  }

  // biome-ignore lint/correctness/useExhaustiveDependencies: mint a fresh code each time the dialog opens
  useEffect(() => {
    if (!open) {
      setToken(null);
      setManual(false);
      return;
    }
    openedAt.current = Date.now();
    void newCode();
  }, [open]);

  // Close automatically once the phone pairs and connects.
  useEffect(() => {
    if (!open || !token) return;
    const paired = devices.find(
      (d) => d.connected_at && new Date(d.connected_at).getTime() > openedAt.current - 2000,
    );
    if (paired) {
      toast.success(`${paired.name} is paired and online`);
      onOpenChange(false);
    }
  }, [devices, open, token, onOpenChange]);

  const mm = String(Math.floor(left / 60)).padStart(1, '0');
  const ss = String(left % 60).padStart(2, '0');

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Pair an Android phone</DialogTitle>
          <DialogDescription>
            The phone sends SMS through its own SIM. Carrier limits on that SIM still apply.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-6 sm:grid-cols-[1fr_auto]">
          <ol className="flex flex-col gap-3 text-xs/relaxed text-muted-foreground">
            <li>
              <span className="font-medium text-foreground">1. Install the Bridge gateway app</span>
              <br />
              Download the APK from this project&apos;s GitHub Releases. Use the{' '}
              <span className="font-mono">foss</span> build unless you configured Firebase.
            </li>
            <li>
              <span className="font-medium text-foreground">
                2. Open it and tap Scan pairing code
              </span>
              <br />
              Point the camera at the QR code. The code works once and expires in 10 minutes.
            </li>
            <li>
              <span className="font-medium text-foreground">
                3. Allow the requested permissions
              </span>
              <br />
              Notifications and an exemption from battery optimisation keep the gateway online.
            </li>
          </ol>
          <div className="flex flex-col items-center gap-2">
            <div
              className={cn(
                'grid size-[13rem] place-items-center rounded-xl bg-white p-3',
                (expired || !token) && 'opacity-30',
              )}
            >
              {token ? (
                <PairingQR value={token.pairing_uri} />
              ) : (
                <HugeiconsIcon
                  icon={QrCodeIcon}
                  strokeWidth={1.5}
                  className="size-12 text-black/40"
                />
              )}
            </div>
            {expired ? (
              <Button size="sm" onClick={() => void newCode()} disabled={create.isPending}>
                New code
              </Button>
            ) : (
              <p className="text-xs tabular-nums text-muted-foreground">
                {token ? `Expires in ${mm}:${ss}` : 'Creating code…'}
              </p>
            )}
          </div>
        </div>
        <div className="border-t pt-4">
          <button
            type="button"
            className="text-xs font-medium text-primary underline-offset-4 hover:underline"
            onClick={() => setManual((v) => !v)}
            aria-expanded={manual}
          >
            {manual ? 'Hide manual entry' : 'Cannot scan? Enter the details by hand'}
          </button>
          {manual && token ? (
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <div className="flex min-w-0 flex-col gap-1.5">
                <Label className="text-xs">Server URL</Label>
                <CopyField value={token.api_url} />
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <Label className="text-xs">Pairing code</Label>
                <CopyField value={token.token} />
              </div>
            </div>
          ) : null}
        </div>
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="signal-live size-1.5 rounded-full bg-primary" aria-hidden />
          Waiting for the phone to connect…
        </p>
      </DialogContent>
    </Dialog>
  );
}

function TroubleshootDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Keep the gateway online</DialogTitle>
          <DialogDescription>
            Android limits apps in the background. Work through these on the phone.
          </DialogDescription>
        </DialogHeader>
        <ol className="flex list-decimal flex-col gap-3 pl-4 text-xs/relaxed text-muted-foreground">
          <li>
            <span className="font-medium text-foreground">Open the Bridge app.</span> Its status
            screen shows what is missing and links straight to each setting.
          </li>
          <li>
            <span className="font-medium text-foreground">
              Exempt Bridge from battery optimisation.
            </span>{' '}
            Settings → Apps → Bridge → Battery → Unrestricted.
          </li>
          <li>
            <span className="font-medium text-foreground">Check vendor battery savers.</span>{' '}
            Xiaomi, Samsung, OnePlus, Oppo, Vivo and Huawei add their own app killers. Allow Bridge
            to autostart and to run in the background; dontkillmyapp.com has steps for each brand.
          </li>
          <li>
            <span className="font-medium text-foreground">
              Keep the phone charging and on a stable network.
            </span>{' '}
            Wi-Fi is the most reliable. Bridge checks in more often while charging.
          </li>
          <li>
            <span className="font-medium text-foreground">Set up wake-ups.</span> The foss build
            uses UnifiedPush: install a distributor such as ntfy. The Google build uses Firebase
            when the server has it configured.
          </li>
        </ol>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TestSendDialog({ device, onClose }: { device: Device | null; onClose: () => void }) {
  const projectId = useProjectId() ?? '';
  const send = useTestSend(projectId);
  const [to, setTo] = useState('');
  const [message, setMessage] = useState('Test message from Bridge.');
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!device) return;
    try {
      await send.mutateAsync({ deviceId: device.id, to, message });
      toast.success(`Queued on ${device.name}. Follow it under Messages.`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={device !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Send a test SMS</DialogTitle>
            <DialogDescription>
              Sends a real message through {device?.name}&apos;s SIM. Normal carrier charges apply.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="test-to">To</Label>
            <Input
              id="test-to"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder="+919876543210"
              className="font-mono"
              inputMode="tel"
              required
              autoFocus
            />
            <p className="text-xs text-muted-foreground">
              International format with the country code.
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="test-message">Message</Label>
            <Input
              id="test-message"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              maxLength={160}
              required
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={send.isPending || !to.trim()}>
              {send.isPending ? 'Sending…' : 'Send'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function SettingsDialog({ device, onClose }: { device: Device | null; onClose: () => void }) {
  const projectId = useProjectId() ?? '';
  const update = useUpdateDevice(projectId);
  const [sim, setSim] = useState('0');
  const [limit, setLimit] = useState('30');
  const [forward, setForward] = useState(false);
  useEffect(() => {
    if (device) {
      setSim(String(device.preferred_sim_slot ?? 0));
      setLimit(String(device.send_limit_count));
      setForward(device.forward_inbound);
    }
  }, [device]);
  const slots: { slot: number; carrier?: string }[] = device?.sims.length
    ? device.sims
    : [{ slot: 1 }, { slot: 2 }];

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!device) return;
    try {
      await update.mutateAsync({
        deviceId: device.id,
        preferred_sim_slot: Number(sim),
        send_limit_count: Number(limit),
        forward_inbound: forward,
      });
      toast.success('Device settings saved');
      onClose();
    } catch (err) {
      showError(err);
    }
  }

  return (
    <Dialog open={device !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-5">
          <DialogHeader>
            <DialogTitle>Settings · {device?.name}</DialogTitle>
            <DialogDescription>
              How this phone sends, and whether it forwards the SMS it receives.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="sim-slot">SIM</Label>
            <Select value={sim} onValueChange={setSim}>
              <SelectTrigger id="sim-slot" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">Phone&apos;s default SMS SIM</SelectItem>
                {slots.map((x) => (
                  <SelectItem key={x.slot} value={String(x.slot)}>
                    SIM {x.slot}
                    {x.carrier ? ` · ${x.carrier}` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {device && device.sims.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                The phone has not reported its SIMs. Allow Phone access in the Bridge app to list
                them.
              </p>
            ) : null}
            <p className="text-xs text-muted-foreground">
              An API request can still choose a SIM with <code className="font-mono">sim_slot</code>
              .
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="send-limit">Messages per 30 minutes</Label>
            <Input
              id="send-limit"
              type="number"
              min={1}
              max={10000}
              value={limit}
              onChange={(e) => setLimit(e.target.value)}
              className="w-32 tabular-nums"
              required
            />
            <p className="text-xs/relaxed text-muted-foreground">
              Android asks the phone&apos;s owner to approve every SMS beyond about 30 per 30
              minutes from one app, and messages stall until someone taps Allow. Bridge paces the
              phone under this number. Raise it only after lifting Android&apos;s limit over USB:
            </p>
            <CodeBlock
              language="adb"
              code={
                'adb shell settings put global sms_outgoing_check_max_count 1000\nadb shell settings put global sms_outgoing_check_interval_ms 1800000'
              }
            />
            <p className="text-xs text-muted-foreground">
              Your carrier may still limit or block bulk sending.
            </p>
          </div>
          <div className="flex flex-col gap-2 border-t pt-4">
            <label
              htmlFor="forward-inbound"
              className="flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors has-checked:border-primary/50 has-checked:bg-primary/5"
            >
              <input
                id="forward-inbound"
                type="checkbox"
                checked={forward}
                onChange={(e) => setForward(e.target.checked)}
                className="mt-0.5 size-4 shrink-0 accent-primary"
              />
              <span className="flex flex-col gap-1">
                <span className="text-sm font-medium">Forward incoming SMS</span>
                <span className="text-xs/relaxed text-muted-foreground">
                  Every SMS this phone receives is stored in Bridge and sent to your{' '}
                  <code className="font-mono">message.received</code> webhooks, including one-time
                  codes and personal messages. Use a SIM dedicated to Bridge. The app asks for
                  permission to receive SMS.
                </span>
              </span>
            </label>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({ device, onClose }: { device: Device | null; onClose: () => void }) {
  const projectId = useProjectId() ?? '';
  const rename = useRenameDevice(projectId);
  const [name, setName] = useState('');
  useEffect(() => {
    if (device) setName(device.name);
  }, [device]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!device) return;
    try {
      await rename.mutateAsync({ deviceId: device.id, name });
      toast.success('Device renamed');
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={device !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Rename device</DialogTitle>
            <DialogDescription>
              A name that tells you which phone this is, such as its location or SIM.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="device-name">Name</Label>
            <Input
              id="device-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={80}
              required
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={rename.isPending || !name.trim()}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RemoveDialog({ device, onClose }: { device: Device | null; onClose: () => void }) {
  const projectId = useProjectId() ?? '';
  const remove = useRemoveDevice(projectId);
  async function confirm() {
    if (!device) return;
    try {
      await remove.mutateAsync(device.id);
      toast.success(`Removed ${device.name}`);
      onClose();
    } catch (err) {
      showError(err);
    }
  }
  return (
    <Dialog open={device !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove {device?.name}?</DialogTitle>
          <DialogDescription>
            Bridge revokes the phone&apos;s credential and disconnects it now. It stops receiving
            messages. You can pair it again later.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? 'Removing…' : 'Remove device'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function DevicesPage() {
  const projectId = useProjectId() ?? '';
  const [pairOpen, setPairOpen] = useState(false);
  const devices = useDevices(projectId, pairOpen ? 2_000 : 10_000);
  const now = useNow();
  const [renaming, setRenaming] = useState<Device | null>(null);
  const [testing, setTesting] = useState<Device | null>(null);
  const [configuring, setConfiguring] = useState<Device | null>(null);
  const [removing, setRemoving] = useState<Device | null>(null);
  const [troubleshoot, setTroubleshoot] = useState(false);

  const { active, removed, onlineCount } = useMemo(() => {
    const list = devices.data ?? [];
    const active = list.filter((d) => d.status !== 'disabled');
    return {
      active,
      removed: list.filter((d) => d.status === 'disabled'),
      onlineCount: active.filter((d) => presenceOf(d, now) === 'online').length,
    };
  }, [devices.data, now]);

  const canAdmin = useCan('admin');
  const pairButton = !canAdmin ? undefined : (
    <Button size="lg" onClick={() => setPairOpen(true)}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} />
      Pair device
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Devices"
        subtitle={
          devices.data
            ? `${onlineCount} of ${active.length} online · Android phones that send SMS through their SIM`
            : 'Android phones that send SMS through their SIM'
        }
        actions={active.length > 0 ? pairButton : undefined}
      />

      {devices.isPending ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-56 rounded-xl" />
          ))}
        </div>
      ) : devices.isError ? (
        <div className="rounded-xl border bg-card">
          <EmptyState
            title="Could not load devices"
            description={devices.error.message}
            action={
              <Button variant="outline" onClick={() => devices.refetch()}>
                Try again
              </Button>
            }
          />
        </div>
      ) : active.length === 0 ? (
        <div className="rounded-xl border bg-card">
          <EmptyState
            icon={<HugeiconsIcon icon={SmartPhone01Icon} strokeWidth={1.8} />}
            title="No phones paired yet"
            description="Pair an Android phone with a SIM card. Bridge sends your messages through it and reports every status change."
            action={pairButton}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          {active.map((d) => (
            <DeviceCard
              key={d.id}
              d={d}
              now={now}
              onRename={() => setRenaming(d)}
              onTestSend={() => setTesting(d)}
              onSettings={() => setConfiguring(d)}
              onRemove={() => setRemoving(d)}
              onTroubleshoot={() => setTroubleshoot(true)}
            />
          ))}
        </div>
      )}

      {removed.length > 0 ? (
        <details className="group">
          <summary className="cursor-pointer text-xs font-medium text-muted-foreground hover:text-foreground">
            Removed devices ({removed.length})
          </summary>
          <div className="mt-3 grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
            {removed.map((d) => (
              <DeviceCard
                key={d.id}
                d={d}
                now={now}
                onRename={() => undefined}
                onTestSend={() => undefined}
                onSettings={() => undefined}
                onRemove={() => undefined}
                onTroubleshoot={() => setTroubleshoot(true)}
              />
            ))}
          </div>
        </details>
      ) : null}

      <PairDialog open={pairOpen} onOpenChange={setPairOpen} devices={devices.data ?? []} />
      <TroubleshootDialog open={troubleshoot} onOpenChange={setTroubleshoot} />
      <RenameDialog device={renaming} onClose={() => setRenaming(null)} />
      <TestSendDialog device={testing} onClose={() => setTesting(null)} />
      <SettingsDialog device={configuring} onClose={() => setConfiguring(null)} />
      <RemoveDialog device={removing} onClose={() => setRemoving(null)} />
    </div>
  );
}
