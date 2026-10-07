import type { Metadata, Viewport } from 'next';
import { Red_Hat_Display, Red_Hat_Mono, Red_Hat_Text } from 'next/font/google';
import { ThemeProvider } from '@/components/theme';
import './globals.css';

const display = Red_Hat_Display({ subsets: ['latin'], variable: '--font-redhat-display' });
const text = Red_Hat_Text({ subsets: ['latin'], variable: '--font-redhat-text' });
const mono = Red_Hat_Mono({ subsets: ['latin'], variable: '--font-redhat-mono' });

export const metadata: Metadata = {
  title: 'Bridge: send SMS through phones you already own',
  description:
    'Open-source SMS infrastructure. Pair an Android phone, call one API, get signed webhooks and delivery reports. Self-host with Docker.',
  openGraph: {
    title: 'Bridge',
    description:
      'Send SMS through phones you already own. One API, delivery reports, webhooks. Open source.',
    type: 'website',
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: dark)', color: '#0c0b0a' },
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
