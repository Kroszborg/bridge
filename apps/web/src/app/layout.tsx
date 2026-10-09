import type { Metadata, Viewport } from 'next';
import { Red_Hat_Display, Red_Hat_Mono, Red_Hat_Text } from 'next/font/google';
import { ThemeProvider } from '@/components/theme';
import { MAKER, SITE, SITE_URL } from '@/lib/site';
import './globals.css';

const display = Red_Hat_Display({ subsets: ['latin'], variable: '--font-redhat-display' });
const text = Red_Hat_Text({ subsets: ['latin'], variable: '--font-redhat-text' });
const mono = Red_Hat_Mono({ subsets: ['latin'], variable: '--font-redhat-mono' });

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: SITE.title,
  description: SITE.description,
  applicationName: SITE.name,
  authors: [{ name: MAKER.name, url: MAKER.portfolio }],
  creator: MAKER.name,
  keywords: [
    'SMS API',
    'SMS gateway',
    'Android SMS gateway',
    'self-hosted SMS',
    'open-source SMS',
    'OTP API',
    'phone verification',
    'Twilio alternative',
    'MSG91',
    'Supabase SMS hook',
  ],
  alternates: { canonical: '/' },
  openGraph: {
    type: 'website',
    url: '/',
    siteName: SITE.name,
    title: SITE.title,
    description:
      'Send SMS and one-time codes through Android phones you own, with delivery reports and signed webhooks. Open source and self-hosted.',
    images: [
      {
        url: '/og.png',
        width: 1200,
        height: 630,
        alt: 'Bridge: send SMS through phones you already own',
      },
    ],
  },
  twitter: {
    card: 'summary_large_image',
    creator: MAKER.xHandle,
    title: SITE.title,
    description:
      'Send SMS and one-time codes through Android phones you own. Open source and self-hosted.',
    images: ['/og.png'],
  },
  robots: { index: true, follow: true },
};

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: dark)', color: '#09090b' },
    { media: '(prefers-color-scheme: light)', color: '#faf8f4' },
  ],
};

// Enables the scroll-reveal styles only when JavaScript runs, so content never stays hidden.
const enableJs = "document.documentElement.classList.add('js')";

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${display.variable} ${text.variable} ${mono.variable}`}
    >
      <head>
        {/* biome-ignore lint/security/noDangerouslySetInnerHtml: a constant one-line script */}
        <script dangerouslySetInnerHTML={{ __html: enableJs }} />
      </head>
      <body>
        <ThemeProvider>{children}</ThemeProvider>
      </body>
    </html>
  );
}
