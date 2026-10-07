'use client';

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { PageHeader } from '@/components/kit/page-header';
import { useCan, useProjectId } from '@/components/layout/console-context';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { AutoReplies } from './_components/auto-replies';
import { Forwarding } from './_components/forwarding';
import { OptOuts } from './_components/opt-outs';

const TABS = [
  { value: 'auto-replies', label: 'Auto-replies' },
  { value: 'opt-outs', label: 'Opt-outs' },
  { value: 'forwarding', label: 'Forwarding' },
] as const;
type Tab = (typeof TABS)[number]['value'];

export default function AutomationPage() {
  const projectId = useProjectId() ?? '';
  const canAdmin = useCan('admin');
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  // The tab lives in ?tab= so links and reloads keep it.
  const wanted = params.get('tab');
  const tab: Tab = TABS.some((t) => t.value === wanted) ? (wanted as Tab) : 'auto-replies';

  function select(next: string) {
    const q = new URLSearchParams(params.toString());
    if (next === 'auto-replies') q.delete('tab');
    else q.set('tab', next);
    const s = q.toString();
    router.replace(s ? `${pathname}?${s}` : pathname, { scroll: false });
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Automation"
        subtitle={`What Bridge does with incoming SMS: answer keywords, keep the opt-out list, and forward messages elsewhere.${canAdmin ? '' : ' Only admins can change these settings.'}`}
      />
      <Tabs value={tab} onValueChange={select} className="gap-5">
        <TabsList className="h-9 w-full sm:w-fit">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value} className="h-7 px-3">
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="auto-replies">
          <AutoReplies projectId={projectId} />
        </TabsContent>
        <TabsContent value="opt-outs">
          <OptOuts projectId={projectId} />
        </TabsContent>
        <TabsContent value="forwarding">
          <Forwarding projectId={projectId} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
