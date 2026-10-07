'use client';

import type { UsagePeriod } from '@bridge/api-types';

import {
  CheckmarkCircle02Icon,
  Key01Icon,
  Message01Icon,
  PasswordValidationIcon,
  SmartPhone01Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import Link from 'next/link';
import { useEffect, useState } from 'react';
import { CodeBlock } from '@/components/kit/code-block';
import { CopyField } from '@/components/kit/copy-button';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { useConsole, useProjectId } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDate } from '@/lib/format';
import {
  useApiKeys,
  useDevices,
  useProject,
  useUsage,
  useVerificationStats,
  useWebhooks,
} from '@/lib/queries';
import { cn } from '@/lib/utils';

type Step = {
  title: string;
  body: React.ReactNode;
  icon: typeof Key01Icon;
  state: 'done' | 'todo' | 'soon';
  action?: React.ReactNode;
};

function StepRow({ step, index, expanded }: { step: Step; index: number; expanded: boolean }) {
  const done = step.state === 'done';
  const soon = step.state === 'soon';
  return (
    <li className="flex gap-4 py-4 first:pt-0 last:pb-0">
      <span
        className={cn(
          'grid size-8 shrink-0 place-items-center rounded-full border font-display text-xs font-semibold',
          done && 'border-primary/40 bg-primary/10 text-primary',
          soon && 'border-dashed text-faint',
          !done && !soon && 'text-foreground',
        )}
      >
        {done ? (
          <HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={2} className="size-4" />
        ) : (
          index + 1
        )}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className={cn('text-sm font-medium', soon && 'text-muted-foreground')}>
            {step.title}
          </h3>
          {soon ? (
            <span className="rounded-full border px-1.5 text-[0.6rem] font-semibold uppercase tracking-wider text-faint">
              Next milestone
            </span>
          ) : null}
        </div>
        {expanded ? (
          <>
            <div className="text-xs/relaxed text-muted-foreground">{step.body}</div>
            {step.action ? <div className="flex flex-wrap gap-2">{step.action}</div> : null}
          </>
        ) : null}
      </div>
    </li>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <dt className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </div>
  );
}

function Stat({
  label,
  value,
  footer,
  tone,
}: {
  label: string;
  value: React.ReactNode;
  footer: React.ReactNode;
  tone?: 'warning';
}) {
  return (
    <div data-slot="stat-card" className="flex flex-col gap-2 rounded-xl border bg-card p-5">
      <span className="text-[0.68rem] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="font-display text-3xl font-semibold tabular-nums tracking-tight">
        {value}
      </span>
      <span
        className={cn(
          'truncate text-xs text-muted-foreground',
          tone === 'warning' && 'text-warning',
        )}
      >
        {footer}
      </span>
    </div>
  );
}

/** Live-environment health for the last 24 hours. Every number answers "is delivery working?". */
function StatsRow({ day, online, total }: { day?: UsagePeriod; online?: number; total?: number }) {
  if (!day) {
    return (
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-28 rounded-xl" />
        ))}
      </div>
    );
  }
  const finished = day.delivered + day.sent + day.failed;
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Stat
        label="Messages · 24 h"
        value={day.total.toLocaleString()}
        footer={day.pending > 0 ? `${day.pending} in flight` : 'Live messages only'}
      />
      <Stat
        label="Success rate"
        value={day.success_rate == null ? '—' : `${(day.success_rate * 100).toFixed(1)}%`}
        footer={
          finished > 0 ? `${day.failed} failed of ${finished} finished` : 'No finished messages yet'
        }
        tone={day.success_rate != null && day.success_rate < 0.9 ? 'warning' : undefined}
      />
      <Stat
        label="Phones online"
        value={
          <>
            {online ?? 0}
            <span className="text-base font-medium text-muted-foreground"> / {total ?? 0}</span>
          </>
        }
        footer={
          total && online !== undefined && online < total
            ? `${total - online} offline`
            : 'All paired phones'
        }
        tone={total && online !== undefined && online < total ? 'warning' : undefined}
      />
      <Stat
        label="Avg. time to send"
        value={day.avg_send_seconds > 0 ? `${day.avg_send_seconds.toFixed(1)} s` : '—'}
        footer="From API request to the phone reporting it sent"
      />
    </div>
  );
}

export default function ProjectOverviewPage() {
  const projectId = useProjectId() ?? '';
  const { apiUrl, organizations } = useConsole();
  const project = useProject(projectId);
  const apiKeys = useApiKeys(projectId);
  const devices = useDevices(projectId);
  const usage = useUsage(projectId, 'live');
  const testUsage = useUsage(projectId, 'test');
  const webhooks = useWebhooks(projectId);
  const liveOtp = useVerificationStats(projectId, 'live');
  const testOtp = useVerificationStats(projectId, 'test');
  const verifications = (liveOtp.data?.total ?? 0) + (testOtp.data?.total ?? 0);
  const [showGuide, setShowGuide] = useState(false);

  useEffect(() => {
    // Remember the last project so "/" can return here.
    // biome-ignore lint/suspicious/noDocumentCookie: a plain preference cookie, not a session
    document.cookie = `bridge_last_project=${projectId}; path=/; max-age=31536000; samesite=lax`;
  }, [projectId]);

  const activeKeys = (apiKeys.data ?? []).filter((k) => k.status === 'active');
  const pairedDevices = (devices.data ?? []).filter((d) => d.status !== 'disabled');
  const onlineDevices = pairedDevices.filter((d) => d.status === 'online');
  const org = organizations.find((o) => o.id === project.data?.organization_id);

  const steps: Step[] = [
    {
      title: 'Create an API key',
      icon: Key01Icon,
      state: activeKeys.length > 0 ? 'done' : 'todo',
      body:
        activeKeys.length > 0
          ? `${activeKeys.length} active ${activeKeys.length === 1 ? 'key' : 'keys'}. Use a test key while developing; test keys never send real SMS.`
          : 'Your server authenticates to Bridge with a project API key. Use a test key while developing.',
      action: (
        <Button asChild size="sm" variant={activeKeys.length > 0 ? 'outline' : 'default'}>
          <Link href={`/projects/${projectId}/api-keys`}>
            {activeKeys.length > 0 ? 'Manage API keys' : 'Create an API key'}
          </Link>
        </Button>
      ),
    },
    {
      title: 'Check your key works',
      icon: Key01Icon,
      state: activeKeys.some((k) => k.last_used_at) ? 'done' : 'todo',
      body: (
        <div className="flex flex-col gap-2">
          <span>
            Call the whoami endpoint. It returns this project and the key&apos;s environment.
          </span>
          <CodeBlock
            language="shell"
            code={`curl ${apiUrl}/v1/whoami \\\n  -H "Authorization: Bearer $BRIDGE_API_KEY"`}
          />
        </div>
      ),
    },
    {
      title: 'Pair an Android phone',
      icon: SmartPhone01Icon,
      state: pairedDevices.length > 0 ? 'done' : 'todo',
      body:
        pairedDevices.length > 0
          ? `${pairedDevices.length} paired, ${onlineDevices.length} online right now.`
          : 'Install the Bridge gateway app, scan a pairing code from the Devices page, and the phone appears online here.',
      action: (
        <Button asChild size="sm" variant={pairedDevices.length > 0 ? 'outline' : 'default'}>
          <Link href={`/projects/${projectId}/devices`}>
            {pairedDevices.length > 0 ? 'View devices' : 'Pair a phone'}
          </Link>
        </Button>
      ),
    },
    {
      title: 'Send your first SMS',
      icon: Message01Icon,
      state:
        (usage.data?.last_30_days.total ?? 0) + (testUsage.data?.last_30_days.total ?? 0) > 0
          ? 'done'
          : 'todo',
      body: (
        <div className="flex flex-col gap-2">
          <span>
            Queue a message; Bridge picks an online phone and reports every status change. With a
            test key it is simulated end to end at no cost.
          </span>
          <CodeBlock
            language="shell"
            code={`curl ${apiUrl}/v1/messages \\\n  -H "Authorization: Bearer $BRIDGE_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"to": "+919876543210", "message": "Hello from Bridge"}'`}
          />
        </div>
      ),
      action: (
        <>
          <Button asChild size="sm">
            <Link href={`/projects/${projectId}/playground`}>Try it in the playground</Link>
          </Button>
          <Button asChild size="sm" variant="outline">
            <Link href={`/projects/${projectId}/messages`}>View messages</Link>
          </Button>
        </>
      ),
    },
    {
      title: 'Verify a phone number',
      icon: PasswordValidationIcon,
      state: verifications > 0 ? 'done' : 'todo',
      body:
        verifications > 0 ? (
          `${verifications} verification${verifications === 1 ? '' : 's'} in the last 30 days.`
        ) : (
          <div className="flex flex-col gap-2">
            <span>
              Sign-in and sign-up codes take two calls: Bridge generates, sends and checks the code.
              With a test key the response includes the code, so tests need no phone.
            </span>
            <CodeBlock
              language="shell"
              code={`curl ${apiUrl}/v1/otp \\\n  -H "Authorization: Bearer $BRIDGE_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"to": "+919876543210"}'`}
            />
          </div>
        ),
      action: (
        <Button asChild size="sm" variant={verifications > 0 ? 'outline' : 'default'}>
          <Link href={`/projects/${projectId}/verify`}>
            {verifications > 0 ? 'Open Verify' : 'Try it on the Verify page'}
          </Link>
        </Button>
      ),
    },
    {
      title: 'Get notified with a webhook',
      icon: Message01Icon,
      state: (webhooks.data?.length ?? 0) > 0 ? 'done' : 'todo',
      body:
        (webhooks.data?.length ?? 0) > 0
          ? `${webhooks.data?.length} endpoint${webhooks.data?.length === 1 ? '' : 's'} receiving signed events.`
          : 'Instead of polling, let Bridge POST delivery results and incoming SMS to your server. Locally, `bridgectl listen` forwards them to localhost.',
      action: (
        <Button
          asChild
          size="sm"
          variant={(webhooks.data?.length ?? 0) > 0 ? 'outline' : 'default'}
        >
          <Link href={`/projects/${projectId}/webhooks`}>
            {(webhooks.data?.length ?? 0) > 0 ? 'Manage webhooks' : 'Add an endpoint'}
          </Link>
        </Button>
      ),
    },
  ];
  const doneCount = steps.filter((st) => st.state === 'done').length;
  const nextIndex = steps.findIndex((st) => st.state !== 'done');
  const allDone = nextIndex === -1;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={project.data?.name ?? <Skeleton className="h-8 w-48" />}
        subtitle={org ? `${org.name} · created ${formatDate(project.data?.created_at)}` : ' '}
      />
      <StatsRow
        day={usage.data?.last_24_hours}
        online={usage.data?.devices_online}
        total={usage.data?.devices_total}
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <SectionCard
          title={allDone ? 'Setup complete' : 'Get started'}
          description={
            allDone
              ? 'Every step is done. Messages, phones and webhooks are ready.'
              : `${doneCount} of ${steps.length} done. From an empty project to a delivered SMS.`
          }
          action={
            allDone ? (
              <Button variant="ghost" size="sm" onClick={() => setShowGuide((v) => !v)}>
                {showGuide ? 'Hide steps' : 'Show steps'}
              </Button>
            ) : undefined
          }
        >
          <div
            className="mb-4 h-1.5 w-full overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-label="Setup progress"
            aria-valuemin={0}
            aria-valuemax={steps.length}
            aria-valuenow={doneCount}
          >
            <div
              className="h-full rounded-full bg-primary transition-[width] duration-500"
              style={{ width: `${(doneCount / steps.length) * 100}%` }}
            />
          </div>
          {!allDone || showGuide ? (
            <ol className="flex flex-col divide-y">
              {steps.map((st, i) => (
                <StepRow
                  key={st.title}
                  step={st}
                  index={i}
                  expanded={showGuide || i === nextIndex}
                />
              ))}
            </ol>
          ) : null}
        </SectionCard>
        <div className="flex flex-col gap-4">
          <SectionCard title="Project">
            <dl className="flex flex-col gap-4">
              <Detail label="Project ID">
                <CopyField value={projectId} />
              </Detail>
              <Detail label="API base URL">
                <CopyField value={`${apiUrl}/v1`} />
              </Detail>
              <Detail label="Organization">{org?.name ?? '—'}</Detail>
            </dl>
          </SectionCard>
          <SectionCard title="Live and test">
            <div className="flex flex-col gap-3 text-xs/relaxed text-muted-foreground">
              <p>
                <span className="font-mono font-medium text-foreground">bk_live_</span> keys send
                real SMS through your paired devices. Carrier and SIM limits apply.
              </p>
              <p>
                <span className="font-mono font-medium text-foreground">bk_test_</span> keys go
                through the same API and validation, but messages are simulated and never leave
                Bridge. They cost nothing.
              </p>
            </div>
          </SectionCard>
        </div>
      </div>
    </div>
  );
}
