import type { Metadata, Viewport } from 'next';
import { Red_Hat_Display, Red_Hat_Mono, Red_Hat_Text } from 'next/font/google';
import './globals.css';
import { Providers } from '@/components/providers';
import { Toaster } from '@/components/ui/sonner';
import { cn } from '@/lib/utils';

const display = Red_Hat_Display({ subsets: ['latin'], variable: '--font-redhat-display' });
const text = Red_Hat_Text({ subsets: ['latin'], variable: '--font-redhat-text' });
const mono = Red_Hat_Mono({ subsets: ['latin'], variable: '--font-redhat-mono' });

export const metadata: Metadata = {
  title: { default: 'Bridge', template: '%s · Bridge' },
  description: 'Open-source infrastructure for SMS and phone verification.',
  robots: { index: false, follow: false },
};

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: dark)', color: '#0c0b0a' },
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
