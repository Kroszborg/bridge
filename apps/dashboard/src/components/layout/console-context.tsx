'use client';

import type { Organization, User } from '@bridge/api-types';
import { useParams } from 'next/navigation';
import { createContext, type ReactNode, useContext } from 'react';
import { useProject } from '@/lib/queries';

type ConsoleValue = {
  user: User;
  organizations: Organization[];
  /** Public API base URL, used in code samples and the API reference link. */
  apiUrl: string;
};

const ConsoleContext = createContext<ConsoleValue | null>(null);

export function ConsoleProvider({ value, children }: { value: ConsoleValue; children: ReactNode }) {
  return <ConsoleContext.Provider value={value}>{children}</ConsoleContext.Provider>;
}

export function useConsole(): ConsoleValue {
  const v = useContext(ConsoleContext);
  if (!v) throw new Error('useConsole must be used inside the console layout');
  return v;
}

/** The project in the current URL, if any. */
export function useProjectId(): string | undefined {
  const params = useParams<{ projectId?: string }>();
  return params.projectId;
}

/** The organization in view: from the URL, else the current project's, else the first. */
export function useOrganization(): Organization | undefined {
  const params = useParams<{ organizationId?: string }>();
  const { organizations } = useConsole();
  const projectId = useProjectId();
  const { data: project } = useProject(projectId ?? '');
  const id = params.organizationId ?? project?.organization_id;
  return organizations.find((o) => o.id === id) ?? (id ? undefined : organizations[0]);
}

const RANK = { member: 1, admin: 2, owner: 3 } as const;

/** Whether the signed-in user has at least this role in the organization in view. */
export function useCan(role: 'admin' | 'owner'): boolean {
  const org = useOrganization();
  return org ? RANK[org.role] >= RANK[role] : false;
}
