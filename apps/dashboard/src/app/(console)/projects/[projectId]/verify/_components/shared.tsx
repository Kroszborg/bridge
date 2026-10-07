'use client';

import type { Verification, VerifyBlockReason } from '@bridge/api-types';
import { useSyncExternalStore } from 'react';
import { StatusBadge, type StatusKind } from '@/components/kit/status-badge';

export const pct = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 });
export const number = new Intl.NumberFormat('en');

export const STATUS: Record<
  Verification['status'],
  { kind: StatusKind; label: string; live?: boolean }
> = {
  pending: { kind: 'info', label: 'Pending', live: true },
  verified: { kind: 'success', label: 'Verified' },
  expired: { kind: 'neutral', label: 'Expired' },
  failed: { kind: 'danger', label: 'Too many attempts' },
  canceled: { kind: 'neutral', label: 'Replaced' },
};

export function VerificationStatus({ status }: { status: Verification['status'] }) {
  const s = STATUS[status];
  return (
    <StatusBadge kind={s.kind} live={s.live}>
      {s.label}
    </StatusBadge>
  );
}

/** Fraud protection refusals, in plain words. */
export const BLOCK_REASONS: Record<VerifyBlockReason, { label: string; hint: string }> = {
  country_not_allowed: {
    label: 'Country not allowed',
    hint: 'The number is in a country this app does not send to.',
  },
  ip_limit: {
    label: 'Too many from one IP address',
    hint: 'The per-IP hourly limit was reached.',
  },
  range_burst: {
    label: 'Too many to similar numbers',
    hint: 'Numbers that differ only in their last 3 digits hit the hourly limit.',
  },
  country_limit: {
    label: 'Country hourly cap reached',
    hint: 'The per-country hourly cap was reached.',
  },
  captcha_failed: {
    label: 'CAPTCHA failed',
    hint: 'The Turnstile check was missing or did not pass.',
  },
};

export function duration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)} s`;
  return `${Math.round(seconds / 6) / 10} min`;
}

const noSubscribe = () => () => {};

/** The dashboard's own origin, for widget and hosted page snippets. Empty during SSR. */
export function useDashboardOrigin(): string {
  return useSyncExternalStore(
    noSubscribe,
    () => window.location.origin,
    () => '',
  );
}

/** A number input value: digits only, as a string. */
export function digitsOnly(v: string): string {
  return v.replace(/\D/g, '').slice(0, 7);
}

export function FieldNote({
  id,
  error,
  children,
}: {
  id?: string;
  error?: string;
  children?: React.ReactNode;
}) {
  if (error) {
    return (
      <p id={id} className="text-xs text-destructive">
        {error}
      </p>
    );
  }
  if (!children) return null;
  return (
    <p id={id} className="text-xs/relaxed text-muted-foreground">
      {children}
    </p>
  );
}
