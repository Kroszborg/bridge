import type { MeResponse, Project } from '@bridge/api-types';
import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { ApiUnreachable } from '@/components/api-unreachable';
import { serverGet } from '@/lib/server-api';

/** Sends a signed-in user to their last project, or the first one they can access. */
export default async function Home() {
  const me = await serverGet<MeResponse>('/v1/me');
  if (!me.ok) {
    if (me.unreachable) return <ApiUnreachable />;
    redirect('/login');
  }

  const last = (await cookies()).get('bridge_last_project')?.value;
  if (last && /^prj_[0-9a-z]{26}$/.test(last)) {
    const p = await serverGet<Project>(`/v1/projects/${last}`);
    if (p.ok) redirect(`/projects/${p.data.id}`);
  }
  for (const org of me.data.organizations) {
    const res = await serverGet<{ data: Project[] }>(`/v1/organizations/${org.id}/projects`);
    const first = res.ok ? res.data.data[0] : undefined;
    if (first) redirect(`/projects/${first.id}`);
  }
  redirect('/account');
}
