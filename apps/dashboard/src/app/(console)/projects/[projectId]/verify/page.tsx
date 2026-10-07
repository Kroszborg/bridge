'use client';

import type { CreatedVerifyApp, VerifyApp } from '@bridge/api-types';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useState } from 'react';
import { EmptyState } from '@/components/kit/empty-state';
import { PageHeader } from '@/components/kit/page-header';
import { Segmented } from '@/components/kit/segmented';
import { useProjectId } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useVerifyApps, type VerifyEnvironment } from '@/lib/queries';
import { Blocks, Integrate, Recent, Stats, TryIt } from './_components/activity';
import { AppBar, CreatedDialog } from './_components/apps';
import { FailoverCard, FraudCard, MessageCard } from './_components/settings';
import { WidgetCard } from './_components/widget';

export default function VerifyPage() {
  const projectId = useProjectId() ?? '';
  const [environment, setEnvironment] = useState<VerifyEnvironment>('test');
  const [created, setCreated] = useState<CreatedVerifyApp | null>(null);
  const apps = useVerifyApps(projectId);
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();

  // The selected app lives in ?app=<slug> so links and reloads keep it.
  const list = apps.data ?? [];
  const wanted = params.get('app');
  const app = list.find((a) => a.slug === wanted || a.id === wanted) ?? list[0];

  function select(next: VerifyApp) {
    const q = new URLSearchParams(params.toString());
    if (next.is_default) q.delete('app');
    else q.set('app', next.slug);
    const s = q.toString();
    router.replace(s ? `${pathname}?${s}` : pathname, { scroll: false });
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Verify"
        subtitle="One-time passwords that Bridge generates, sends and checks. Your app makes two calls and never stores a code."
        actions={
          <Segmented
            label="Environment"
            value={environment}
            onChange={setEnvironment}
            options={[
              { value: 'test', label: 'Test' },
              { value: 'live', label: 'Live' },
            ]}
          />
        }
      />
      {apps.isPending ? (
        <>
          <Skeleton className="h-14" />
          <div className="grid grid-cols-2 gap-4 md:grid-cols-3 2xl:grid-cols-6">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
        </>
      ) : apps.isError || !app ? (
        <div className="rounded-xl border bg-card">
          <EmptyState
            title="Could not load Verify apps"
            description={apps.error?.message ?? 'The project has no Verify app.'}
            action={
              <Button variant="outline" onClick={() => apps.refetch()}>
                Try again
              </Button>
            }
          />
        </div>
      ) : (
        <AppView
          key={app.id}
          projectId={projectId}
          apps={list}
          app={app}
          environment={environment}
          onSelect={select}
          onCreated={(a) => {
            setCreated(a);
            select(a);
          }}
        />
      )}
      <CreatedDialog app={created} onClose={() => setCreated(null)} />
    </div>
  );
}

function AppView({
  projectId,
  apps,
  app,
  environment,
  onSelect,
  onCreated,
}: {
  projectId: string;
  apps: VerifyApp[];
  app: VerifyApp;
  environment: VerifyEnvironment;
  onSelect: (app: VerifyApp) => void;
  onCreated: (app: CreatedVerifyApp) => void;
}) {
  const props = { projectId, app, environment };
  return (
    <>
      <AppBar
        projectId={projectId}
        apps={apps}
        app={app}
        onSelect={onSelect}
        onCreated={onCreated}
      />
      <Stats {...props} />
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,26rem)_minmax(0,1fr)]">
        {/* Codes belong to one environment, so switching starts over. */}
        <TryIt key={environment} {...props} />
        <MessageCard projectId={projectId} app={app} />
      </div>
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,26rem)_minmax(0,1fr)]">
        <FailoverCard projectId={projectId} app={app} />
        <FraudCard projectId={projectId} app={app} />
      </div>
      <Blocks {...props} />
      <WidgetCard projectId={projectId} app={app} />
      <Recent {...props} />
      <Integrate app={app} />
    </>
  );
}
