'use client';

import { CloudServerIcon, LockKeyIcon, SmartPhone01Icon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useId, useState } from 'react';

type Mode = 'phones' | 'phones_then_providers' | 'providers';

const providers = ['MSG91', 'Twilio', 'Vonage', 'Plivo'];

const modes: {
  id: Mode;
  label: string;
  steps: { kind: 'phones' | 'providers'; title: string; body: string }[];
}[] = [
  {
    id: 'phones',
    label: 'Phones only',
    steps: [
      {
        kind: 'phones',
        title: 'Your paired phones',
        body: 'Bridge picks an online phone under its send limit and retries on another if one fails. Nothing leaves your SIM cards.',
      },
    ],
  },
  {
    id: 'phones_then_providers',
    label: 'Phones, then providers',
    steps: [
      {
        kind: 'phones',
        title: 'Your paired phones first',
        body: 'A message waits up to 60 seconds for a phone that can send. You set the wait, from 0 to 3600 seconds.',
      },
      {
        kind: 'providers',
        title: 'Then your providers',
        body: 'If no phone takes it, the next enabled provider does, in the order you set. The timeline records why.',
      },
    ],
  },
  {
    id: 'providers',
    label: 'Providers only',
    steps: [
      {
        kind: 'providers',
        title: 'Your providers',
        body: 'Every message goes to a provider. Useful before you pair a phone, or for DLT traffic in India through MSG91.',
      },
    ],
  },
];

/** The routing setting of a project, as a small working control. */
export function RoutingModes() {
  const [mode, setMode] = useState<Mode>('phones_then_providers');
  const name = useId();
  const current = modes.find((m) => m.id === mode) ?? modes[1];
  if (!current) return null;
  return (
    <div className="overflow-hidden rounded-2xl border bg-card">
      <fieldset className="border-b p-4 sm:p-5">
        <legend className="sr-only">How this project sends messages</legend>
        <div className="grid gap-1 rounded-xl bg-muted p-1 sm:grid-cols-3">
          {modes.map((m) => (
            <label
              key={m.id}
              className="cursor-pointer rounded-lg px-3 py-2 text-center text-sm font-medium text-muted-foreground transition-colors hover:text-foreground has-[:checked]:bg-card has-[:checked]:text-foreground has-[:checked]:shadow-sm has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary"
            >
              <input
                type="radio"
                name={name}
                value={m.id}
                checked={mode === m.id}
                onChange={() => setMode(m.id)}
                className="sr-only"
              />
              {m.label}
            </label>
          ))}
        </div>
      </fieldset>

      <ol key={current.id} className="swap-in flex flex-col gap-0 p-5 sm:p-6" aria-live="polite">
        {current.steps.map((s, i) => (
          <li key={s.title} className="relative grid grid-cols-[2.5rem_1fr] gap-4 pb-6 last:pb-0">
            {i < current.steps.length - 1 ? (
              <span aria-hidden className="absolute top-11 bottom-1 left-5 w-px bg-border" />
            ) : null}
            <span className="grid size-10 place-items-center rounded-xl border bg-background text-primary">
              <HugeiconsIcon
                icon={s.kind === 'phones' ? SmartPhone01Icon : CloudServerIcon}
                strokeWidth={2}
                className="size-5"
              />
            </span>
            <div className="min-w-0">
              <h3 className="font-semibold">{s.title}</h3>
              <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{s.body}</p>
              {s.kind === 'providers' ? (
                <ol className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {providers.map((p, n) => (
                    <li
                      key={p}
                      className="flex items-baseline gap-2 rounded-lg border bg-background px-3 py-2 text-sm"
                    >
                      <span className="font-mono text-xs text-faint">{n + 1}</span>
                      <span className="font-medium">{p}</span>
                    </li>
                  ))}
                </ol>
              ) : null}
            </div>
          </li>
        ))}
      </ol>

      <p className="flex items-start gap-2.5 border-t bg-background/60 px-5 py-4 text-sm text-muted-foreground sm:px-6">
        <HugeiconsIcon icon={LockKeyIcon} strokeWidth={2} className="mt-0.5 size-4 shrink-0" />
        <span>
          Provider credentials are encrypted with AES-256-GCM under{' '}
          <code className="font-mono text-[0.8rem] text-foreground">BRIDGE_SECRET_KEY</code> before
          they reach the database.
        </span>
      </p>
    </div>
  );
}
