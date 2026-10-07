import type { Metadata } from 'next';

// A plain string title here would stop the root template reaching the project's
// pages, so the template is repeated: "Overview · Bridge", "Messages · Bridge".
export const metadata: Metadata = { title: { default: 'Overview', template: '%s · Bridge' } };

export default function Layout({ children }: { children: React.ReactNode }) {
  return children;
}
