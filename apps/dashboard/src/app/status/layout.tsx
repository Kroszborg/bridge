import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Status',
  description: 'Live health and 90-day uptime of this Bridge installation.',
};

export default function StatusLayout({ children }: { children: React.ReactNode }) {
  return children;
}
