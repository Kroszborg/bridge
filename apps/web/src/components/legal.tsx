import { Wordmark } from '@/components/brand';
import { CONTACT_EMAIL, MAKER, REPO_URL } from '@/lib/site';

/** A plain, readable page for the privacy policy and terms. */
export function LegalPage({
  title,
  updated,
  children,
}: {
  title: string;
  updated: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header>
        <div className="mx-auto flex max-w-3xl items-center justify-between px-4 py-4 sm:px-6">
          <a href="/" aria-label="Bridge home">
            <Wordmark />
          </a>
          <nav className="flex gap-5 text-sm text-muted-foreground">
            <a href="/privacy/" className="hover:text-foreground">
              Privacy
            </a>
            <a href="/terms/" className="hover:text-foreground">
              Terms
            </a>
          </nav>
        </div>
      </header>
      <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-12 sm:px-6 sm:py-16">
        <h1 className="font-display text-3xl font-bold tracking-tight sm:text-4xl">{title}</h1>
        <p className="mt-2 text-sm text-muted-foreground">Last updated {updated}</p>
        <div className="legal mt-10 flex flex-col gap-4 text-[0.95rem] leading-relaxed text-muted-foreground [&_a]:font-medium [&_a]:text-foreground [&_a]:underline [&_a]:decoration-border [&_a]:underline-offset-4 hover:[&_a]:decoration-primary [&_h2]:mt-6 [&_h2]:font-display [&_h2]:text-xl [&_h2]:font-semibold [&_h2]:text-foreground [&_li]:ml-5 [&_li]:list-disc [&_strong]:text-foreground [&_ul]:flex [&_ul]:flex-col [&_ul]:gap-2">
          {children}
        </div>
      </main>
      <footer className="border-t">
        <div className="mx-auto flex max-w-3xl flex-col gap-2 px-4 py-6 text-xs text-muted-foreground sm:flex-row sm:justify-between sm:px-6">
          <span>
            Bridge is open source: <a href={REPO_URL}>read the code</a>.
          </span>
          <span>
            Operated by <a href={MAKER.portfolio}>{MAKER.name}</a> ·{' '}
            <a href={`mailto:${CONTACT_EMAIL}`}>{CONTACT_EMAIL}</a>
          </span>
        </div>
      </footer>
    </div>
  );
}
