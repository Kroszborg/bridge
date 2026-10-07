import {
  ArrowRight01Icon,
  BatteryFullIcon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  CpuIcon,
  Database01Icon,
  DocumentValidationIcon,
  Mail01Icon,
  PasswordValidationIcon,
  SecurityCheckIcon,
  SignalFull01Icon,
  SmartPhone01Icon,
  TestTube01Icon,
  Wifi01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Image from 'next/image';
import { Wordmark } from '@/components/brand';
import { CodeTabs, CopyButton } from '@/components/code';
import { PairingQR } from '@/components/pairing-qr';
import { RevealObserver } from '@/components/reveal';
import { ThemeToggle } from '@/components/theme';
import { DASHBOARD_URL, REPO_URL, STATUS_URL } from '@/lib/site';

const sendCurl = `curl https://sms.example.com/v1/messages \\
  -H "Authorization: Bearer $BRIDGE_API_KEY" \\
  -H "Idempotency-Key: order-2291-shipped" \\
  -d '{
    "to": "+919876543210",
    "message": "Order ORD-2291 has shipped."
  }'`;

const otpSample = `// When the user asks for a code
await bridge.otp.send({ to: '+919876543210' });

// When they type it in
const { valid } = await bridge.otp.verify({ to: '+919876543210', code });`;

const otpFacts = [
  {
    icon: SecurityCheckIcon,
    title: 'Never stored readable',
    body: 'Only a keyed hash, erased once the code is used.',
  },
  {
    icon: Clock01Icon,
    title: 'Limits built in',
    body: 'Attempts, expiry, resend cooldown and an hourly cap per number.',
  },
  {
    icon: PasswordValidationIcon,
    title: 'Autofill',
    body: 'SMS Retriever on Android, WebOTP in browsers, one-tap on iOS.',
  },
  {
    icon: TestTube01Icon,
    title: 'Free in tests',
    body: 'Test keys return the code, so CI never needs a phone.',
  },
];

const snippets = [
  { id: 'curl', label: 'curl', language: 'shell', code: sendCurl },
  {
    id: 'ts',
    label: 'TypeScript',
    language: '@bridge/sdk',
    code: `import { Bridge } from '@bridge/sdk';

const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY });

const msg = await bridge.messages.send(
  { to: '+919876543210', message: 'Your code is 482913.' },
  { idempotencyKey: 'login-7731' },
);

const final = await bridge.messages.waitFor(msg.id); // delivered, or failed with a reason`,
  },
  {
    id: 'webhook',
    label: 'Webhooks',
    language: 'Node.js',
    code: `import { verifyWebhook } from '@bridge/sdk';

app.post('/webhooks/bridge', express.raw({ type: 'application/json' }), async (req, res) => {
  const event = await verifyWebhook({
    payload: req.body,
    headers: req.headers,
    secret: process.env.BRIDGE_WEBHOOK_SECRET,
  });
  if (event.type === 'message.received') await handleReply(event.data.from, event.data.body);
  res.sendStatus(204);
});`,
  },
  {
    id: 'cli',
    label: 'CLI',
    language: 'bridgectl',
    code: `$ bridgectl send +15550000001 "Hello from bridgectl" --wait
msg_06gh9q17gdw7rgqmgftswexty8  delivered  → +15550000001
  11:05:34  created
  11:05:34  queued
  11:05:34  device_accepted
  11:05:35  sent
  11:05:37  delivered

$ bridgectl listen --forward-to http://localhost:3000/webhooks/bridge`,
  },
];

const selfHost = `git clone ${REPO_URL}.git
cd bridge
docker compose up -d
# dashboard  http://localhost:3000
# API        http://localhost:8080`;

const timeline = [
  { time: '14:01:02.114', label: 'Created', detail: 'POST /v1/messages' },
  { time: '14:01:02.131', label: 'Queued', detail: 'waiting for a phone' },
  { time: '14:01:03.402', label: 'Phone accepted', detail: 'Pixel 8 · SIM 1' },
  { time: '14:01:05.880', label: 'Sent', detail: '1 segment, GSM-7' },
  { time: '14:01:07.215', label: 'Delivered', detail: 'carrier delivery report' },
];

const testNumbers = [
  ['+15550000001', 'Delivered'],
  ['+15550000002', 'Invalid number'],
  ['+15550000003', 'No delivery report'],
  ['+15550000005', 'Undelivered'],
];

const roadmap = [
  {
    v: '0.1',
    title: 'Android gateway',
    body: 'Pair a phone, send through its SIM, delivery reports.',
    state: 'Shipped',
  },
  {
    v: '0.2',
    title: 'Webhooks and incoming SMS',
    body: 'Signed events, replies, TypeScript SDK.',
    state: 'Shipped',
  },
  {
    v: '0.3',
    title: 'Developer platform',
    body: 'Playground, CLI, usage, request logs, status.',
    state: 'Shipping now',
  },
  {
    v: '0.4',
    title: 'Verify API',
    body: 'Send and check codes in one call each.',
    state: 'In progress',
  },
  {
    v: '0.5',
    title: 'Providers',
    body: 'Route to MSG91 or Twilio when a phone is not enough.',
    state: 'Later',
  },
];

function Screenshot({
  name,
  alt,
  priority = false,
}: {
  name: string;
  alt: string;
  priority?: boolean;
}) {
  return (
    <>
      <Image
        src={`/screens/${name}-dark.webp`}
        alt={alt}
        width={2160}
        height={1350}
        priority={priority}
        className="hidden h-auto w-full dark:block"
      />
      <Image
        src={`/screens/${name}-light.webp`}
        alt={alt}
        width={2160}
        height={1350}
        priority={priority}
        className="block h-auto w-full dark:hidden"
      />
    </>
  );
}

function PrimaryCta({ className = '' }: { className?: string }) {
  return (
    <a
      href="#self-host"
      className={`inline-flex h-11 items-center gap-2 rounded-xl bg-primary px-5 text-sm font-semibold text-primary-foreground transition-transform hover:brightness-110 active:translate-y-px ${className}`}
    >
      Self-host it
      <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} className="size-4" />
    </a>
  );
}

function SecondaryCta() {
  return (
    <a
      href={DASHBOARD_URL}
      className="inline-flex h-11 items-center rounded-xl border bg-card px-5 text-sm font-semibold transition-colors hover:bg-muted active:translate-y-px"
    >
      Open the dashboard
    </a>
  );
}

export default function Home() {
  return (
    <>
      <RevealObserver />
      <header className="sticky top-0 z-40 border-b border-transparent bg-background/80 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
        <nav className="mx-auto flex h-16 max-w-7xl items-center justify-between gap-6 px-4 sm:px-6">
          <a href="/" aria-label="Bridge home">
            <Wordmark />
          </a>
          <div className="hidden items-center gap-7 text-sm text-muted-foreground md:flex">
            <a href="#how" className="transition-colors hover:text-foreground">
              How it works
            </a>
            <a href="#verify" className="transition-colors hover:text-foreground">
              Verify
            </a>
            <a href="#developers" className="transition-colors hover:text-foreground">
              Developers
            </a>
            <a href="#self-host" className="transition-colors hover:text-foreground">
              Self-host
            </a>
            <a href={STATUS_URL} className="transition-colors hover:text-foreground">
              Status
            </a>
            <a href={REPO_URL} className="transition-colors hover:text-foreground">
              Source
            </a>
          </div>
          <div className="flex items-center gap-2">
            <ThemeToggle />
            <a
              href={DASHBOARD_URL}
              className="hidden h-9 items-center rounded-lg border bg-card px-3.5 text-sm font-semibold transition-colors hover:bg-muted sm:inline-flex"
            >
              Open the dashboard
            </a>
          </div>
        </nav>
      </header>

      <main>
        {/* Hero: copy left, the real console right, bleeding off the edge on large screens. */}
        <section className="relative overflow-hidden">
          <div
            aria-hidden
            className="pointer-events-none absolute -top-40 right-0 h-[36rem] w-[60rem] rounded-full blur-3xl"
            style={{ background: 'radial-gradient(closest-side, var(--glow), transparent)' }}
          />
          <div className="relative mx-auto grid max-w-7xl items-center gap-12 px-4 pt-14 pb-20 sm:px-6 lg:min-h-[calc(100dvh-4rem)] lg:grid-cols-[minmax(0,7fr)_minmax(0,6fr)] lg:pt-10 lg:pb-16">
            <div className="flex flex-col gap-6">
              <span
                className="hero-in inline-flex w-fit items-center gap-2 rounded-full border bg-card px-3 py-1 text-xs font-medium text-muted-foreground"
                style={{ '--i': 0 } as React.CSSProperties}
              >
                <span className="signal size-1.5 rounded-full bg-primary" aria-hidden />
                Open source, self-hosted
              </span>
              <h1
                className="hero-in text-4xl font-bold leading-[1.05] sm:text-5xl lg:text-[3.25rem] xl:text-[3.5rem]"
                style={{ '--i': 1 } as React.CSSProperties}
              >
                Send SMS through phones you already own.
              </h1>
              <p
                className="hero-in max-w-[34rem] text-lg leading-relaxed text-muted-foreground"
                style={{ '--i': 2 } as React.CSSProperties}
              >
                Pair an Android phone, call one API, and follow every message to the carrier&apos;s
                delivery report.
              </p>
              <div
                className="hero-in flex flex-wrap gap-3"
                style={{ '--i': 3 } as React.CSSProperties}
              >
                <PrimaryCta />
                <SecondaryCta />
              </div>
            </div>
            <div className="hero-in lg:-mr-[22vw]" style={{ '--i': 2 } as React.CSSProperties}>
              <div className="overflow-hidden rounded-2xl border bg-card p-1.5 shadow-[0_40px_120px_-40px_var(--glow)]">
                <div className="overflow-hidden rounded-xl">
                  <Screenshot
                    name="messages"
                    priority
                    alt="The Bridge console listing delivered messages and forwarded incoming SMS for a project"
                  />
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* How it works: one request, three stations. */}
        <section id="how" className="scroll-mt-20 border-t">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-28">
            <div data-reveal className="max-w-2xl">
              <h2 className="text-3xl font-bold sm:text-4xl">
                One request in. A delivery report out.
              </h2>
              <p className="mt-4 text-lg leading-relaxed text-muted-foreground">
                Your app talks to Bridge. Bridge picks a phone that can send right now and tells you
                what the carrier said.
              </p>
            </div>
            <ol className="mt-14 grid gap-6 lg:grid-cols-[minmax(0,6fr)_minmax(0,4fr)_minmax(0,4fr)]">
              <li
                data-reveal
                style={{ '--i': 0 } as React.CSSProperties}
                className="flex min-w-0 flex-col gap-4"
              >
                <div className="flex items-center gap-3">
                  <span className="grid size-8 place-items-center rounded-full border font-mono text-xs">
                    1
                  </span>
                  <h3 className="text-lg font-semibold">Your app sends</h3>
                </div>
                <div className="overflow-hidden rounded-2xl border bg-card">
                  <div className="flex items-center justify-between border-b px-4 py-2">
                    <span className="font-mono text-[0.7rem] text-faint">shell</span>
                    <CopyButton value={sendCurl} label="Copy command" />
                  </div>
                  <pre className="overflow-x-auto p-4 font-mono text-[0.75rem] leading-relaxed">
                    <code>{sendCurl}</code>
                  </pre>
                </div>
                <p className="text-sm text-muted-foreground">
                  An idempotency key means a retried request never sends twice.
                </p>
              </li>
              <li
                data-reveal
                style={{ '--i': 1 } as React.CSSProperties}
                className="flex min-w-0 flex-col gap-4"
              >
                <div className="flex items-center gap-3">
                  <span className="grid size-8 place-items-center rounded-full border font-mono text-xs">
                    2
                  </span>
                  <h3 className="text-lg font-semibold">Bridge picks a phone</h3>
                </div>
                <ul className="flex flex-col gap-3 rounded-2xl border bg-card p-5 text-sm">
                  {[
                    'Online and under its send limit',
                    'Charging and on Wi-Fi, preferred',
                    'Retries on another phone if one fails',
                    'Wakes sleeping phones with a push',
                  ].map((t) => (
                    <li key={t} className="flex gap-2.5">
                      <HugeiconsIcon
                        icon={CheckmarkCircle02Icon}
                        strokeWidth={2}
                        className="mt-0.5 size-4 shrink-0 text-primary"
                      />
                      {t}
                    </li>
                  ))}
                </ul>
              </li>
              <li
                data-reveal
                style={{ '--i': 2 } as React.CSSProperties}
                className="flex min-w-0 flex-col gap-4"
              >
                <div className="flex items-center gap-3">
                  <span className="grid size-8 place-items-center rounded-full border font-mono text-xs">
                    3
                  </span>
                  <h3 className="text-lg font-semibold">The phone reports back</h3>
                </div>
                <div className="flex flex-col gap-4 rounded-2xl border bg-card p-5">
                  <div className="flex items-center gap-3">
                    <span className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground">
                      <HugeiconsIcon icon={SmartPhone01Icon} strokeWidth={2} className="size-5" />
                    </span>
                    <div>
                      <div className="text-sm font-semibold">Pixel 8</div>
                      <div className="text-xs text-success">Online</div>
                    </div>
                  </div>
                  <dl className="grid grid-cols-3 gap-3 text-xs text-muted-foreground">
                    <div className="flex items-center gap-1.5">
                      <HugeiconsIcon icon={BatteryFullIcon} strokeWidth={2} className="size-3.5" />
                      86%
                    </div>
                    <div className="flex items-center gap-1.5">
                      <HugeiconsIcon icon={Wifi01Icon} strokeWidth={2} className="size-3.5" />
                      Wi-Fi
                    </div>
                    <div className="flex items-center gap-1.5">
                      <HugeiconsIcon icon={SignalFull01Icon} strokeWidth={2} className="size-3.5" />
                      SIM 1
                    </div>
                  </dl>
                  <p className="text-sm text-muted-foreground">
                    Sent, then delivered, as the carrier reports it. Never guessed.
                  </p>
                </div>
              </li>
            </ol>
          </div>
        </section>

        {/* What you get: a bento with real artifacts. */}
        <section className="border-t bg-card/40">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-28">
            <div data-reveal className="max-w-2xl">
              <span className="text-xs font-semibold uppercase tracking-[0.16em] text-primary">
                Built for developers
              </span>
              <h2 className="mt-3 text-3xl font-bold sm:text-4xl">
                Everything after the API call is handled.
              </h2>
            </div>
            <div className="mt-12 grid gap-4 lg:grid-cols-6">
              <article
                data-reveal
                className="relative overflow-hidden rounded-2xl border bg-card p-6 lg:col-span-4 lg:row-span-2"
              >
                <div
                  aria-hidden
                  className="pointer-events-none absolute -right-24 -bottom-24 size-96 rounded-full blur-3xl"
                  style={{ background: 'radial-gradient(closest-side, var(--glow), transparent)' }}
                />
                <div className="relative grid h-full gap-8 lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)] lg:items-center">
                  <div className="flex flex-col gap-4">
                    <h3 className="text-xl font-semibold">Every status, with evidence</h3>
                    <p className="text-sm leading-relaxed text-muted-foreground">
                      Each message keeps a timeline: which phone took it, which SIM sent it, and
                      what the carrier reported.
                    </p>
                    <ul className="flex flex-col gap-2 text-sm">
                      {[
                        'Fetch it with GET /v1/messages/{id}',
                        'Or get it pushed as webhooks',
                        'Bodies redacted after 30 days',
                      ].map((t) => (
                        <li key={t} className="flex gap-2">
                          <HugeiconsIcon
                            icon={CheckmarkCircle02Icon}
                            strokeWidth={2}
                            className="mt-0.5 size-4 shrink-0 text-primary"
                          />
                          {t}
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div className="relative rounded-xl border bg-background/70 p-5 backdrop-blur">
                    <div className="mb-4 flex items-center justify-between">
                      <span className="font-mono text-xs text-muted-foreground">
                        msg_06gh9q17gd…
                      </span>
                      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-success">
                        <HugeiconsIcon
                          icon={CheckmarkCircle02Icon}
                          strokeWidth={2}
                          className="size-3.5"
                        />
                        Delivered
                      </span>
                    </div>
                    <ol>
                      {timeline.map((s, i) => (
                        <li
                          key={s.label}
                          className="relative grid grid-cols-[6.5rem_1rem_1fr] gap-3 pb-3 last:pb-0"
                        >
                          <time className="pt-px font-mono text-[0.7rem] text-faint">{s.time}</time>
                          <span className="relative flex justify-center pt-1">
                            <span className="z-10 size-2.5 rounded-full bg-primary" />
                            {i < timeline.length - 1 ? (
                              <span
                                aria-hidden
                                className="absolute top-3.5 -bottom-3 w-px bg-border"
                              />
                            ) : null}
                          </span>
                          <span>
                            <span className="block text-sm font-medium">{s.label}</span>
                            <span className="block font-mono text-xs text-muted-foreground">
                              {s.detail}
                            </span>
                          </span>
                        </li>
                      ))}
                    </ol>
                  </div>
                </div>
              </article>
              <article
                data-reveal
                style={{ '--i': 1 } as React.CSSProperties}
                className="rounded-2xl border bg-card p-6 lg:col-span-2 [background-image:radial-gradient(var(--border)_1px,transparent_1px)] [background-size:14px_14px]"
              >
                <HugeiconsIcon
                  icon={SecurityCheckIcon}
                  strokeWidth={2}
                  className="size-5 text-primary"
                />
                <h3 className="mt-4 text-lg font-semibold">Signed webhooks</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Deliveries, failures and replies arrive as Standard Webhooks, retried for about
                  three days.
                </p>
                <pre className="mt-4 overflow-x-auto rounded-lg border bg-background p-3 font-mono text-[0.7rem] leading-relaxed text-muted-foreground">
                  <code>{'webhook-id: evt_06gh9q…\nwebhook-signature: v1,K5oZ…'}</code>
                </pre>
              </article>
              <article
                data-reveal
                style={{ '--i': 2 } as React.CSSProperties}
                className="flex items-center gap-5 rounded-2xl border bg-primary/[0.06] p-6 lg:col-span-2"
              >
                <div className="shrink-0 rounded-xl bg-white p-1.5 shadow-sm">
                  <PairingQR size={104} />
                </div>
                <div>
                  <h3 className="text-lg font-semibold">Pair in one scan</h3>
                  <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                    Scan the code with the Bridge app. The phone confirms your server before it
                    trusts it.
                  </p>
                </div>
              </article>
              <article data-reveal className="rounded-2xl border bg-card p-6 lg:col-span-3">
                <HugeiconsIcon icon={Mail01Icon} strokeWidth={2} className="size-5 text-primary" />
                <h3 className="mt-4 text-lg font-semibold">Incoming SMS, forwarded</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Turn on forwarding for a phone and its replies reach your webhooks within seconds.
                </p>
                <ul className="mt-5 flex flex-col divide-y rounded-xl border bg-background text-sm">
                  {[
                    ['+919812345678', 'Got the parcel, thanks!'],
                    ['+14155550132', 'STOP'],
                  ].map(([from, body]) => (
                    <li key={from} className="flex items-center justify-between gap-3 px-4 py-2.5">
                      <span className="font-mono text-xs">{from}</span>
                      <span className="truncate text-muted-foreground">{body}</span>
                    </li>
                  ))}
                </ul>
              </article>
              <article
                data-reveal
                style={{ '--i': 1 } as React.CSSProperties}
                className="rounded-2xl border bg-gradient-to-br from-primary/[0.08] via-card to-card p-6 lg:col-span-3"
              >
                <HugeiconsIcon
                  icon={DocumentValidationIcon}
                  strokeWidth={2}
                  className="size-5 text-primary"
                />
                <h3 className="mt-4 text-lg font-semibold">A test mode that costs nothing</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Test keys simulate the whole lifecycle, webhooks included. These numbers fail on
                  purpose:
                </p>
                <dl className="mt-5 grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                  {testNumbers.map(([n, outcome]) => (
                    <div key={n} className="flex flex-col">
                      <dt className="font-mono text-xs">{n}</dt>
                      <dd className="text-muted-foreground">{outcome}</dd>
                    </div>
                  ))}
                </dl>
              </article>
            </div>
          </div>
        </section>

        {/* Verify: the OTP API, shown as the SMS and the autofill it produces. */}
        <section id="verify" className="scroll-mt-20 border-t">
          <div className="mx-auto grid max-w-7xl items-center gap-12 px-4 py-20 sm:px-6 lg:grid-cols-[minmax(0,6fr)_minmax(0,5fr)] lg:py-28">
            <div data-reveal className="min-w-0">
              <h2 className="text-3xl font-bold sm:text-4xl">Phone verification in two calls.</h2>
              <p className="mt-4 max-w-xl text-lg leading-relaxed text-muted-foreground">
                Bridge generates the code, sends it from your phones and checks what the user types.
                Your app never stores, compares or expires a code.
              </p>
              <ul className="mt-8 grid gap-x-8 gap-y-5 sm:grid-cols-2">
                {otpFacts.map((f) => (
                  <li key={f.title} className="flex gap-3">
                    <HugeiconsIcon
                      icon={f.icon}
                      strokeWidth={2}
                      className="mt-0.5 size-5 shrink-0 text-primary"
                    />
                    <span className="flex flex-col gap-0.5">
                      <span className="font-semibold">{f.title}</span>
                      <span className="text-sm leading-relaxed text-muted-foreground">
                        {f.body}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
              <div className="mt-8 overflow-hidden rounded-xl border bg-card">
                <pre className="overflow-x-auto p-4 font-mono text-[0.8rem] leading-relaxed">
                  <code>{otpSample}</code>
                </pre>
              </div>
            </div>

            <figure data-reveal className="mx-auto w-full max-w-sm">
              <div className="rounded-[2rem] border bg-card p-5 shadow-[0_24px_60px_-30px_var(--glow)]">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span className="font-semibold text-foreground">Acme</span>
                  <span>now</span>
                </div>
                <p className="mt-2 rounded-2xl rounded-tl-md bg-muted px-4 py-3 text-sm leading-relaxed">
                  482913 is your Acme code. It expires in 10 minutes. Do not share it.
                  <span className="mt-2 block font-mono text-xs text-muted-foreground">
                    @acme.com #482913
                  </span>
                </p>
                <div className="mt-6 border-t pt-5">
                  <p className="text-sm font-semibold">Sign in to Acme</p>
                  <p className="text-xs text-muted-foreground">
                    Enter the code sent to +91 98765 43210
                  </p>
                  <div className="mt-3 grid grid-cols-6 gap-1.5">
                    {'482913'.split('').map((d, i) => (
                      <span
                        // biome-ignore lint/suspicious/noArrayIndexKey: fixed six digits
                        key={i}
                        className="grid h-11 place-items-center rounded-lg border border-primary/50 bg-background font-mono text-lg font-semibold"
                      >
                        {d}
                      </span>
                    ))}
                  </div>
                  <span className="mt-3 inline-flex items-center gap-2 rounded-full border bg-background px-3 py-1 text-xs">
                    <HugeiconsIcon
                      icon={Mail01Icon}
                      strokeWidth={2}
                      className="size-3.5 text-primary"
                    />
                    From Messages: 482913
                  </span>
                  <span className="mt-4 flex h-10 items-center justify-center rounded-lg bg-primary text-sm font-semibold text-primary-foreground">
                    Continue
                  </span>
                </div>
              </div>
              <figcaption className="mt-4 text-center text-xs text-muted-foreground">
                The domain line lets browsers and iOS offer the code above the keyboard.
              </figcaption>
            </figure>
          </div>
        </section>

        {/* Developers: one full-width tabbed sample. */}
        <section id="developers" className="scroll-mt-20 border-t">
          <div className="mx-auto max-w-5xl px-4 py-20 sm:px-6 lg:py-28">
            <div data-reveal className="max-w-2xl">
              <h2 className="text-3xl font-bold sm:text-4xl">
                Use it from anywhere you write code.
              </h2>
              <p className="mt-4 text-lg leading-relaxed text-muted-foreground">
                A REST API with an OpenAPI spec, a zero-dependency TypeScript SDK, and a CLI that
                forwards live events to localhost.
              </p>
            </div>
            <div data-reveal className="mt-10">
              <CodeTabs snippets={snippets} />
            </div>
          </div>
        </section>

        {/* The console: one large real screenshot. */}
        <section className="border-t bg-card/40">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-28">
            <div data-reveal className="mx-auto max-w-2xl text-center">
              <h2 className="text-3xl font-bold sm:text-4xl">
                A console your whole team can read.
              </h2>
              <p className="mt-4 text-lg leading-relaxed text-muted-foreground">
                Usage by day, request logs, a playground, roles and an audit log. Dark by default,
                light when you want it.
              </p>
            </div>
            <div data-reveal className="mt-12 overflow-hidden rounded-2xl border bg-card p-1.5">
              <div className="overflow-hidden rounded-xl">
                <Screenshot
                  name="usage"
                  alt="The Bridge usage page with daily charts of delivered and failed messages"
                />
              </div>
            </div>
          </div>
        </section>

        {/* Self-host: facts left, the three commands right. */}
        <section id="self-host" className="scroll-mt-20 border-t">
          <div className="mx-auto grid max-w-7xl items-center gap-12 px-4 py-20 sm:px-6 lg:grid-cols-2 lg:py-28">
            <div data-reveal>
              <span className="text-xs font-semibold uppercase tracking-[0.16em] text-primary">
                Self-host
              </span>
              <h2 className="mt-3 text-3xl font-bold sm:text-4xl">
                Your server, your phones, your data.
              </h2>
              <p className="mt-4 max-w-xl text-lg leading-relaxed text-muted-foreground">
                Bridge runs with Docker Compose on any machine that has it. Message text is removed
                after 30 days by default.
              </p>
              <dl className="mt-10 grid grid-cols-1 gap-6 sm:grid-cols-3">
                {[
                  { icon: CpuIcon, k: 'One binary', v: 'API, worker and migrations in Go' },
                  {
                    icon: Database01Icon,
                    k: 'One database',
                    v: 'PostgreSQL holds data and the job queue',
                  },
                  { icon: SecurityCheckIcon, k: 'AGPL-3.0', v: 'Client SDKs are MIT' },
                ].map((f) => (
                  <div key={f.k} className="flex flex-col gap-2">
                    <HugeiconsIcon icon={f.icon} strokeWidth={2} className="size-5 text-primary" />
                    <dt className="font-display text-lg font-semibold">{f.k}</dt>
                    <dd className="text-sm text-muted-foreground">{f.v}</dd>
                  </div>
                ))}
              </dl>
            </div>
            <div
              data-reveal
              style={{ '--i': 1 } as React.CSSProperties}
              className="overflow-hidden rounded-2xl border bg-card"
            >
              <div className="flex items-center justify-between border-b px-4 py-2.5">
                <span className="font-mono text-xs text-faint">terminal</span>
                <CopyButton value={selfHost} label="Copy commands" />
              </div>
              <pre className="overflow-x-auto p-5 font-mono text-sm leading-loose">
                <code>{selfHost}</code>
              </pre>
            </div>
          </div>
        </section>

        {/* Honest limits. */}
        <section className="border-t bg-card/40">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-24">
            <h2 data-reveal className="max-w-2xl text-3xl font-bold sm:text-4xl">
              What Bridge will not pretend.
            </h2>
            <div className="mt-12 grid gap-10 sm:grid-cols-2">
              {[
                [
                  'Not for bulk marketing',
                  'Bridge sends the messages your product needs. Carrier rules, DLT registration and consent still apply to you.',
                ],
                [
                  'A phone sends at a phone’s pace',
                  'Android asks before an app sends more than about 30 SMS in 30 minutes. Bridge paces each phone and tells you how to raise it.',
                ],
                [
                  'Delivery means the carrier said so',
                  'A message is delivered only when a delivery report arrives. Without one, it stays sent.',
                ],
                [
                  'Failures come with a reason',
                  'Invalid numbers, no signal, radio off: every failure has a stable error code you can act on.',
                ],
              ].map(([t, b], i) => (
                <div
                  key={t}
                  data-reveal
                  style={{ '--i': i } as React.CSSProperties}
                  className="border-t pt-5"
                >
                  <h3 className="text-lg font-semibold">{t}</h3>
                  <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">{b}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* Roadmap. */}
        <section className="border-t">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-24">
            <div data-reveal className="max-w-2xl">
              <span className="text-xs font-semibold uppercase tracking-[0.16em] text-primary">
                Roadmap
              </span>
              <h2 className="mt-3 text-3xl font-bold sm:text-4xl">
                From your own phones to any provider.
              </h2>
            </div>
            <ol className="mt-12 grid gap-8 md:grid-cols-5">
              {roadmap.map((r, i) => (
                <li
                  key={r.v}
                  data-reveal
                  style={{ '--i': i } as React.CSSProperties}
                  className="relative flex flex-col gap-2"
                >
                  <div className="flex items-center gap-3">
                    <span
                      className={`grid size-9 shrink-0 place-items-center rounded-full border font-mono text-xs ${
                        r.state === 'Shipped'
                          ? 'border-primary/50 bg-primary/10 text-primary'
                          : r.state === 'Shipping now'
                            ? 'border-primary bg-primary text-primary-foreground'
                            : 'text-muted-foreground'
                      }`}
                    >
                      {r.v}
                    </span>
                    {i < roadmap.length - 1 ? (
                      <span aria-hidden className="hidden h-px flex-1 bg-border md:block" />
                    ) : null}
                  </div>
                  <span className="mt-2 text-xs font-medium text-muted-foreground">{r.state}</span>
                  <h3 className="font-semibold">{r.title}</h3>
                  <p className="text-sm text-muted-foreground">{r.body}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        {/* Closing call to action. */}
        <section className="relative overflow-hidden border-t">
          <div
            aria-hidden
            className="pointer-events-none absolute inset-x-0 -bottom-48 mx-auto h-96 max-w-4xl rounded-full blur-3xl"
            style={{ background: 'radial-gradient(closest-side, var(--glow), transparent)' }}
          />
          <div
            data-reveal
            className="relative mx-auto flex max-w-3xl flex-col items-center gap-6 px-4 py-24 text-center sm:px-6"
          >
            <h2 className="text-3xl font-bold sm:text-5xl">
              Your first SMS is three commands away.
            </h2>
            <p className="max-w-xl text-lg text-muted-foreground">
              Start in test mode for free, then pair a phone when you are ready.
            </p>
            <div className="flex flex-wrap justify-center gap-3">
              <PrimaryCta />
              <SecondaryCta />
            </div>
          </div>
        </section>
      </main>

      <footer className="border-t">
        <div className="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-10 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <Wordmark />
          <nav className="flex flex-wrap gap-x-6 gap-y-2">
            <a href={DASHBOARD_URL} className="hover:text-foreground">
              Dashboard
            </a>
            <a href={STATUS_URL} className="hover:text-foreground">
              Status
            </a>
            <a href={REPO_URL} className="hover:text-foreground">
              Source
            </a>
            <span>AGPL-3.0, SDKs MIT</span>
          </nav>
        </div>
      </footer>
    </>
  );
}
