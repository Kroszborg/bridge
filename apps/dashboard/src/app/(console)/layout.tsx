import type { MeResponse } from '@bridge/api-types';
import { redirect } from 'next/navigation';
import { ApiUnreachable } from '@/components/api-unreachable';
import { ConsoleProvider } from '@/components/layout/console-context';
import { ConsoleShell } from '@/components/layout/shell';
import { PUBLIC_API_URL, serverGet } from '@/lib/server-api';

export default async function ConsoleLayout({ children }: { children: React.ReactNode }) {
  const me = await serverGet<MeResponse>('/v1/me');
  if (!me.ok) {
    if (me.unreachable) return <ApiUnreachable />;
    if (me.status === 401) redirect('/login');
    throw new Error(`Bridge API returned ${me.status} for /v1/me`);
  }
  return (
    <ConsoleProvider
      value={{ user: me.data.user, organizations: me.data.organizations, apiUrl: PUBLIC_API_URL }}
    >
      <ConsoleShell>{children}</ConsoleShell>
    </ConsoleProvider>
  );
}
