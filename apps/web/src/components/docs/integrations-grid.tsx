import Link from 'next/link';

type Tool = {
  name: string;
  kind: 'Auth' | 'SMS provider' | 'Workflows';
  /** A Simple Icons logo in public/logos (CC0). Tools without one are shown by name only. */
  logo?: string;
  body: string;
  href: string;
};

const TOOLS: Tool[] = [
  {
    name: 'Supabase',
    kind: 'Auth',
    logo: 'supabase',
    body: 'Phone sign-in codes through the Send SMS hook. No code to write.',
    href: '/docs/integrations/supabase/',
  },
  {
    name: 'Auth0',
    kind: 'Auth',
    logo: 'auth0',
    body: 'Passwordless and MFA codes from a custom phone provider Action.',
    href: '/docs/integrations/auth0/',
  },
  {
    name: 'Better Auth',
    kind: 'Auth',
    body: 'Send codes from the phone number plugin with a few lines of the SDK.',
    href: '/docs/integrations/better-auth/',
  },
  {
    name: 'Firebase',
    kind: 'Auth',
    logo: 'firebase',
    body: 'Verify phone numbers with Bridge Verify next to Firebase Auth.',
    href: '/docs/integrations/firebase-clerk/#firebase-authentication',
  },
  {
    name: 'Clerk',
    kind: 'Auth',
    logo: 'clerk',
    body: 'Deliver the SMS Clerk generates from its webhook.',
    href: '/docs/integrations/firebase-clerk/#clerk',
  },
  {
    name: 'Twilio',
    kind: 'SMS provider',
    logo: 'twilio',
    body: 'A fallback when no phone can send, or the main route.',
    href: '/docs/providers/#twilio',
  },
  {
    name: 'Vonage',
    kind: 'SMS provider',
    logo: 'vonage',
    body: 'Send through the Vonage SMS API, with delivery reports.',
    href: '/docs/providers/#vonage',
  },
  {
    name: 'MSG91',
    kind: 'SMS provider',
    body: 'DLT-registered templates for sending in India.',
    href: '/docs/providers/#msg91',
  },
  {
    name: 'Plivo',
    kind: 'SMS provider',
    body: 'Send through the Plivo Message API, with delivery reports.',
    href: '/docs/providers/#plivo',
  },
  {
    name: 'n8n',
    kind: 'Workflows',
    logo: 'n8n',
    body: 'Send SMS and codes with HTTP requests; start workflows from webhooks.',
    href: '/docs/integrations/no-code/',
  },
  {
    name: 'Zapier',
    kind: 'Workflows',
    logo: 'zapier',
    body: 'Send an SMS from any Zap, and react to deliveries and incoming SMS.',
    href: '/docs/integrations/no-code/',
  },
  {
    name: 'Make',
    kind: 'Workflows',
    logo: 'make',
    body: 'An HTTP module to send, and a custom webhook to receive events.',
    href: '/docs/integrations/no-code/',
  },
];

function Logo({ slug }: { slug: string }) {
  const mask = `url(/logos/${slug}.svg) center / contain no-repeat`;
  return (
    <span aria-hidden className="size-5 shrink-0 bg-current" style={{ mask, WebkitMask: mask }} />
  );
}

/** "Works with the tools you already use": every integration guide, with its logo. */
export function IntegrationsGrid() {
  return (
    <section aria-labelledby="works-with" className="not-prose mb-12">
      <h2 id="works-with" className="text-xl font-semibold tracking-tight text-fd-foreground">
        Works with the tools you already use
      </h2>
      <ul className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {TOOLS.map((tool) => (
          <li key={tool.name}>
            <Link
              href={tool.href}
              className="flex h-full flex-col gap-2 rounded-xl border p-4 transition-colors hover:border-fd-primary/50"
            >
              <span className="flex items-center gap-2.5">
                {tool.logo ? <Logo slug={tool.logo} /> : null}
                <span className="font-semibold text-fd-foreground">{tool.name}</span>
                <span className="ml-auto font-mono text-[0.7rem] text-fd-muted-foreground">
                  {tool.kind}
                </span>
              </span>
              <span className="text-sm leading-relaxed text-fd-muted-foreground">{tool.body}</span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
