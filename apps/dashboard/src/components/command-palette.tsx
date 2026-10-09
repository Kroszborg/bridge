'use client';

import {
  Activity01Icon,
  BookOpen01Icon,
  ChartHistogramIcon,
  CloudServerIcon,
  CreditCardIcon,
  DashboardSquare01Icon,
  Folder01Icon,
  Key01Icon,
  LeftToRightListBulletIcon,
  Message01Icon,
  Moon02Icon,
  PasswordValidationIcon,
  PlugSocketIcon,
  Search01Icon,
  SecurityCheckIcon,
  SentIcon,
  Settings02Icon,
  SmartPhone01Icon,
  TestTube01Icon,
  TimeScheduleIcon,
  UserBlock01Icon,
  UserCircleIcon,
  UserGroupIcon,
  WebhookIcon,
  WorkflowSquare03Icon,
} from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { Command, defaultFilter } from 'cmdk';
import { useRouter } from 'next/navigation';
import { useTheme } from 'next-themes';
import { useEffect, useState } from 'react';
import {
  useCan,
  useConsole,
  useOrganization,
  useProjectId,
} from '@/components/layout/console-context';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { useProjectsByOrg } from '@/lib/queries';

type Item = {
  id: string;
  label: string;
  hint?: string;
  icon: typeof Key01Icon;
  run: () => void;
  keywords?: string[];
};

const itemClass =
  'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm text-foreground aria-selected:bg-muted data-[disabled=true]:opacity-50';

const SEP = ' | ';
const itemValue = (it: Item) => `${it.label}${SEP}${it.hint ?? ''} ${it.id}`;

/**
 * Label matches outrank everything else, so "aud" finds Audit log before a
 * project whose name merely contains those letters in order.
 */
function rank(value: string, search: string, keywords?: string[]) {
  const q = search.trim().toLowerCase();
  if (!q) return 1;
  const label = (value.split(SEP)[0] ?? '').toLowerCase();
  if (label.startsWith(q)) return 1;
  if (label.split(/\s+/).some((w) => w.startsWith(q))) return 0.9;
  if (keywords?.some((k) => k.toLowerCase().startsWith(q))) return 0.8;
  return defaultFilter(value, search, keywords) * 0.7;
}

/** ⌘K / Ctrl+K: jump to any page or project, or run a common action. */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const router = useRouter();
  const { organizations, apiUrl, user } = useConsole();
  const projectId = useProjectId();
  const org = useOrganization();
  const canAdmin = useCan('admin');
  const { resolvedTheme, setTheme } = useTheme();
  const projects = useProjectsByOrg(organizations);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    const onOpen = () => setOpen(true);
    document.addEventListener('keydown', onKey);
    window.addEventListener('bridge:command-palette', onOpen);
    return () => {
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('bridge:command-palette', onOpen);
    };
  }, []);

  const go = (href: string) => () => {
    setOpen(false);
    router.push(href);
  };
  const base = projectId ? `/projects/${projectId}` : null;

  const pages: Item[] = base
    ? [
        { id: 'overview', label: 'Overview', icon: DashboardSquare01Icon, run: go(base) },
        {
          id: 'messages',
          label: 'Messages',
          icon: Message01Icon,
          run: go(`${base}/messages`),
          keywords: ['sms', 'history'],
        },
        {
          id: 'send',
          label: 'Send',
          icon: SentIcon,
          run: go(`${base}/send`),
          keywords: ['sms', 'compose', 'new message'],
        },
        {
          id: 'schedules',
          label: 'Scheduled messages',
          icon: TimeScheduleIcon,
          run: go(`${base}/send?tab=scheduled`),
          keywords: ['schedule', 'recurring', 'reminder', 'later', 'daily', 'weekly'],
        },
        {
          id: 'automation',
          label: 'Automation',
          icon: WorkflowSquare03Icon,
          run: go(`${base}/automation`),
          keywords: ['auto-reply', 'autoreply', 'keyword', 'stop', 'forward', 'telegram', 'email'],
        },
        {
          id: 'opt-outs',
          label: 'Opt-out list',
          icon: UserBlock01Icon,
          run: go(`${base}/automation?tab=opt-outs`),
          keywords: ['opt-out', 'unsubscribe', 'stop', 'blocklist', 'suppression'],
        },
        {
          id: 'forwarding',
          label: 'Forwarding rules',
          icon: WebhookIcon,
          run: go(`${base}/automation?tab=forwarding`),
          keywords: ['forward', 'telegram', 'email', 'slack', 'discord', 'incoming'],
        },
        {
          id: 'verify',
          label: 'Verify',
          icon: PasswordValidationIcon,
          run: go(`${base}/verify`),
          keywords: ['otp', 'code', 'one-time password', '2fa'],
        },
        {
          id: 'devices',
          label: 'Phones',
          icon: SmartPhone01Icon,
          run: go(`${base}/devices`),
          keywords: ['devices', 'android', 'pair'],
        },
        {
          id: 'providers',
          label: 'Providers',
          icon: CloudServerIcon,
          run: go(`${base}/providers`),
          keywords: ['sms provider', 'twilio', 'msg91', 'vonage', 'plivo', 'routing', 'fallback'],
        },
        {
          id: 'integrations',
          label: 'Integrations',
          icon: PlugSocketIcon,
          run: go(`${base}/integrations`),
          keywords: ['supabase', 'auth', 'send sms hook', 'better auth', 'auth0'],
        },
        {
          id: 'usage',
          label: 'Usage',
          icon: ChartHistogramIcon,
          run: go(`${base}/usage`),
          keywords: ['charts', 'stats'],
        },
        {
          id: 'playground',
          label: 'Playground',
          icon: TestTube01Icon,
          run: go(`${base}/playground`),
          keywords: ['send', 'test'],
        },
        {
          id: 'api-keys',
          label: 'API keys',
          icon: Key01Icon,
          run: go(`${base}/api-keys`),
          keywords: ['token', 'secret'],
        },
        {
          id: 'webhooks',
          label: 'Webhooks',
          icon: WebhookIcon,
          run: go(`${base}/webhooks`),
          keywords: ['events', 'endpoint'],
        },
        {
          id: 'logs',
          label: 'Logs',
          icon: LeftToRightListBulletIcon,
          run: go(`${base}/logs`),
          keywords: ['requests', 'api'],
        },
        {
          id: 'settings',
          label: 'Project settings',
          icon: Settings02Icon,
          run: go(`${base}/settings`),
        },
      ]
    : [];
  const workspace: Item[] = [
    ...(org
      ? [
          {
            id: 'team',
            label: 'Team',
            hint: org.name,
            icon: UserGroupIcon,
            run: go(`/organizations/${org.id}/team`),
            keywords: ['members', 'invite'],
          },
          {
            id: 'billing',
            label: 'Billing',
            hint: org.name,
            icon: CreditCardIcon,
            run: go(`/organizations/${org.id}/billing`),
            keywords: ['plan', 'upgrade', 'subscription', 'invoice', 'usage', 'limits'],
          },
          ...(canAdmin
            ? [
                {
                  id: 'audit',
                  label: 'Audit log',
                  hint: org.name,
                  icon: SecurityCheckIcon,
                  run: go(`/organizations/${org.id}/audit`),
                  keywords: ['security', 'history'],
                },
              ]
            : []),
        ]
      : []),
    {
      id: 'account',
      label: 'Account',
      icon: UserCircleIcon,
      run: go('/account'),
      keywords: ['password', 'sessions', 'profile'],
    },
    ...(user.operator
      ? [{ id: 'system', label: 'System health', icon: Activity01Icon, run: go('/system') }]
      : []),
  ];
  const actions: Item[] = [
    ...(base
      ? [
          {
            id: 'a-send',
            label: 'Send a message',
            icon: SentIcon,
            run: go(`${base}/send`),
          },
          {
            id: 'a-broadcast',
            label: 'Send a bulk message',
            icon: SentIcon,
            run: go(`${base}/send?tab=bulk`),
            keywords: ['bulk', 'csv', 'campaign'],
          },
          {
            id: 'a-schedule',
            label: 'Schedule a message',
            icon: TimeScheduleIcon,
            run: go(`${base}/send?tab=scheduled`),
            keywords: ['recurring', 'reminder'],
          },
          {
            id: 'a-pair',
            label: 'Pair a phone',
            icon: SmartPhone01Icon,
            run: go(`${base}/devices`),
          },
        ]
      : []),
    ...(org && canAdmin
      ? [
          {
            id: 'a-invite',
            label: 'Invite people',
            icon: UserGroupIcon,
            run: go(`/organizations/${org.id}/team`),
          },
        ]
      : []),
    {
      id: 'a-theme',
      label: resolvedTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode',
      icon: Moon02Icon,
      run: () => {
        setTheme(resolvedTheme === 'dark' ? 'light' : 'dark');
        setOpen(false);
      },
      keywords: ['theme', 'appearance'],
    },
    {
      id: 'a-docs',
      label: 'Open the API reference',
      icon: BookOpen01Icon,
      run: () => {
        setOpen(false);
        window.open(`${apiUrl}/docs`, '_blank', 'noopener');
      },
    },
    {
      id: 'a-status',
      label: 'Open the status page',
      icon: Activity01Icon,
      run: () => {
        setOpen(false);
        window.open('/status', '_blank', 'noopener');
      },
    },
  ];

  const projectItems: Item[] = organizations.flatMap((o) =>
    (projects[o.id] ?? []).map((p) => ({
      id: `p-${p.id}`,
      label: p.name,
      hint: o.name,
      icon: Folder01Icon,
      run: go(`/projects/${p.id}`),
    })),
  );
  // cmdk 1.1 sorts items within a group but fails to reorder the groups
  // themselves, so the best-matching group is moved to the top here.
  const best = (items: Item[]) =>
    Math.max(0, ...items.map((it) => rank(itemValue(it), search, it.keywords)));
  const sections: [string, Item[]][] = [
    ['This project', pages],
    ['Actions', actions],
    ['Projects', projectItems],
    ['Workspace', workspace],
  ];
  if (search.trim()) sections.sort((a, b) => best(b[1]) - best(a[1]));

  const group = (heading: string, items: Item[]) =>
    items.length ? (
      <Command.Group
        key={heading}
        heading={heading}
        className="px-2 py-1.5 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-[0.62rem] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.14em] [&_[cmdk-group-heading]]:text-faint"
      >
        {items.map((it) => (
          <Command.Item
            key={it.id}
            value={itemValue(it)}
            keywords={it.keywords}
            onSelect={it.run}
            className={itemClass}
          >
            <HugeiconsIcon
              icon={it.icon}
              strokeWidth={2}
              className="size-4 shrink-0 text-muted-foreground"
            />
            <span className="flex-1 truncate">{it.label}</span>
            {it.hint ? (
              <span className="truncate text-xs text-muted-foreground">{it.hint}</span>
            ) : null}
          </Command.Item>
        ))}
      </Command.Group>
    ) : null;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSearch('');
      }}
    >
      <DialogContent
        className="top-[20%] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-xl"
        showClose={false}
      >
        <DialogTitle className="sr-only">Command palette</DialogTitle>
        <DialogDescription className="sr-only">
          Search pages, projects and actions.
        </DialogDescription>
        <Command loop filter={rank} className="flex flex-col">
          <div className="flex items-center gap-2 border-b px-4">
            <HugeiconsIcon
              icon={Search01Icon}
              strokeWidth={2}
              className="size-4 text-muted-foreground"
            />
            <Command.Input
              value={search}
              onValueChange={setSearch}
              placeholder="Search pages, projects and actions…"
              className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            <kbd className="rounded border px-1.5 py-0.5 font-mono text-[0.65rem] text-muted-foreground">
              Esc
            </kbd>
          </div>
          <Command.List className="scroll-slim max-h-[min(60vh,26rem)] overflow-y-auto py-2">
            <Command.Empty className="px-4 py-8 text-center text-sm text-muted-foreground">
              Nothing matches.
            </Command.Empty>
            {sections.map(([heading, items]) => group(heading, items))}
          </Command.List>
        </Command>
      </DialogContent>
    </Dialog>
  );
}

/** Topbar button that opens the palette and shows its shortcut. */
export function CommandTrigger() {
  const [mac, setMac] = useState(false);
  useEffect(() => setMac(/Mac|iPhone|iPad/.test(navigator.platform)), []);
  return (
    <button
      type="button"
      onClick={() => window.dispatchEvent(new Event('bridge:command-palette'))}
      className="hidden h-8 items-center gap-2 rounded-lg border bg-card px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground sm:flex"
      aria-label="Open the command palette"
    >
      <HugeiconsIcon icon={Search01Icon} strokeWidth={2} className="size-3.5" />
      <span className="pr-6">Search…</span>
      <kbd className="rounded border bg-background px-1 font-mono text-[0.65rem]">
        {mac ? '⌘' : 'Ctrl'} K
      </kbd>
    </button>
  );
}
