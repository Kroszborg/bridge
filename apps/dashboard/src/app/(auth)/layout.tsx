import { Wordmark } from '@/components/brand';
import { ThemeToggle } from '@/components/theme-toggle';

const lifecycle = [
  { time: '14:01:02.114', label: 'Created', detail: 'POST /v1/messages' },
  { time: '14:01:02.131', label: 'Queued', detail: 'outbound_sms · attempt 1' },
  { time: '14:01:03.402', label: 'Device accepted', detail: 'Pixel 7 · SIM 1' },
  { time: '14:01:05.880', label: 'Sent', detail: '1 segment · GSM-7' },
  { time: '14:01:07.215', label: 'Delivered', detail: 'carrier delivery report' },
];

function LifecycleCard() {
  return (
    <div className="w-full max-w-md rounded-xl border bg-card/80 p-5 backdrop-blur">
      <div className="mb-4 flex items-center justify-between">
        <span className="font-mono text-xs text-muted-foreground">msg_01ja8z3k5w…</span>
        <span className="inline-flex items-center gap-1.5 text-xs font-medium text-success">
          <span className="signal-live size-1.5 rounded-full bg-success" aria-hidden />
          Delivered
        </span>
      </div>
      <ol className="flex flex-col">
        {lifecycle.map((step, i) => (
          <li
            key={step.label}
            className="relative grid grid-cols-[6.5rem_1rem_1fr] items-start gap-3 pb-3 last:pb-0"
          >
            <time className="pt-px font-mono text-[0.7rem] text-faint">{step.time}</time>
            <span className="relative flex justify-center pt-1">
              <span className="z-10 size-2.5 rounded-full bg-primary" />
              {i < lifecycle.length - 1 ? (
                <span className="absolute top-3.5 -bottom-3 w-px bg-border" aria-hidden />
              ) : null}
            </span>
            <span className="min-w-0">
              <span className="block text-sm font-medium text-foreground">{step.label}</span>
              <span className="block truncate text-xs text-muted-foreground">{step.detail}</span>
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-1 flex-col lg:flex-row">
      {/* Brand panel: always dark, in both themes. */}
      <div className="dark relative hidden flex-col justify-between overflow-hidden border-r bg-background p-10 text-foreground lg:flex lg:w-1/2">
        <div
          aria-hidden
          className="pointer-events-none absolute -top-40 -left-40 size-[32rem] rounded-full opacity-[0.07] blur-3xl"
          style={{ background: 'var(--primary)' }}
        />
        <Wordmark subtitle="Open source" />
        <div className="relative flex flex-col gap-8">
          <div className="max-w-md">
            <h1 className="font-display text-4xl/tight font-bold tracking-tight">
              Send SMS through phones you already own.
            </h1>
            <p className="mt-3 text-sm/relaxed text-muted-foreground">
              Pair an Android phone, create an API key and send through its SIM. Move to a messaging
              provider later without changing your application code.
            </p>
          </div>
          <LifecycleCard />
        </div>
        <p className="text-xs text-faint">AGPL-3.0 · Self-host with Docker Compose</p>
      </div>

      <div className="flex items-center justify-between border-b px-6 py-4 lg:hidden">
        <Wordmark />
        <ThemeToggle />
      </div>

      <div className="relative flex flex-1 items-center justify-center bg-background px-6 py-12 sm:px-10">
        <div className="absolute top-4 right-4 hidden lg:block">
          <ThemeToggle />
        </div>
        <div className="w-full max-w-sm">{children}</div>
      </div>
    </div>
  );
}
