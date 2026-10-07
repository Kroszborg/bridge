import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'Verify' };

export default function Layout({ children }: { children: React.ReactNode }) {
  return children;
}
