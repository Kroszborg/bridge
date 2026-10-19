import type { Metadata, Viewport } from 'next';
import { Red_Hat_Display, Red_Hat_Mono, Red_Hat_Text } from 'next/font/google';
import { connection } from 'next/server';
import './globals.css';
import { Providers } from '@/components/providers';
import { Toaster } from '@/components/ui/sonner';
import { cn } from '@/lib/utils';

const display = Red_Hat_Display({ subsets: ['latin'], variable: '--font-redhat-display' });
const text = Red_Hat_Text({ subsets: ['latin'], variable: '--font-redhat-text' });
const mono = Red_Hat_Mono({ subsets: ['latin'], variable: '--font-redhat-mono' });

const description = 'Open-source infrastructure for SMS and phone verification.';

// Link previews need absolute URLs, and the address is only known when the server runs.
export async function generateMetadata(): Promise<Metadata> {
  await connection();
  const base = (process.env.BRIDGE_DASHBOARD_URL ?? 'http://localhost:3000').replace(/\/$/, '');
  const image = {
    url: '/og.png',
    width: 1200,
    height: 630,
    alt: 'Bridge: send SMS through phones you already own',
  };
  return {
    metadataBase: new URL(base),
    title: { default: 'Bridge', template: '%s · Bridge' },
    description,
    robots: { index: false, follow: false },
    openGraph: {
      type: 'website',
      siteName: 'Bridge',
      title: 'Bridge',
      description,
      images: [image],
    },
    twitter: { card: 'summary_large_image', title: 'Bridge', description, images: [image.url] },
  };
}

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: dark)', color: '#09090b' },
    { media: '(prefers-color-scheme: light)', color: '#faf8f4' },
  ],
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={cn('h-full antialiased font-sans', display.variable, text.variable, mono.variable)}
    >
      <body className="flex min-h-full flex-col">
        <Providers>{children}</Providers>
        <Toaster />
      </body>
    </html>
  );
}
