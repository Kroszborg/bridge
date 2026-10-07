'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useProject } from '@/lib/queries';
import { useConsole, useProjectId } from './console-context';

const LABELS: Record<string, string> = {
  'api-keys': 'API keys',
  devices: 'Devices',
  messages: 'Messages',
  settings: 'Settings',
  webhooks: 'Webhooks',
  usage: 'Usage',
  logs: 'Logs',
  playground: 'Playground',
  verify: 'Verify',
  account: 'Account',
  team: 'Team',
  audit: 'Audit log',
  system: 'System health',
};

function ProjectCrumb({ projectId }: { projectId: string }) {
  const { data } = useProject(projectId);
  return <>{data?.name ?? 'Project'}</>;
}

/** "Organization / Project / Section", derived from the route. */
export function Breadcrumb() {
  const pathname = usePathname();
  const projectId = useProjectId();
  const { organizations } = useConsole();
  const { data: project } = useProject(projectId ?? '');
  const org = organizations.find((o) => o.id === project?.organization_id);

  const crumbs: { key: string; label: React.ReactNode; href?: string }[] = [];
  if (projectId) {
    if (org) crumbs.push({ key: 'org', label: org.name });
    crumbs.push({
      key: 'project',
      label: <ProjectCrumb projectId={projectId} />,
      href: `/projects/${projectId}`,
    });
    const [section, detail] = pathname.split('/').slice(3);
    const sectionLabel = section ? (LABELS[section] ?? section) : 'Overview';
    if (section && detail) {
      crumbs.push({
        key: 'section',
        label: sectionLabel,
        href: `/projects/${projectId}/${section}`,
      });
      crumbs.push({ key: 'detail', label: <span className="font-mono text-xs">{detail}</span> });
    } else {
      crumbs.push({ key: 'section', label: sectionLabel });
    }
  } else if (pathname.startsWith('/organizations/')) {
    const [, , orgId, section] = pathname.split('/');
    const o = organizations.find((x) => x.id === orgId);
    if (o) crumbs.push({ key: 'org', label: o.name });
    crumbs.push({ key: 'page', label: LABELS[section ?? ''] ?? section });
  } else {
    const seg = pathname.split('/')[1] ?? '';
    crumbs.push({ key: 'page', label: LABELS[seg] ?? seg });
  }

  return (
    <nav
      aria-label="Breadcrumb"
      className="flex min-w-0 items-center gap-2 whitespace-nowrap text-sm"
    >
      {crumbs.map((c, i) => {
        const last = i === crumbs.length - 1;
        return (
          <span
            key={c.key}
            className={
              last
                ? 'flex min-w-0 items-center gap-2'
                : 'hidden shrink-0 items-center gap-2 sm:flex'
            }
          >
            {i > 0 ? (
              <span className={last ? 'hidden text-border sm:inline' : 'text-border'}>/</span>
            ) : null}
            {last ? (
              <span className="truncate font-medium text-foreground">{c.label}</span>
            ) : c.href ? (
              <Link
                href={c.href}
                className="text-muted-foreground transition-colors hover:text-foreground"
              >
                {c.label}
              </Link>
            ) : (
              <span className="text-muted-foreground">{c.label}</span>
            )}
          </span>
        );
      })}
    </nav>
  );
}
