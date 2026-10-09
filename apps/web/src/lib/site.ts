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

/** Support, privacy and legal requests for hosted Bridge. */
export const CONTACT_EMAIL = 'abhimanpanwar6@gmail.com';

/** A file in the repository, as GitHub renders it. */
export const repoFile = (path: string) => `${REPO_URL}/blob/main/${path}`;

/** Documentation pages on this site (rendered from docs/ by Fumadocs, see src/lib/source.ts). */
export const DOCS = {
  home: '/docs/',
  messages: '/docs/messages/',
  webhooks: '/docs/webhooks/',
  otp: '/docs/otp/',
  cli: '/docs/cli/',
  mcp: '/docs/mcp/',
  android: '/docs/android/',
  selfHosting: '/docs/self-hosting/',
  security: '/docs/security/',
  providers: '/docs/providers/',
  broadcasts: '/docs/broadcasts/',
  schedules: '/docs/schedules/',
  automation: '/docs/automation/',
  integrations: '/docs/integrations/',
  supabase: '/docs/integrations/supabase/',
  betterAuth: '/docs/integrations/better-auth/',
  auth0: '/docs/integrations/auth0/',
  noCode: '/docs/integrations/no-code/',
  firebaseClerk: '/docs/integrations/firebase-clerk/',
  billing: '/docs/hosted/billing/',
  sdk: '/docs/sdk/',
} as const;

export const SITE = {
  name: 'Bridge',
  title: 'Bridge: open-source SMS and phone verification',
  description:
    'Open-source SMS API that sends through Android phones you own, hosted or self-hosted, with delivery reports, signed webhooks, a Verify API for one-time codes and fallback to MSG91, Twilio, Vonage or Plivo.',
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
