/** Where the console lives. Set NEXT_PUBLIC_DASHBOARD_URL when you deploy. */
export const DASHBOARD_URL = (
  process.env.NEXT_PUBLIC_DASHBOARD_URL ?? 'http://localhost:3000'
).replace(/\/$/, '');

/** Public source repository. Set NEXT_PUBLIC_REPO_URL to point a fork at itself. */
export const REPO_URL = (
  process.env.NEXT_PUBLIC_REPO_URL ?? 'https://github.com/kroszborg/bridge'
).replace(/\/$/, '');

/** Where this website is served. Used for canonical URLs, the sitemap and structured data. */
export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL ?? 'https://bridge.kroszborg.co').replace(
  /\/$/,
  '',
);

export const STATUS_URL = `${DASHBOARD_URL}/status`;

/** A file in the repository, as GitHub renders it. */
export const repoFile = (path: string) => `${REPO_URL}/blob/main/${path}`;

export const DOCS = {
  messages: repoFile('docs/messages/README.md'),
  webhooks: repoFile('docs/webhooks/README.md'),
  otp: repoFile('docs/otp/README.md'),
  cli: repoFile('docs/cli/README.md'),
  android: repoFile('docs/android/README.md'),
  selfHosting: repoFile('docs/self-hosting/README.md'),
  security: repoFile('docs/security/README.md'),
  providers: repoFile('docs/providers/README.md'),
  integrations: repoFile('docs/integrations/README.md'),
  supabase: repoFile('docs/integrations/supabase.md'),
  betterAuth: repoFile('docs/integrations/better-auth.md'),
  auth0: repoFile('docs/integrations/auth0.md'),
  noCode: repoFile('docs/integrations/no-code.md'),
  firebaseClerk: repoFile('docs/integrations/firebase-clerk.md'),
  sdk: repoFile('packages/sdk/README.md'),
} as const;

export const SITE = {
  name: 'Bridge',
  title: 'Bridge: open-source SMS and phone verification',
  description:
    'Self-hosted SMS API that sends through Android phones you own, with delivery reports, signed webhooks, a Verify API for one-time codes and fallback to MSG91, Twilio, Vonage or Plivo.',
} as const;

/** The maker, as described on beam.kroszborg.co and rune.kroszborg.co. */
export const MAKER = {
  name: 'Abhiman Panwar',
  initials: 'AP',
  tagline: 'Independent developer. I build fast, privacy-first web tools.',
  portfolio: 'https://www.kroszborg.co/',
  github: 'https://github.com/kroszborg',
  x: 'https://x.com/kroszborgg',
  xHandle: '@kroszborgg',
  projects: [
    {
      name: 'Beam',
      url: 'https://beam.kroszborg.co/',
      body: 'End-to-end encrypted, peer-to-peer file transfer. No uploads. No accounts.',
    },
    {
      name: 'Rune',
      url: 'https://rune.kroszborg.co/',
      body: 'A lightweight, fully customizable QR code library. It draws the pairing code on this page.',
    },
  ],
} as const;
