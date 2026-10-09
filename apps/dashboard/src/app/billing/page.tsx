import type { MeResponse, Project } from '@bridge/api-types';
import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { ApiUnreachable } from '@/components/api-unreachable';
import { serverGet } from '@/lib/server-api';

/** Opens billing for the workspace of the last project, or the first workspace. */
export default async function BillingShortcut() {
  const me = await serverGet<MeResponse>('/v1/me');
  if (!me.ok) {
    if (me.unreachable) return <ApiUnreachable />;
    redirect('/login');
  }

  const last = (await cookies()).get('bridge_last_project')?.value;
  if (last && /^prj_[0-9a-z]{26}$/.test(last)) {
    const p = await serverGet<Project>(`/v1/projects/${last}`);
    if (p.ok) redirect(`/organizations/${p.data.organization_id}/billing`);
  }
  const first = me.data.organizations[0];
  redirect(first ? `/organizations/${first.id}/billing` : '/account');
}
