import { BridgeMark } from '@/components/brand';
import { ThemeToggle } from '@/components/theme-toggle';
import { AuthFooter } from './auth-footer';

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="relative flex min-h-dvh flex-1 flex-col bg-background">
      <div className="auth-glow" aria-hidden />
      <header className="relative flex items-center justify-between px-5 py-4 sm:px-8">
        <span className="flex items-center gap-2.5">
          <span className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground">
            <BridgeMark className="size-5" />
          </span>
          <span className="font-display text-[0.95rem] font-bold tracking-tight">Bridge</span>
        </span>
        <ThemeToggle />
      </header>

      <main className="relative flex flex-1 items-start justify-center px-5 pt-[8vh] pb-16 sm:items-center sm:pt-0">
        <div className="auth-enter w-full max-w-[25rem]">
          <div className="rounded-2xl border bg-card p-6 shadow-[0_24px_64px_-40px_rgb(0_0_0/0.5)] sm:p-8">
            {children}
          </div>
        </div>
      </main>

      <AuthFooter />
    </div>
  );
}
