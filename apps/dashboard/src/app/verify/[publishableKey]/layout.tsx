import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: { absolute: 'Verify your phone' },
  description: 'Confirm your phone number with a one-time code.',
  referrer: 'strict-origin-when-cross-origin',
};

export default function HostedVerifyLayout({ children }: { children: React.ReactNode }) {
  return children;
}
