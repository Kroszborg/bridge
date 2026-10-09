'use client';

import { PlusSignIcon, Tick02Icon, UnfoldMoreIcon } from '@hugeicons/core-free-icons';
import { HugeiconsIcon } from '@hugeicons/react';
import { useRouter } from 'next/navigation';
import { useState } from 'react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useProjectsByOrg } from '@/lib/queries';
import { useConsole, useNavProjectId } from './console-context';
import { CreateOrganizationDialog, CreateProjectDialog } from './create-dialogs';

function initial(name: string) {
  return (name.trim()[0] ?? '?').toUpperCase();
}

/** Organization / project switcher at the top of the rail. */
export function ProjectSwitcher() {
  const router = useRouter();
  const { organizations } = useConsole();
  const projectId = useNavProjectId();
  const projectsByOrg = useProjectsByOrg(organizations);
  const [newProjectOrg, setNewProjectOrg] = useState<string | null>(null);
  const [newOrgOpen, setNewOrgOpen] = useState(false);

  let currentOrg = organizations[0];
  let currentProject = undefined as { id: string; name: string } | undefined;
  for (const org of organizations) {
    const p = projectsByOrg[org.id]?.find((x) => x.id === projectId);
    if (p) {
      currentOrg = org;
      currentProject = p;
    }
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger className="mx-3 mb-1 flex items-center gap-2.5 rounded-lg border bg-background px-2.5 py-2 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <span className="grid size-6 shrink-0 place-items-center rounded-md bg-muted font-display text-[0.7rem] font-bold text-muted-foreground">
            {initial(currentOrg?.name ?? '?')}
          </span>
          <span className="min-w-0 flex-1 leading-tight">
            <span className="block truncate text-[0.68rem] text-muted-foreground">
              {currentOrg?.name}
            </span>
            <span className="block truncate text-sm font-medium">
              {currentProject?.name ?? 'Select a project'}
            </span>
          </span>
          <HugeiconsIcon
            icon={UnfoldMoreIcon}
            strokeWidth={2}
            className="size-4 shrink-0 text-faint"
          />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          {organizations.map((org, i) => (
            <DropdownMenuGroup key={org.id}>
              {i > 0 ? <DropdownMenuSeparator /> : null}
              <DropdownMenuLabel className="text-[0.62rem] font-semibold uppercase tracking-[0.14em] text-faint">
                {org.name}
              </DropdownMenuLabel>
              {(projectsByOrg[org.id] ?? []).map((p) => (
                <DropdownMenuItem key={p.id} onSelect={() => router.push(`/projects/${p.id}`)}>
                  <span className="flex-1 truncate">{p.name}</span>
                  {p.id === projectId ? (
                    <HugeiconsIcon
                      icon={Tick02Icon}
                      strokeWidth={2}
                      className="size-4 text-primary"
                    />
                  ) : null}
                </DropdownMenuItem>
              ))}
              {org.role !== 'member' ? (
                <DropdownMenuItem
                  onSelect={() => setNewProjectOrg(org.id)}
                  className="text-muted-foreground"
                >
                  <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} className="size-4" />
                  New project
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuGroup>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setNewOrgOpen(true)} className="text-muted-foreground">
            <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} className="size-4" />
            New organization
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateProjectDialog
        organizationId={newProjectOrg}
        onOpenChange={(open) => !open && setNewProjectOrg(null)}
      />
      <CreateOrganizationDialog open={newOrgOpen} onOpenChange={setNewOrgOpen} />
    </>
  );
}
