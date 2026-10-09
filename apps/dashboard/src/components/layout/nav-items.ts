import {
  Analytics01Icon,
  BookOpen01Icon,
  ChartHistogramIcon,
  CloudServerIcon,
  CreditCardIcon,
  DashboardSquare01Icon,
  Key01Icon,
  LeftToRightListBulletIcon,
  Message01Icon,
  PasswordValidationIcon,
  PlugSocketIcon,
  SecurityCheckIcon,
  SentIcon,
  ServerStack01Icon,
  Settings02Icon,
  SmartPhone01Icon,
  TestTube01Icon,
  UserCircleIcon,
  UserGroupIcon,
  WebhookIcon,
  WorkflowSquare03Icon,
} from '@hugeicons/core-free-icons';

export type NavItem = {
  href: string;
  label: string;
  icon: typeof DashboardSquare01Icon;
  /** Match only the exact path (for index routes). */
  exact?: boolean;
  /** Planned for a later milestone; rendered disabled. */
  soon?: string;
  external?: boolean;
};
export type NavGroup = { title: string; items: NavItem[] };

export type NavContext = {
  projectId?: string;
  apiUrl: string;
  /** Set on pages outside a project (account, team, billing): the workspace links. */
  workspace?: {
    organizationId?: string;
    hosted: boolean;
    admin: boolean;
    operator: boolean;
  };
};

export function buildNav({ projectId, apiUrl, workspace }: NavContext): NavGroup[] {
  const groups: NavGroup[] = [];
  if (projectId) {
    const base = `/projects/${projectId}`;
    groups.push({
      title: 'Project',
      items: [
        { href: base, label: 'Overview', icon: DashboardSquare01Icon, exact: true },
        { href: `${base}/messages`, label: 'Messages', icon: Message01Icon },
        { href: `${base}/send`, label: 'Send', icon: SentIcon },
        { href: `${base}/verify`, label: 'Verify', icon: PasswordValidationIcon },
        { href: `${base}/automation`, label: 'Automation', icon: WorkflowSquare03Icon },
      ],
    });
    groups.push({
      title: 'Sending',
      items: [
        { href: `${base}/devices`, label: 'Phones', icon: SmartPhone01Icon },
        { href: `${base}/providers`, label: 'Providers', icon: CloudServerIcon },
      ],
    });
    groups.push({
      title: 'Developers',
      items: [
        { href: `${base}/api-keys`, label: 'API keys', icon: Key01Icon },
        { href: `${base}/webhooks`, label: 'Webhooks', icon: WebhookIcon },
        { href: `${base}/integrations`, label: 'Integrations', icon: PlugSocketIcon },
        { href: `${base}/playground`, label: 'Playground', icon: TestTube01Icon },
        { href: `${base}/logs`, label: 'Logs', icon: LeftToRightListBulletIcon },
        { href: `${apiUrl}/docs`, label: 'API reference', icon: BookOpen01Icon, external: true },
      ],
    });
    groups.push({
      title: 'Manage',
      items: [
        { href: `${base}/usage`, label: 'Usage', icon: ChartHistogramIcon },
        { href: `${base}/settings`, label: 'Settings', icon: Settings02Icon },
      ],
    });
  } else {
    groups.push({
      title: 'Developers',
      items: [
        { href: `${apiUrl}/docs`, label: 'API reference', icon: BookOpen01Icon, external: true },
      ],
    });
  }
  // Off a project, the workspace pages (also in the account menu) get their own group.
  if (workspace) {
    const org = workspace.organizationId ? `/organizations/${workspace.organizationId}` : null;
    const items: NavItem[] = [{ href: '/account', label: 'Account', icon: UserCircleIcon }];
    if (org) {
      items.push({ href: `${org}/team`, label: 'Team', icon: UserGroupIcon });
      if (workspace.hosted)
        items.push({ href: `${org}/billing`, label: 'Billing', icon: CreditCardIcon });
      if (workspace.admin)
        items.push({ href: `${org}/audit`, label: 'Audit log', icon: SecurityCheckIcon });
    }
    // Operator-only pages; nobody else sees them (the routes 404 for them).
    if (workspace.operator)
      items.push(
        { href: '/system', label: 'System health', icon: ServerStack01Icon, exact: true },
        { href: '/system/insights', label: 'Insights', icon: Analytics01Icon },
      );
    groups.push({ title: 'Workspace', items });
  }
  return groups;
}

export function isActive(pathname: string, item: NavItem): boolean {
  if (item.exact) return pathname === item.href;
  return pathname === item.href || pathname.startsWith(`${item.href}/`);
}
