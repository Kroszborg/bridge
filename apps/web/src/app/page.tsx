import {
  ArrowRight01Icon,
  ArrowUpRight01Icon,
  BatteryCharging01Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  CpuIcon,
  Database01Icon,
  DocumentValidationIcon,
  GithubIcon,
  Mail01Icon,
  PasswordValidationIcon,
  PlusSignIcon,
  SecurityCheckIcon,
  SignalFull01Icon,
  SmartPhone01Icon,
  TestTube01Icon,
  Wifi01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Image from 'next/image';
import { Wordmark } from '@/components/brand';
import { Bridge3D } from '@/components/bridge-3d';
import { CodeTabs, CopyButton } from '@/components/code';
import {
  FadeUp,
  HeadlineReveal,
  Marquee,
  MobileMenu,
  RisingShot,
  SpotlightCard,
} from '@/components/motion';
import { PairingQR } from '@/components/pairing-qr';
import { RevealObserver } from '@/components/reveal';
import { ThemeToggle } from '@/components/theme';
import { FAQ } from '@/lib/faq';
import {
  DASHBOARD_URL,
  DOCS,
  MAKER,
  REPO_URL,
  repoFile,
  SITE,
  SITE_URL,
  STATUS_URL,
} from '@/lib/site';

/** Stagger index for reveal and hero animations. */
const stagger = (i: number) => ({ '--i': i }) as React.CSSProperties;

/** Tools with a Bridge guide, shown under the hero. Logos are Simple Icons (CC0). */
const WORKS_WITH = [
  { slug: 'supabase', name: 'Supabase', href: DOCS.supabase },
  { slug: 'auth0', name: 'Auth0', href: DOCS.auth0 },
  { slug: 'firebase', name: 'Firebase', href: DOCS.firebaseClerk },
  { slug: 'clerk', name: 'Clerk', href: DOCS.firebaseClerk },
  { slug: 'twilio', name: 'Twilio', href: DOCS.providers },
  { slug: 'vonage', name: 'Vonage', href: DOCS.providers },
  { slug: 'n8n', name: 'n8n', href: DOCS.noCode },
  { slug: 'zapier', name: 'Zapier', href: DOCS.noCode },
  { slug: 'make', name: 'Make', href: DOCS.noCode },
] as const;

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
    body: 'SMS Retriever on Android, WebOTP in Chrome, the domain line in Safari.',
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
    language: '@kroszborg/bridge',
    code: `import { Bridge } from '@kroszborg/bridge';

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
    code: `import { verifyWebhook } from '@kroszborg/bridge';

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
  { time: '14:01:03.402', label: 'Phone accepted', detail: 'realme, SIM 1 (Airtel)' },
  { time: '14:01:05.880', label: 'Sent', detail: '1 segment, GSM-7' },
  { time: '14:01:07.215', label: 'Delivered', detail: 'carrier delivery report' },
];

const testNumbers = [
  ['+15550000001', 'Delivered'],
  ['+15550000002', 'Invalid number'],
  ['+15550000003', 'No delivery report'],
  ['+15550000005', 'Undelivered'],
];

/** Structured data for search engines and LLM crawlers. */
const jsonLd = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'Person',
      '@id': `${SITE_URL}/#maker`,
      name: MAKER.name,
      url: MAKER.portfolio,
      sameAs: [MAKER.github, MAKER.x],
    },
    {
      '@type': 'WebSite',
      '@id': `${SITE_URL}/#website`,
      url: `${SITE_URL}/`,
      name: SITE.name,
      description: SITE.description,
      inLanguage: 'en',
      publisher: { '@id': `${SITE_URL}/#maker` },
    },
    {
      '@type': 'SoftwareApplication',
      '@id': `${SITE_URL}/#software`,
      name: 'Bridge',
      url: `${SITE_URL}/`,
      description: SITE.description,
      applicationCategory: 'DeveloperApplication',
      operatingSystem: 'Linux, macOS, Windows (Docker); Android (gateway app)',
      softwareVersion: '1.1.0',
      license: 'https://spdx.org/licenses/AGPL-3.0-only.html',
      isAccessibleForFree: true,
      offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
      author: { '@id': `${SITE_URL}/#maker` },
      downloadUrl: `${REPO_URL}/releases`,
      softwareHelp: { '@type': 'CreativeWork', url: `${SITE_URL}/docs/` },
      featureList: [
        'Send SMS through paired Android phones (UnifiedPush or Firebase Cloud Messaging)',
        'REST API described by OpenAPI 3.1',
        'Delivery reports and a per-message timeline',
        'Incoming SMS forwarding',
        'Signed webhooks (Standard Webhooks)',
        'Verify API for one-time passwords with SMS Retriever and WebOTP autofill',
        'Fallback to MSG91, Twilio, Vonage or Plivo',
        'Supabase Auth Send SMS hook',
        'Dashboard with usage, request logs, playground, teams, audit log and status page',
        'Self-hosted with Docker Compose: one Go binary and PostgreSQL',
      ],
    },
    {
      '@type': 'SoftwareSourceCode',
      '@id': `${SITE_URL}/#source`,
      name: 'Bridge server, dashboard and Android gateway',
      codeRepository: REPO_URL,
      programmingLanguage: ['Go', 'TypeScript', 'Kotlin'],
      license: 'https://spdx.org/licenses/AGPL-3.0-only.html',
      author: { '@id': `${SITE_URL}/#maker` },
      targetProduct: { '@id': `${SITE_URL}/#software` },
    },
    {
      '@type': 'SoftwareSourceCode',
      '@id': `${SITE_URL}/#sdk`,
      name: '@kroszborg/bridge',
      description: 'Zero-dependency TypeScript SDK for the Bridge API.',
      codeRepository: `${REPO_URL}/tree/main/packages/sdk`,
      programmingLanguage: 'TypeScript',
      license: 'https://spdx.org/licenses/MIT.html',
      author: { '@id': `${SITE_URL}/#maker` },
      targetProduct: { '@id': `${SITE_URL}/#software` },
    },
    {
      '@type': 'FAQPage',
      '@id': `${SITE_URL}/#faq`,
      mainEntity: FAQ.map((f) => ({
        '@type': 'Question',
        name: f.q,
        acceptedAnswer: { '@type': 'Answer', text: f.a },
      })),
    },
  ],
};

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

function PrimaryCta() {
  return (
    <a
      href={`${DASHBOARD_URL}/signup`}
      className="group inline-flex h-12 items-center gap-2 rounded-xl bg-primary px-6 text-sm font-semibold text-primary-foreground shadow-[0_10px_30px_-12px_var(--primary)] transition-[filter,transform] hover:brightness-110 active:translate-y-px"
    >
      Start free
      <HugeiconsIcon
        icon={ArrowRight01Icon}
        strokeWidth={2}
        className="size-4 transition-transform group-hover:translate-x-0.5"
      />
    </a>
  );
}

function SecondaryCta() {
  return (
    <a
      href="#self-host"
      className="inline-flex h-12 items-center rounded-xl border bg-card/70 px-6 text-sm font-semibold backdrop-blur transition-[background-color,transform] hover:bg-muted active:translate-y-px"
    >
      Self-host it
    </a>
  );
}

/** A text link with a trailing arrow, for "read more" style destinations. */
function TextLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className="group inline-flex items-center gap-1.5 text-sm font-semibold text-foreground underline decoration-border underline-offset-4 transition-colors hover:decoration-primary"
    >
      {children}
      <HugeiconsIcon
        icon={ArrowUpRight01Icon}
        strokeWidth={2}
        className="size-4 text-muted-foreground transition-transform group-hover:-translate-y-px group-hover:translate-x-px"
      />
    </a>
  );
}

const navLinks = [
  ['#verify', 'Verify'],
  ['#developers', 'Developers'],
  ['#pricing', 'Pricing'],
  ['/docs/', 'Docs'],
  ['#faq', 'FAQ'],
] as const;

/** Hosted plans. Keep in step with the plans table (migration 00014). */
const PLANS = [
  {
    name: 'Free',
    price: '$0',
    blurb: 'Try Bridge on one phone.',
    items: ['1 phone', '300 live SMS a month', '1 project', 'Just you'],
    cta: 'Start free',
  },
  {
    name: 'Pro',
    price: '$5',
    blurb: 'For an app in production.',
    items: ['3 phones', '5,000 live SMS a month', '3 projects', '3 team members'],
    cta: 'Start with Pro',
    featured: true,
  },
  {
    name: 'Team',
    price: '$15',
    blurb: 'For teams and several apps.',
    items: ['10 phones', '25,000 live SMS a month', 'Unlimited projects', '10 team members'],
    cta: 'Start with Team',
  },
] as const;

export default function Home() {
  return (
    <>
      <script
        type="application/ld+json"
        // biome-ignore lint/security/noDangerouslySetInnerHtml: static JSON-LD built from constants
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd).replace(/</g, '\\u003c') }}
      />
      <RevealObserver />
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50 focus:rounded-lg focus:bg-card focus:px-4 focus:py-2 focus:text-sm focus:font-semibold"
      >
        Skip to content
      </a>
      <header className="sticky top-0 z-40 bg-background/85 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
        <nav
          aria-label="Main"
          className="mx-auto flex h-16 max-w-7xl items-center justify-between gap-6 px-4 sm:px-6"
        >
          <a href="/" aria-label="Bridge home" className="rounded-lg">
            <Wordmark />
          </a>
          <div className="hidden items-center gap-6 text-sm text-muted-foreground lg:flex">
            {navLinks.map(([href, label]) => (
              <a key={href} href={href} className="transition-colors hover:text-foreground">
                {label}
              </a>
            ))}
          </div>
          <div className="flex items-center gap-1.5">
            <a
              href={REPO_URL}
              aria-label="Bridge on GitHub"
              className="grid size-9 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              <HugeiconsIcon icon={GithubIcon} strokeWidth={2} className="size-[1.1rem]" />
            </a>
            <ThemeToggle />
            <a
              href={`${DASHBOARD_URL}/login`}
              className="ml-1.5 hidden h-9 items-center rounded-lg border bg-card px-3.5 text-sm font-semibold transition-colors hover:bg-muted sm:inline-flex"
            >
              Sign in
            </a>
            <MobileMenu
              links={navLinks}
              signIn={{ href: `${DASHBOARD_URL}/login`, label: 'Sign in' }}
            />
          </div>
        </nav>
      </header>

      <main id="main" className="page-bg">
        {/*
          Hero: the promise, then the bridge itself, assembling out of points of light. The
          bridge's canvas covers the whole hero, behind the copy, so a click anywhere but the
          buttons can throw it apart and the pieces have the full hero to fly through.
        */}
        <section className="relative isolate flex flex-col items-center overflow-hidden px-4 pt-20 pb-10 sm:px-6 sm:pt-24 sm:pb-14">
          <div className="hero-glow" aria-hidden />
          <Bridge3D />
          <div className="pointer-events-none relative z-10 flex flex-col items-center [&_a]:pointer-events-auto">
            <FadeUp>
              <p className="inline-flex items-center gap-2 rounded-full border bg-card/60 px-3 py-1 text-xs font-medium text-muted-foreground backdrop-blur-sm">
                <span className="size-1.5 rounded-full bg-primary" aria-hidden />
                Open source · SMS and phone verification
              </p>
            </FadeUp>
            <h1 className="mt-6 text-balance text-center text-[2.4rem] font-bold leading-[1.06] tracking-[-0.035em] sm:text-6xl lg:text-[4.25rem]">
              <HeadlineReveal
                lines={[
                  { text: 'Send SMS through phones' },
                  { text: 'you already own.', className: 'text-primary' },
                ]}
              />
            </h1>
            <FadeUp delay={0.45}>
              <p className="mt-5 max-w-[34rem] text-center text-lg leading-relaxed text-muted-foreground">
                Pair an Android phone, call one API, and follow every message to the carrier&apos;s
                delivery report.
              </p>
            </FadeUp>
            <FadeUp delay={0.6} className="mt-8 flex flex-wrap justify-center gap-3">
              <PrimaryCta />
              <SecondaryCta />
            </FadeUp>
            <FadeUp delay={0.7}>
              <p className="mt-4 text-center text-xs text-faint">
                Free plan, no card needed · AGPL-3.0 to self-host
              </p>
            </FadeUp>
          </div>
          {/* Where the bridge stands, right under the buttons; the canvas frames it into this box. */}
          <div
            data-bridge-stage
            aria-hidden
            className="-mx-4 mt-6 aspect-[5/2] w-[calc(100%+2rem)] sm:-mx-6 sm:mt-6 sm:aspect-[4/1] sm:w-[calc(100%+3rem)] xl:aspect-[5/1]"
          />
        </section>

        {/* What Bridge plugs into, as real logos. Each has a guide in the repository. */}
        <section aria-labelledby="works-with">
          <div className="mx-auto flex max-w-7xl flex-col items-center gap-6 px-4 py-14 sm:px-6 lg:flex-row lg:gap-10 lg:py-16">
            <p id="works-with" className="shrink-0 text-sm text-muted-foreground">
              Works with the tools you already use
            </p>
            <div className="min-w-0 w-full flex-1">
              <Marquee>
                {WORKS_WITH.map((l) => (
                  <span key={l.slug} className="flex justify-center">
                    <a
                      href={l.href}
                      title={l.name}
                      className="group flex items-center gap-2 text-muted-foreground transition-colors hover:text-foreground"
                    >
                      <span
                        aria-hidden
                        className="size-5 bg-current opacity-80 transition-opacity group-hover:opacity-100"
                        style={{
                          mask: `url(/logos/${l.slug}.svg) center / contain no-repeat`,
                          WebkitMask: `url(/logos/${l.slug}.svg) center / contain no-repeat`,
                        }}
                      />
                      <span className="text-sm font-medium">{l.name}</span>
                    </a>
                  </span>
                ))}
              </Marquee>
            </div>
          </div>
        </section>

        {/* The console, settling flat as it scrolls into view. */}
        <section className="relative">
          <div className="mx-auto flex max-w-7xl flex-col items-center px-4 pt-20 pb-24 text-center sm:px-6">
            <h2 data-reveal className="max-w-2xl text-3xl font-bold sm:text-4xl">
              Every message, in one console.
            </h2>
            <p data-reveal className="mt-4 max-w-xl text-lg leading-relaxed text-muted-foreground">
              Which phone sent it, which SIM, and what the carrier said, live.
            </p>
            <RisingShot className="mt-14 w-full max-w-6xl">
              <div className="shot overflow-hidden rounded-2xl border bg-card p-1.5">
                <div className="overflow-hidden rounded-xl">
                  <Screenshot
                    name="messages"
                    priority
                    alt="The Bridge console listing delivered messages and forwarded incoming SMS for a project"
                  />
                </div>
              </div>
            </RisingShot>
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
            <ol className="mt-14 grid gap-10 lg:grid-cols-[minmax(0,6fr)_minmax(0,4fr)_minmax(0,4fr)] lg:gap-6">
              <li data-reveal style={stagger(0)} className="flex min-w-0 flex-col gap-4">
                <h3 className="text-lg font-semibold">Your app sends</h3>
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
              <li data-reveal style={stagger(1)} className="flex min-w-0 flex-col gap-4">
                <h3 className="text-lg font-semibold">Bridge picks a phone</h3>
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
              <li data-reveal style={stagger(2)} className="flex min-w-0 flex-col gap-4">
                <h3 className="text-lg font-semibold">The phone reports back</h3>
                <div className="flex flex-col gap-4 rounded-2xl border bg-card p-5">
                  <div className="flex items-center gap-3">
                    <span className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground">
                      <HugeiconsIcon icon={SmartPhone01Icon} strokeWidth={2} className="size-5" />
                    </span>
                    <div>
                      <div className="text-sm font-semibold">realme, Android 14</div>
                      <div className="text-xs text-success">Online</div>
                    </div>
                  </div>
                  <ul className="grid grid-cols-3 gap-3 text-xs text-muted-foreground">
                    <li className="flex items-center gap-1.5">
                      <HugeiconsIcon
                        icon={BatteryCharging01Icon}
                        strokeWidth={2}
                        className="size-3.5"
                      />
                      Charging
                    </li>
                    <li className="flex items-center gap-1.5">
                      <HugeiconsIcon icon={Wifi01Icon} strokeWidth={2} className="size-3.5" />
                      Wi-Fi
                    </li>
                    <li className="flex items-center gap-1.5">
                      <HugeiconsIcon icon={SignalFull01Icon} strokeWidth={2} className="size-3.5" />
                      Airtel
                    </li>
                  </ul>
                  <p className="text-sm text-muted-foreground">
                    Sent, then delivered, as the carrier reports it. Never guessed.
                  </p>
                </div>
              </li>
            </ol>
          </div>
        </section>

        {/* What you get: a bento with real artifacts. */}
        <section className="border-t">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-28">
            <h2 data-reveal className="max-w-2xl text-3xl font-bold sm:text-4xl">
              Everything after the API call is handled.
            </h2>
            <div className="mt-12 grid gap-4 lg:grid-cols-6">
              <article
                data-reveal
                className="rounded-2xl border bg-card p-6 lg:col-span-4 lg:row-span-2"
              >
                <div className="grid h-full gap-8 lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)] lg:items-center">
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
                  <div className="rounded-xl border bg-background p-5">
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
                          <span className="min-w-0">
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
                style={stagger(1)}
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
                style={stagger(2)}
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
                style={stagger(1)}
                className="rounded-2xl border bg-gradient-to-br from-primary/[0.07] via-card to-card p-6 lg:col-span-3"
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
              <div className="mt-6">
                <TextLink href={DOCS.otp}>Read the Verify guide</TextLink>
              </div>
            </div>

            <figure data-reveal className="mx-auto w-full max-w-sm">
              <div className="rounded-[1.75rem] border bg-card p-5 shadow-[0_24px_60px_-36px_var(--shadow)]">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span className="font-semibold text-foreground">Pinecart</span>
                  <span>now</span>
                </div>
                <p className="mt-2 rounded-2xl rounded-tl-md bg-muted px-4 py-3 text-sm leading-relaxed">
                  482913 is your Pinecart code. It expires in 10 minutes. Do not share it.
                  <span className="mt-2 block font-mono text-xs text-muted-foreground">
                    @pinecart.in #482913
                  </span>
                </p>
                <div className="mt-6 border-t pt-5">
                  <p className="text-sm font-semibold">Sign in to Pinecart</p>
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
                  <span className="mt-3 inline-flex items-center gap-2 rounded-lg border bg-background px-3 py-1.5 text-xs">
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
                The last line lets Chrome and Safari offer the code above the keyboard.
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

        {/* Pricing: hosted plans; self-hosting below is free. */}
        <section id="pricing" className="scroll-mt-20 border-t">
          <div className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:py-28">
            <div data-reveal className="mx-auto max-w-2xl text-center">
              <h2 className="text-3xl font-bold sm:text-4xl">
                Simple pricing. Your SIMs do the sending.
              </h2>
              <p className="mt-4 text-lg leading-relaxed text-muted-foreground">
                Messages go out through your own phones, so there is no per-SMS fee. Test messages
                are unlimited on every plan.
              </p>
            </div>
            <div className="mx-auto mt-14 grid max-w-5xl gap-4 md:grid-cols-3">
              {PLANS.map((p) => (
                <SpotlightCard
                  key={p.name}
                  className={`rounded-2xl border p-6 ${
                    'featured' in p ? 'border-primary/50 bg-primary/[0.04]' : 'bg-card/60'
                  }`}
                >
                  <div className="flex items-baseline justify-between">
                    <h3 className="font-display text-lg font-semibold">{p.name}</h3>
                    {'featured' in p ? (
                      <span className="text-xs font-medium text-primary">Most popular</span>
                    ) : null}
                  </div>
                  <p className="mt-3">
                    <span className="font-display text-4xl font-bold tracking-tight">
                      {p.price}
                    </span>
                    <span className="text-sm text-muted-foreground"> / month</span>
                  </p>
                  <p className="mt-2 text-sm text-muted-foreground">{p.blurb}</p>
                  <ul className="mt-6 flex flex-1 flex-col gap-2.5 text-sm">
                    {p.items.map((item) => (
                      <li key={item} className="flex items-center gap-2.5">
                        <HugeiconsIcon
                          icon={CheckmarkCircle02Icon}
                          strokeWidth={2}
                          className="size-4 shrink-0 text-primary"
                        />
                        {item}
                      </li>
                    ))}
                  </ul>
                  <a
                    href={`${DASHBOARD_URL}/signup`}
                    className={`mt-8 inline-flex h-11 items-center justify-center rounded-xl text-sm font-semibold transition-[filter,background-color] ${
                      'featured' in p
                        ? 'bg-primary text-primary-foreground hover:brightness-110'
                        : 'border bg-card hover:bg-muted'
                    }`}
                  >
                    {p.cta}
                  </a>
                </SpotlightCard>
              ))}
            </div>
            <p className="mx-auto mt-8 max-w-2xl text-center text-sm text-muted-foreground">
              Prices in USD, before tax. Each phone sends at most 100 SMS a day by default, within
              what most operators allow a SIM, so your numbers stay safe.{' '}
              <a
                href="#self-host"
                className="font-medium text-foreground underline-offset-4 hover:underline"
              >
                Or self-host Bridge for free.
              </a>
            </p>
          </div>
        </section>

        {/* Self-host: facts left, the three commands right. */}
        <section id="self-host" className="scroll-mt-20 border-t">
          <div className="mx-auto grid max-w-7xl items-center gap-12 px-4 py-20 sm:px-6 lg:grid-cols-2 lg:py-28">
            <div data-reveal>
              <h2 className="text-3xl font-bold sm:text-4xl">
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
              <div className="mt-8">
                <TextLink href={DOCS.selfHosting}>Read the self-hosting guide</TextLink>
              </div>
            </div>
            <div
              data-reveal
              style={stagger(1)}
              className="min-w-0 overflow-hidden rounded-2xl border bg-card"
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

        {/* FAQ: native disclosure widgets, readable without JavaScript. */}
        <section id="faq" className="scroll-mt-20 border-t">
          <div className="mx-auto grid max-w-7xl gap-12 px-4 py-20 sm:px-6 lg:grid-cols-[minmax(0,4fr)_minmax(0,8fr)] lg:py-28">
            <div data-reveal className="lg:sticky lg:top-28 lg:self-start">
              <h2 className="text-3xl font-bold sm:text-4xl">Frequently asked questions</h2>
              <p className="mt-4 text-muted-foreground">
                Something missing?{' '}
                <a
                  href={`${REPO_URL}/issues`}
                  className="font-medium text-foreground underline decoration-border underline-offset-4 hover:decoration-primary"
                >
                  Open an issue on GitHub
                </a>
                .
              </p>
            </div>
            <div data-reveal style={stagger(1)} className="min-w-0 divide-y border-y">
              {FAQ.map((f, i) => (
                <details key={f.q} className="group" open={i === 0}>
                  <summary className="flex cursor-pointer list-none items-center justify-between gap-6 py-5 [&::-webkit-details-marker]:hidden">
                    <h3 className="text-base font-semibold sm:text-lg">{f.q}</h3>
                    <HugeiconsIcon
                      icon={PlusSignIcon}
                      strokeWidth={2}
                      className="size-4 shrink-0 text-muted-foreground transition-transform duration-300 group-open:rotate-45"
                    />
                  </summary>
                  <p className="max-w-2xl pb-6 leading-relaxed text-muted-foreground">{f.a}</p>
                </details>
              ))}
            </div>
          </div>
        </section>

        {/* The maker: same pattern as Beam's about card, in Bridge's materials. */}
        <section id="maker" className="scroll-mt-20 border-t">
          <div className="mx-auto max-w-5xl px-4 py-20 sm:px-6 lg:py-24">
            <p data-reveal className="text-sm font-medium text-primary">
              Made by
            </p>
            <article data-reveal className="mt-6 overflow-hidden rounded-2xl border bg-card">
              <div className="grid gap-6 p-6 sm:p-8 md:grid-cols-[auto_1fr] md:gap-8">
                <span
                  aria-hidden
                  className="grid size-16 place-items-center rounded-full border bg-muted font-display text-xl font-bold"
                >
                  {MAKER.initials}
                </span>
                <div className="flex min-w-0 flex-col gap-5">
                  <div className="flex flex-col gap-1">
                    <h2 className="text-3xl font-bold">{MAKER.name}</h2>
                    <p className="text-sm text-muted-foreground">{MAKER.tagline}</p>
                  </div>
                  <p className="max-w-2xl leading-relaxed text-muted-foreground">
                    Hi, I&apos;m Abhiman. Bridge is an independent, open-source project: one API for
                    SMS and phone verification that you run yourself, on phones you own.
                  </p>
                  <div className="flex flex-wrap gap-3">
                    <a
                      href={MAKER.portfolio}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-10 items-center gap-2 rounded-xl bg-foreground px-4 text-sm font-semibold text-background transition-[opacity,transform] hover:opacity-90 active:translate-y-px"
                    >
                      Visit my portfolio
                      <HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={2} className="size-4" />
                    </a>
                    <a
                      href={MAKER.github}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-10 items-center gap-2 rounded-xl border px-4 text-sm font-semibold transition-[background-color,transform] hover:bg-muted active:translate-y-px"
                    >
                      <HugeiconsIcon icon={GithubIcon} strokeWidth={2} className="size-4" />
                      GitHub
                    </a>
                    <a
                      href={MAKER.x}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-10 items-center gap-2 rounded-xl border px-4 text-sm font-semibold transition-[background-color,transform] hover:bg-muted active:translate-y-px"
                    >
                      X<span className="font-normal text-muted-foreground">{MAKER.xHandle}</span>
                    </a>
                  </div>
                </div>
              </div>
              <div className="border-t bg-background/50 px-6 py-6 sm:px-8">
                <h3 className="text-sm font-semibold">Also by Abhiman</h3>
                <ul className="mt-4 grid gap-3 sm:grid-cols-2">
                  {MAKER.projects.map((p) => (
                    <li key={p.name}>
                      <a
                        href={p.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="group flex h-full items-start justify-between gap-4 rounded-xl border bg-card p-4 transition-colors hover:bg-muted/60"
                      >
                        <span className="flex flex-col gap-1">
                          <span className="font-semibold">{p.name}</span>
                          <span className="text-sm leading-relaxed text-muted-foreground">
                            {p.body}
                          </span>
                        </span>
                        <HugeiconsIcon
                          icon={ArrowUpRight01Icon}
                          strokeWidth={2}
                          className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-hover:-translate-y-px group-hover:translate-x-px group-hover:text-foreground"
                        />
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            </article>
          </div>
        </section>

        {/* Closing call to action. */}
        <section className="border-t">
          <div
            data-reveal
            className="mx-auto flex max-w-3xl flex-col items-center gap-6 px-4 py-24 text-center sm:px-6"
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
        <div className="mx-auto grid max-w-7xl gap-10 px-4 py-12 text-sm sm:px-6 md:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <div className="flex flex-col gap-4">
            <Wordmark />
            <p className="max-w-xs leading-relaxed text-muted-foreground">
              Open-source SMS and phone verification through phones you own.
            </p>
          </div>
          <div className="grid grid-cols-2 gap-8 sm:grid-cols-3">
            <nav aria-label="Product" className="flex flex-col gap-2.5">
              <p className="font-semibold">Product</p>
              <a href={DASHBOARD_URL} className="text-muted-foreground hover:text-foreground">
                Dashboard
              </a>
              <a href={STATUS_URL} className="text-muted-foreground hover:text-foreground">
                Status
              </a>
              <a href="#pricing" className="text-muted-foreground hover:text-foreground">
                Pricing
              </a>
            </nav>
            <nav aria-label="Documentation" className="flex flex-col gap-2.5">
              <p className="font-semibold">Docs</p>
              <a href={DOCS.home} className="text-muted-foreground hover:text-foreground">
                Guides
              </a>
              <a href={DOCS.sdk} className="text-muted-foreground hover:text-foreground">
                TypeScript SDK
              </a>
              <a
                href={repoFile('packages/api-types/openapi.json')}
                className="text-muted-foreground hover:text-foreground"
              >
                OpenAPI
              </a>
              <a href="/llms.txt" className="text-muted-foreground hover:text-foreground">
                llms.txt
              </a>
            </nav>
            <nav aria-label="Project" className="flex flex-col gap-2.5">
              <p className="font-semibold">Project</p>
              <a href={REPO_URL} className="text-muted-foreground hover:text-foreground">
                Source
              </a>
              <a
                href={repoFile('CHANGELOG.md')}
                className="text-muted-foreground hover:text-foreground"
              >
                Changelog
              </a>
              <a href={DOCS.security} className="text-muted-foreground hover:text-foreground">
                Security
              </a>
              <a href="/privacy/" className="text-muted-foreground hover:text-foreground">
                Privacy
              </a>
              <a href="/terms/" className="text-muted-foreground hover:text-foreground">
                Terms
              </a>
            </nav>
          </div>
        </div>
        <div className="mx-auto max-w-7xl px-4 sm:px-6">
          <div className="flex flex-col gap-2 border-t py-6 text-xs text-muted-foreground sm:flex-row sm:justify-between">
            <span>Server, dashboard and app under AGPL-3.0. SDKs under MIT.</span>
            <span>
              Built by{' '}
              <a
                href={MAKER.portfolio}
                className="font-medium text-foreground underline decoration-border underline-offset-4 hover:decoration-primary"
              >
                {MAKER.name}
              </a>
            </span>
          </div>
        </div>
      </footer>
    </>
  );
}
