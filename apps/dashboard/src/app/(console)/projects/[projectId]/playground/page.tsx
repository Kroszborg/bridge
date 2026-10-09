'use client';

import { PageHeader } from '@/components/kit/page-header';
import { useProjectId } from '@/components/layout/console-context';
import { SendOne } from '@/components/send-one';

export default function PlaygroundPage() {
  const projectId = useProjectId() ?? '';

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Playground"
        subtitle="Try the API from the dashboard and watch a message move. Test mode is simulated and free; live mode sends a real SMS from your phones. Each request comes with the same call in curl, TypeScript and the CLI."
      />
      <SendOne projectId={projectId} developer />
    </div>
  );
}
