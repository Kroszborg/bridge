import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'Send' };

export default function Layout({ children }: { children: React.ReactNode }) {
  return children;
}
