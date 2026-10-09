'use client';

import type { Billing, BillingPlan } from '@bridge/api-types';
import { CheckmarkCircle02Icon, LinkSquare02Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useQueryClient } from '@tanstack/react-query';
import { useParams, usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useEffect } from 'react';
import { toast } from 'sonner';
import { PageHeader } from '@/components/kit/page-header';
import { SectionCard } from '@/components/kit/section-card';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';
import { useCan, useOrganization } from '@/components/layout/console-context';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { showError } from '@/lib/errors';
import { formatDate } from '@/lib/format';
import { useBilling, useBillingMutations, usePlans } from '@/lib/queries';
import { cn } from '@/lib/utils';

type Limits = BillingPlan['limits'];

const RESOURCES: { key: keyof Limits; label: string; unit: string }[] = [
  { key: 'live_messages', label: 'Live SMS this month', unit: 'live SMS a month' },
  { key: 'phones', label: 'Phones', unit: 'phones' },
  { key: 'projects', label: 'Projects', unit: 'projects' },
  { key: 'members', label: 'Members', unit: 'members' },
];

const STATUS: Record<Billing['status'], { kind: StatusKind; label: string } | null> = {
  none: null,
  active: { kind: 'success', label: 'Active' },
  past_due: { kind: 'warning', label: 'Payment failing' },
  cancelled: { kind: 'neutral', label: 'Cancelled' },
  expired: { kind: 'neutral', label: 'Expired' },
  incomplete: { kind: 'warning', label: 'Payment incomplete' },
};

function price(p: BillingPlan) {
  if (p.price_cents === 0) return 'Free';
  const amount = new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: p.currency,
    minimumFractionDigits: p.price_cents % 100 === 0 ? 0 : 2,
  }).format(p.price_cents / 100);
  return `${amount}/month`;
}

function count(n: number) {
  return new Intl.NumberFormat().format(n);
}

/** One resource's usage against its limit, as a bar that warns near the limit. */
function Meter({ label, used, limit }: { label: string; used: number; limit: number | null }) {
  const ratio = limit === null ? 0 : limit === 0 ? 1 : Math.min(1, used / limit);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono tabular-nums">
          {count(used)}
          <span className="text-muted-foreground"> / {limit === null ? '∞' : count(limit)}</span>
        </span>
      </div>
      {/* The figures above carry the value; the bar is decoration. */}
      <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
        <div
          className={cn(
            'h-full rounded-full transition-[width]',
            ratio >= 1 ? 'bg-destructive' : ratio >= 0.8 ? 'bg-warning' : 'bg-primary',
          )}
          style={{ width: `${limit === null ? 0 : Math.max(ratio * 100, used > 0 ? 2 : 0)}%` }}
        />
      </div>
    </div>
  );
}

function PlanCard({
  plan,
  current,
  rank,
  currentRank,
  canBuy,
  blocked,
  freeNote,
  busy,
  onChoose,
}: {
  plan: BillingPlan;
  current: boolean;
  rank: number;
  currentRank: number;
  canBuy: boolean;
  /** Why plans cannot be changed right now, if they cannot. */
  blocked?: string;
  /** What the Free card says to a paying workspace. */
  freeNote: string;
  busy: boolean;
  onChoose: () => void;
}) {
  let action: React.ReactNode = null;
  if (current) {
    action = (
      <Button variant="outline" disabled className="w-full">
        Current plan
      </Button>
    );
  } else if (plan.price_cents === 0) {
    action = <p className="text-center text-xs text-muted-foreground">{freeNote}</p>;
  } else if (canBuy && plan.purchasable && blocked) {
    action = <p className="text-center text-xs text-muted-foreground">{blocked}</p>;
  } else if (canBuy && plan.purchasable) {
    action = (
      <Button
        className="w-full"
        variant={rank > currentRank ? 'default' : 'outline'}
        disabled={busy}
        onClick={onChoose}
      >
        {rank > currentRank ? `Upgrade to ${plan.name}` : `Switch to ${plan.name}`}
      </Button>
    );
  }

  return (
    <div
      className={cn(
        'flex flex-col gap-4 rounded-xl border bg-card p-5',
        current && 'border-primary/50 bg-primary/5',
      )}
    >
      <div className="flex flex-col gap-1">
        <span className="font-display text-base font-semibold">{plan.name}</span>
        <span className="text-2xl font-semibold tracking-tight">{price(plan)}</span>
      </div>
      <ul className="flex flex-1 flex-col gap-2 text-sm">
        {RESOURCES.map((r) => {
          const v = plan.limits[r.key];
          return (
            <li key={r.key} className="flex items-center gap-2">
              <HugeiconsIcon
                icon={CheckmarkCircle02Icon}
                strokeWidth={2}
                className="size-4 shrink-0 text-primary"
              />
              {v === null ? `Unlimited ${r.unit}` : `${count(v)} ${r.unit}`}
            </li>
          );
        })}
        <li className="flex items-center gap-2 text-muted-foreground">
          <HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={2} className="size-4 shrink-0" />
          Unlimited test messages
        </li>
      </ul>
      {action}
    </div>
  );
}

/** Dodo statuses on the return URL that mean the payment did not complete. */
const FAILED = new Set([
  'failed',
  'cancelled',
  'requires_payment_method',
  'requires_customer_action',
]);

/** One plain sentence about where the workspace's plan stands. */
function describe(b: Billing, ends: string | null): string {
  if (!b.enabled) return 'This server is self-hosted, so nothing is limited.';
  const resets = `Live SMS usage resets on ${formatDate(b.period_end)}.`;
  switch (b.status) {
    case 'past_due':
      return 'The last payment failed. Update the payment method in Manage billing to keep the plan.';
    case 'cancelled':
    case 'expired':
      return `The paid plan ended${ends ? ` on ${ends}` : ''}. ${resets}`;
    case 'incomplete':
      return `The last checkout did not finish, so nothing was charged. ${resets}`;
    case 'active':
      if (b.cancel_at_period_end && ends) {
        return `Set to cancel. The ${b.plan.name} plan stays until ${ends}, then the workspace moves to Free.`;
      }
      return ends ? `${price(b.plan)}, renews on ${ends}.` : resets;
    default:
      return resets;
  }
}

export default function BillingPage() {
  const { organizationId } = useParams<{ organizationId: string }>();
  const org = useOrganization();
  const isOwner = useCan('owner');
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const qc = useQueryClient();
  const billing = useBilling(organizationId);
  const plans = usePlans();
  const { checkout, portal, sync, resume } = useBillingMutations(organizationId);
  const returned = params.get('checkout');
  const changed = params.get('change') === 'requested';
  const dodoStatus = params.get('status');
  const subscriptionId = params.get('subscription_id');

  // Back from Dodo's checkout (Dodo appends subscription_id and status), or from
  // an in-place plan change. Nothing here claims success: the plan shown always
  // comes from Dodo, by the webhook or the sync below.
  // biome-ignore lint/correctness/useExhaustiveDependencies: runs once per return
  useEffect(() => {
    if (!returned && !changed) return;
    router.replace(pathname, { scroll: false });
    if (returned && dodoStatus && FAILED.has(dodoStatus)) {
      toast.error(
        'The payment did not go through, so nothing changed. Try again with another card.',
      );
      return;
    }
    if (changed) {
      toast.success('Plan change requested. It shows here once Dodo confirms the charge.');
    } else {
      toast.success('Thanks. Your plan updates here as soon as Dodo confirms the payment.');
      if (subscriptionId) sync.mutate(subscriptionId);
    }
    const timers = [3000, 8000, 15000].map((ms) =>
      setTimeout(
        () => qc.invalidateQueries({ queryKey: ['organizations', organizationId, 'billing'] }),
        ms,
      ),
    );
    return () => timers.forEach(clearTimeout);
  }, [returned, changed]);

  async function open(run: () => Promise<{ url: string }>) {
    try {
      const { url } = await run();
      window.location.assign(url);
    } catch (err) {
      showError(err);
    }
  }

  const b = billing.data;
  const list = plans.data?.data ?? [];
  const currentRank = list.findIndex((p) => p.id === b?.plan.id);
  const status = b ? STATUS[b.status] : null;
  const ends = b?.current_period_end ? formatDate(b.current_period_end) : null;
  const blocked =
    b?.status === 'past_due'
      ? 'Update the payment method in Manage billing to change plans.'
      : b?.cancel_at_period_end
        ? 'Choose Keep my plan to change plans.'
        : undefined;
  const freeNote =
    b?.cancel_at_period_end && ends
      ? `The workspace moves to Free on ${ends}.`
      : 'To move to Free, cancel in Manage billing. The paid plan stays until the period ends.';

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Billing"
        subtitle={`${org?.name ?? 'This workspace'}: plan, usage and payments. Limits apply to the whole workspace; test messages are always free.`}
        actions={
          b?.manage_billing && isOwner ? (
            <>
              {b.cancel_at_period_end && b.status === 'active' ? (
                <Button
                  disabled={resume.isPending}
                  onClick={() =>
                    resume.mutate(undefined, {
                      onSuccess: () => toast.success('Your plan will renew as usual.'),
                      onError: showError,
                    })
                  }
                >
                  Keep my plan
                </Button>
              ) : null}
              <Button
                variant="outline"
                disabled={portal.isPending}
                onClick={() => open(() => portal.mutateAsync())}
              >
                <HugeiconsIcon icon={LinkSquare02Icon} strokeWidth={2} />
                Manage billing
              </Button>
            </>
          ) : null
        }
      />

      {billing.isPending ? (
        <Skeleton className="h-48 w-full rounded-xl" />
      ) : billing.isError ? (
        <p className="text-sm text-destructive">{billing.error.message}</p>
      ) : b ? (
        <>
          <SectionCard
            title={
              <span className="flex flex-wrap items-center gap-3">
                {b.plan.name} plan
                {status ? <StatusBadge kind={status.kind}>{status.label}</StatusBadge> : null}
              </span>
            }
            description={describe(b, ends)}
          >
            <div className="grid gap-5 sm:grid-cols-2">
              {RESOURCES.map((r) => (
                <Meter
                  key={r.key}
                  label={r.label}
                  used={b.usage[r.key]}
                  limit={b.enabled ? b.plan.limits[r.key] : null}
                />
              ))}
            </div>
          </SectionCard>

          {b.enabled && list.length > 0 ? (
            <section className="flex flex-col gap-3">
              <h2 className="font-display text-sm font-semibold">Plans</h2>
              {!b.payments ? (
                <p className="text-sm text-muted-foreground">
                  Payments are not set up on this server yet, so plans cannot be changed here.
                </p>
              ) : !isOwner ? (
                <p className="text-sm text-muted-foreground">
                  Only workspace owners can change the plan.
                </p>
              ) : null}
              <div className="grid gap-4 md:grid-cols-3">
                {list.map((p, i) => (
                  <PlanCard
                    key={p.id}
                    plan={p}
                    current={p.id === b.plan.id}
                    rank={i}
                    currentRank={currentRank}
                    canBuy={isOwner && b.payments}
                    blocked={blocked}
                    freeNote={freeNote}
                    busy={checkout.isPending}
                    onChoose={() => open(() => checkout.mutateAsync(p.id as 'pro' | 'business'))}
                  />
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                Prices exclude tax, which is added at checkout. Payments are handled by Dodo
                Payments. Changing between paid plans is prorated.
              </p>
            </section>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
