import type { MeResponse } from '@bridge/api-types';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { serverGet } from '@/lib/server-api';

export const metadata: Metadata = { title: 'System health' };

/**
 * System health and Insights exist only for the operators of this Bridge
 * instance. Everyone else gets the ordinary 404 page, as the API answers them
 * with an unknown route. Other /v1/me failures (signed out, API down) are left
 * to the console layout.
 */
export default async function Layout({ children }: { children: React.ReactNode }) {
  const me = await serverGet<MeResponse>('/v1/me');
  if (me.ok && !me.data.user.operator) notFound();
  return children;
}
