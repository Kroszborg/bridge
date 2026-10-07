'use client';

import Link from 'next/link';
import { useState } from 'react';
import { PageHeader } from '@/components/kit/page-header';
import { useProjectId } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import type { Environment } from '@/lib/queries';
import { BroadcastDialog, BroadcastList } from './_components/broadcasts';
import { Composer } from './_components/composer';

export default function SendPage() {
  const projectId = useProjectId() ?? '';
  const [listEnvironment, setListEnvironment] = useState<Environment>('test');
  const [open, setOpen] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Send"
        subtitle="Send one message to a list of numbers from a CSV. Bridge spreads the messages over your phones' send limits and skips numbers that opted out."
        actions={
          <Button asChild variant="outline">
            <Link href={`/projects/${projectId}/playground`}>Send one message</Link>
          </Button>
        }
      />
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
    </div>
  );
}
