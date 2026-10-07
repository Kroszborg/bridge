import {
  Activity01Icon,
  BookOpen01Icon,
  ChartHistogramIcon,
  DashboardSquare01Icon,
  Key01Icon,
  LeftToRightListBulletIcon,
  Message01Icon,
  SecurityCheckIcon,
  ServerStack01Icon,
  Settings02Icon,
  SmartPhone01Icon,
  TestTube01Icon,
  UserCircleIcon,
  UserGroupIcon,
  WebhookIcon,
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
  organizationId?: string;
  apiUrl: string;
  /** Admin or owner of the organization in view. */
  canAdmin: boolean;
  /** Operator of this Bridge instance. */
  operator: boolean;
};

export function buildNav({
  projectId,
  organizationId,
  apiUrl,
  canAdmin,
  operator,
}: NavContext): NavGroup[] {
  const groups: NavGroup[] = [];
  if (projectId) {
    const base = `/projects/${projectId}`;
    groups.push({
      title: 'Project',
      items: [
        { href: base, label: 'Overview', icon: DashboardSquare01Icon, exact: true },
        { href: `${base}/messages`, label: 'Messages', icon: Message01Icon },
        { href: `${base}/devices`, label: 'Devices', icon: SmartPhone01Icon },
        { href: `${base}/usage`, label: 'Usage', icon: ChartHistogramIcon },
        { href: `${base}/settings`, label: 'Settings', icon: Settings02Icon },
      ],
    });
    groups.push({
      title: 'Developers',
      items: [
        { href: `${base}/playground`, label: 'Playground', icon: TestTube01Icon },
        { href: `${base}/api-keys`, label: 'API keys', icon: Key01Icon },
        { href: `${base}/webhooks`, label: 'Webhooks', icon: WebhookIcon },
        { href: `${base}/logs`, label: 'Logs', icon: LeftToRightListBulletIcon },
      ],
    });
  }
  const workspace: NavItem[] = [];
  if (organizationId) {
    const org = `/organizations/${organizationId}`;
    workspace.push({ href: `${org}/team`, label: 'Team', icon: UserGroupIcon });
    if (canAdmin)
      workspace.push({ href: `${org}/audit`, label: 'Audit log', icon: SecurityCheckIcon });
  }
  workspace.push(
    { href: `${apiUrl}/docs`, label: 'API reference', icon: BookOpen01Icon, external: true },
    { href: '/account', label: 'Account', icon: UserCircleIcon },
  );
  groups.push({ title: 'Workspace', items: workspace });
  const instance: NavItem[] = [];
  if (operator) instance.push({ href: '/system', label: 'System health', icon: ServerStack01Icon });
  instance.push({ href: '/status', label: 'Status page', icon: Activity01Icon, external: true });
  groups.push({ title: 'Instance', items: instance });
  return groups;
}

export function isActive(pathname: string, item: NavItem): boolean {
  if (item.exact) return pathname === item.href;
  return pathname === item.href || pathname.startsWith(`${item.href}/`);
}
