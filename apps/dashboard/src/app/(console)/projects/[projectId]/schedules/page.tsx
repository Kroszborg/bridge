import { redirect } from 'next/navigation';

// Schedules moved into Send as a tab; keep old links and bookmarks working.
export default async function SchedulesRedirect({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  redirect(`/projects/${projectId}/send?tab=scheduled`);
}
