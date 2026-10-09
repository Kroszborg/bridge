'use client';

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useState } from 'react';
import { PageHeader } from '@/components/kit/page-header';
import { useProjectId } from '@/components/layout/console-context';
import { SendOne } from '@/components/send-one';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import type { Environment } from '@/lib/queries';
import { BroadcastDialog, BroadcastList } from './_components/broadcasts';
import { Composer } from './_components/composer';
import { Schedules } from './_components/schedules';

const TABS = [
  { value: 'one', label: 'One message' },
  { value: 'bulk', label: 'Bulk (CSV)' },
  { value: 'scheduled', label: 'Scheduled' },
] as const;
type Tab = (typeof TABS)[number]['value'];

export default function SendPage() {
  const projectId = useProjectId() ?? '';
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const [listEnvironment, setListEnvironment] = useState<Environment>('test');
  const [open, setOpen] = useState<string | null>(null);
  // The tab lives in ?tab= so links and reloads keep it.
  const wanted = params.get('tab');
  const tab: Tab = TABS.some((t) => t.value === wanted) ? (wanted as Tab) : 'one';

  function select(next: string) {
    const q = new URLSearchParams(params.toString());
    if (next === 'one') q.delete('tab');
    else q.set('tab', next);
    const s = q.toString();
    router.replace(s ? `${pathname}?${s}` : pathname, { scroll: false });
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Send"
        subtitle="Send SMS from your phones: one message now, a list of numbers from a CSV, or on a schedule."
      />
      <Tabs value={tab} onValueChange={select} className="gap-5">
        <TabsList className="h-9 w-full sm:w-fit">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value} className="h-7 px-3">
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="one">
          <SendOne projectId={projectId} />
        </TabsContent>
        <TabsContent value="bulk" className="flex flex-col gap-6">
          <p className="max-w-prose text-sm text-muted-foreground">
            Send one message to a list of numbers. Bridge spreads the messages over your phones'
            send limits and skips numbers that opted out.
          </p>
          <Composer
            projectId={projectId}
            onSent={(b) => {
              setListEnvironment(b.environment);
              setOpen(b.id);
            }}
          />
          <BroadcastList
            projectId={projectId}
            environment={listEnvironment}
            onEnvironmentChange={setListEnvironment}
            onOpen={setOpen}
          />
          <BroadcastDialog projectId={projectId} broadcastId={open} onClose={() => setOpen(null)} />
        </TabsContent>
        <TabsContent value="scheduled">
          <Schedules projectId={projectId} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
