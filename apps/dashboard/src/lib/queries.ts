'use client';

import type {
  ApiKey,
  Message,
  Organization,
  OtpSettingsInput,
  Project,
  WebhookEventType,
} from '@bridge/api-types';
import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { useSyncExternalStore } from 'react';
import { api, unwrap } from './api';

export const keys = {
  projects: (orgId: string) => ['organizations', orgId, 'projects'] as const,
  project: (projectId: string) => ['projects', projectId] as const,
  apiKeys: (projectId: string) => ['projects', projectId, 'api-keys'] as const,
  devices: (projectId: string) => ['projects', projectId, 'devices'] as const,
  messages: (projectId: string) => ['projects', projectId, 'messages'] as const,
  usage: (projectId: string, env: string) => ['projects', projectId, 'usage', env] as const,
  webhooks: (projectId: string) => ['projects', projectId, 'webhooks'] as const,
};

export function useProject(projectId: string) {
  return useQuery({
    queryKey: keys.project(projectId),
    queryFn: () => unwrap(api.GET('/v1/projects/{projectId}', { params: { path: { projectId } } })),
    enabled: projectId !== '',
  });
}

/** Projects for every organization, keyed by organization ID. */
export function useProjectsByOrg(orgs: Organization[]) {
  return useQueries({
    queries: orgs.map((org) => ({
      queryKey: keys.projects(org.id),
      queryFn: () =>
        unwrap(
          api.GET('/v1/organizations/{organizationId}/projects', {
            params: { path: { organizationId: org.id } },
          }),
        ).then((r) => r.data),
    })),
    combine: (results) =>
      Object.fromEntries(orgs.map((org, i) => [org.id, results[i]?.data ?? []])) as Record<
        string,
        Project[]
      >,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ organizationId, name }: { organizationId: string; name: string }) =>
      unwrap(
        api.POST('/v1/organizations/{organizationId}/projects', {
          params: { path: { organizationId } },
          body: { name },
        }),
      ),
    onSuccess: (project) =>
      qc.invalidateQueries({ queryKey: keys.projects(project.organization_id) }),
  });
}

export function useRenameProject(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}', { params: { path: { projectId } }, body: { name } }),
      ),
    onSuccess: (project) => {
      qc.setQueryData(keys.project(projectId), project);
      qc.invalidateQueries({ queryKey: keys.projects(project.organization_id) });
    },
  });
}

export function useCreateOrganization() {
  return useMutation({
    mutationFn: (name: string) => unwrap(api.POST('/v1/organizations', { body: { name } })),
  });
}

export function useApiKeys(projectId: string) {
  return useQuery({
    queryKey: keys.apiKeys(projectId),
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/api-keys', { params: { path: { projectId } } }),
      ).then((r) => r.data),
  });
}

export type NewApiKey = {
  name: string;
  environment: ApiKey['environment'];
  expires_in_days?: number;
};

export function useCreateApiKey(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: NewApiKey) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/api-keys', { params: { path: { projectId } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.apiKeys(projectId) }),
  });
}

export function useRevokeApiKey(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (keyId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/api-keys/{keyId}', {
          params: { path: { projectId, keyId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.apiKeys(projectId) }),
  });
}

/** Devices refresh every 10s so presence stays current; faster while pairing. */
export function useDevices(projectId: string, refetchInterval = 10_000) {
  return useQuery({
    queryKey: keys.devices(projectId),
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/devices', { params: { path: { projectId } } })).then(
        (r) => r.data,
      ),
    refetchInterval,
  });
}

export function useCreatePairingToken(projectId: string) {
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/v1/projects/{projectId}/pairing-tokens', { params: { path: { projectId } } }),
      ),
  });
}

export function useRenameDevice(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ deviceId, name }: { deviceId: string; name: string }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/devices/{deviceId}', {
          params: { path: { projectId, deviceId } },
          body: { name },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices(projectId) }),
  });
}

export function useRemoveDevice(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (deviceId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/devices/{deviceId}', {
          params: { path: { projectId, deviceId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices(projectId) }),
  });
}

export function useWakeDevice(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (deviceId: string) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/devices/{deviceId}/wake', {
          params: { path: { projectId, deviceId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices(projectId) }),
  });
}

export type DeviceSettings = {
  name?: string;
  preferred_sim_slot?: number;
  send_limit_count?: number;
  forward_inbound?: boolean;
};

export function useUpdateDevice(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ deviceId, ...body }: DeviceSettings & { deviceId: string }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/devices/{deviceId}', {
          params: { path: { projectId, deviceId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices(projectId) }),
  });
}

export function useTestSend(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ deviceId, to, message }: { deviceId: string; to: string; message: string }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/devices/{deviceId}/test', {
          params: { path: { projectId, deviceId } },
          body: { to, message },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.messages(projectId) }),
  });
}

export type MessageFilters = {
  environment: 'live' | 'test';
  direction?: Message['direction'];
  status?: Message['status'];
  to?: string;
  from?: string;
  device_id?: string;
};

/** Paged message history; refreshes every 4s while anything is still in flight. */
export function useMessages(projectId: string, filters: MessageFilters) {
  return useInfiniteQuery({
    queryKey: [...keys.messages(projectId), filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/messages', {
          params: {
            path: { projectId },
            query: { ...filters, limit: 25, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: (q) =>
      q.state.data?.pages.some((p) =>
        p.data.some((m) => ['created', 'queued', 'sending', 'sent'].includes(m.status)),
      )
        ? 4_000
        : 15_000,
  });
}

export function useMessage(projectId: string, messageId: string | null) {
  return useQuery({
    queryKey: [...keys.messages(projectId), 'detail', messageId],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/messages/{messageId}', {
          params: { path: { projectId, messageId: messageId ?? '' } },
        }),
      ),
    enabled: messageId !== null,
    refetchInterval: (q) =>
      q.state.data && ['created', 'queued', 'sending', 'sent'].includes(q.state.data.status)
        ? 2_000
        : false,
  });
}

export function useUsage(projectId: string, environment: 'live' | 'test' = 'live') {
  return useQuery({
    queryKey: keys.usage(projectId, environment),
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/usage', {
          params: { path: { projectId }, query: { environment } },
        }),
      ),
    refetchInterval: 15_000,
  });
}

export function useWebhooks(projectId: string) {
  return useQuery({
    queryKey: keys.webhooks(projectId),
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/webhooks', { params: { path: { projectId } } })),
    refetchInterval: 15_000,
  });
}

export function useWebhook(projectId: string, webhookId: string) {
  return useQuery({
    queryKey: [...keys.webhooks(projectId), webhookId],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/webhooks/{webhookId}', {
          params: { path: { projectId, webhookId } },
        }),
      ),
    refetchInterval: 10_000,
  });
}

export type WebhookInput = {
  url?: string;
  description?: string;
  events?: WebhookEventType[];
  enabled?: boolean;
};

export function useCreateWebhook(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WebhookInput & { url: string }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/webhooks', { params: { path: { projectId } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.webhooks(projectId) }),
  });
}

export function useUpdateWebhook(projectId: string, webhookId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WebhookInput) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/webhooks/{webhookId}', {
          params: { path: { projectId, webhookId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.webhooks(projectId) }),
  });
}

export function useDeleteWebhook(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (webhookId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/webhooks/{webhookId}', {
          params: { path: { projectId, webhookId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.webhooks(projectId) }),
  });
}

export function useWebhookSecret(projectId: string, webhookId: string) {
  return useMutation({
    mutationFn: (action: 'reveal' | 'rotate') =>
      action === 'reveal'
        ? unwrap(
            api.GET('/v1/projects/{projectId}/webhooks/{webhookId}/secret', {
              params: { path: { projectId, webhookId } },
            }),
          )
        : unwrap(
            api.POST('/v1/projects/{projectId}/webhooks/{webhookId}/rotate-secret', {
              params: { path: { projectId, webhookId } },
            }),
          ),
  });
}

export function useTestWebhook(projectId: string, webhookId: string) {
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/v1/projects/{projectId}/webhooks/{webhookId}/test', {
          params: { path: { projectId, webhookId } },
        }),
      ),
  });
}

/** The delivery log refreshes every 3s so test events and retries show up quickly. */
export function useWebhookDeliveries(projectId: string, webhookId: string) {
  return useQuery({
    queryKey: [...keys.webhooks(projectId), webhookId, 'deliveries'],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/webhooks/{webhookId}/deliveries', {
          params: { path: { projectId, webhookId }, query: { limit: 100 } },
        }),
      ),
    refetchInterval: 3_000,
  });
}

function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

const noSubscribe = () => () => {};

/**
 * The browser's IANA time zone, for day boundaries in usage reports. Null
 * during server rendering, so the server and first client render agree.
 */
export function useBrowserTimeZone(): string | null {
  return useSyncExternalStore(noSubscribe, browserTimeZone, () => null);
}

export function useUsageHistory(projectId: string, environment: 'live' | 'test', days: number) {
  const tz = useBrowserTimeZone();
  return useQuery({
    queryKey: [...keys.usage(projectId, environment), 'history', days, tz],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/usage/history', {
          params: { path: { projectId }, query: { environment, days, tz: tz ?? 'UTC' } },
        }),
      ),
    enabled: tz !== null,
    // Keep the previous render while a new range loads: no skeleton flash.
    placeholderData: (prev) => prev,
    refetchInterval: 60_000,
  });
}

export type RequestLogFilters = {
  environment: 'live' | 'test';
  status?: 'success' | 'error' | '2xx' | '4xx' | '5xx';
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  api_key_id?: string;
  path?: string;
};

export function useRequestLogs(projectId: string, filters: RequestLogFilters) {
  return useInfiniteQuery({
    queryKey: ['projects', projectId, 'request-logs', filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/request-logs', {
          params: {
            path: { projectId },
            query: { ...filters, limit: 50, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: 5_000,
  });
}

export type PlaygroundSend = {
  environment: 'live' | 'test';
  idempotencyKey?: string;
  body: {
    to: string;
    message: string;
    device_id?: string;
    sim_slot?: number;
    metadata?: Record<string, unknown>;
  };
};

export function usePlaygroundSend(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ environment, idempotencyKey, body }: PlaygroundSend) => {
      const request = api.POST('/v1/projects/{projectId}/messages', {
        params: {
          path: { projectId },
          query: { environment },
          header: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : {},
        },
        body,
      });
      const message = await unwrap(request); // throws BridgeApiError on failure
      const { response } = await request;
      return {
        message,
        status: response.status,
        replayed: response.headers.get('idempotent-replayed') === 'true',
      };
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.messages(projectId) }),
  });
}

// ---- organizations, members, invites, audit -------------------------------

export function useMembers(organizationId: string) {
  return useQuery({
    queryKey: ['organizations', organizationId, 'members'],
    queryFn: () =>
      unwrap(
        api.GET('/v1/organizations/{organizationId}/members', {
          params: { path: { organizationId } },
        }),
      ),
    enabled: organizationId !== '',
  });
}

export function useInvites(organizationId: string, enabled = true) {
  return useQuery({
    queryKey: ['organizations', organizationId, 'invites'],
    queryFn: () =>
      unwrap(
        api.GET('/v1/organizations/{organizationId}/invites', {
          params: { path: { organizationId } },
        }),
      ),
    enabled: enabled && organizationId !== '',
  });
}

export function useTeamMutations(organizationId: string) {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ['organizations', organizationId] });
  const path = { organizationId };
  return {
    createInvite: useMutation({
      mutationFn: (body: { role: 'owner' | 'admin' | 'member'; email?: string }) =>
        unwrap(api.POST('/v1/organizations/{organizationId}/invites', { params: { path }, body })),
      onSuccess: refresh,
    }),
    revokeInvite: useMutation({
      mutationFn: (inviteId: string) =>
        unwrap(
          api.DELETE('/v1/organizations/{organizationId}/invites/{inviteId}', {
            params: { path: { ...path, inviteId } },
          }),
        ),
      onSuccess: refresh,
    }),
    updateRole: useMutation({
      mutationFn: ({ memberId, role }: { memberId: string; role: 'owner' | 'admin' | 'member' }) =>
        unwrap(
          api.PATCH('/v1/organizations/{organizationId}/members/{memberId}', {
            params: { path: { ...path, memberId } },
            body: { role },
          }),
        ),
      onSuccess: refresh,
    }),
    removeMember: useMutation({
      mutationFn: (memberId: string) =>
        unwrap(
          api.DELETE('/v1/organizations/{organizationId}/members/{memberId}', {
            params: { path: { ...path, memberId } },
          }),
        ),
      onSuccess: refresh,
    }),
    rename: useMutation({
      mutationFn: (name: string) =>
        unwrap(
          api.PATCH('/v1/organizations/{organizationId}', { params: { path }, body: { name } }),
        ),
      onSuccess: refresh,
    }),
  };
}

export type AuditFilters = { project_id?: string; action?: string; actor_id?: string };

export function useAuditLog(organizationId: string, filters: AuditFilters) {
  return useInfiniteQuery({
    queryKey: ['organizations', organizationId, 'audit', filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/organizations/{organizationId}/audit-logs', {
          params: {
            path: { organizationId },
            query: { ...filters, limit: 50, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
  });
}

export function useDeleteProject(projectId: string) {
  return useMutation({
    mutationFn: (confirm: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}', {
          params: { path: { projectId } },
          body: { confirm },
        }),
      ),
  });
}

// ---- account ----------------------------------------------------------------

export function useSessions() {
  return useQuery({
    queryKey: ['me', 'sessions'],
    queryFn: () => unwrap(api.GET('/v1/me/sessions')),
  });
}

export function useAccountMutations() {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ['me'] });
  return {
    updateName: useMutation({
      mutationFn: (name: string) => unwrap(api.PATCH('/v1/me', { body: { name } })),
    }),
    changePassword: useMutation({
      mutationFn: (body: { current_password: string; new_password: string }) =>
        unwrap(api.POST('/v1/me/password', { body })),
      onSuccess: refresh,
    }),
    revokeSession: useMutation({
      mutationFn: (sessionId: string) =>
        unwrap(api.DELETE('/v1/me/sessions/{sessionId}', { params: { path: { sessionId } } })),
      onSuccess: refresh,
    }),
    revokeOthers: useMutation({
      mutationFn: () => unwrap(api.POST('/v1/me/sessions/revoke-others')),
      onSuccess: refresh,
    }),
    deleteAccount: useMutation({
      mutationFn: (body: { password: string; confirm: string }) =>
        unwrap(api.DELETE('/v1/me', { body })),
    }),
  };
}

// ---- status -------------------------------------------------------------------

export function useStatusPage() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => unwrap(api.GET('/v1/status')),
    refetchInterval: 30_000,
  });
}

export function useSystemHealth() {
  return useQuery({
    queryKey: ['system'],
    queryFn: () => unwrap(api.GET('/v1/system')),
    refetchInterval: 10_000,
  });
}

// ---- verify (one-time passwords) ------------------------------------------

export type VerifyEnvironment = 'live' | 'test';

export type VerificationFilters = {
  environment: VerifyEnvironment;
  status?: 'pending' | 'verified' | 'expired' | 'failed' | 'canceled';
  to?: string;
};

export function useVerifications(projectId: string, filters: VerificationFilters) {
  return useInfiniteQuery({
    queryKey: ['projects', projectId, 'otp', filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/otp', {
          params: {
            path: { projectId },
            query: { ...filters, limit: 25, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: 5_000,
  });
}

export function useVerificationStats(projectId: string, environment: VerifyEnvironment) {
  return useQuery({
    queryKey: ['projects', projectId, 'otp', 'stats', environment],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/otp/stats', {
          params: { path: { projectId }, query: { environment } },
        }),
      ),
    refetchInterval: 15_000,
  });
}

export function useOtpSettings(projectId: string) {
  return useQuery({
    queryKey: ['projects', projectId, 'otp', 'settings'],
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/otp/settings', { params: { path: { projectId } } })),
  });
}

export function useUpdateOtpSettings(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: OtpSettingsInput) =>
      unwrap(
        api.PUT('/v1/projects/{projectId}/otp/settings', {
          params: { path: { projectId } },
          body,
        }),
      ),
    onSuccess: (data) => qc.setQueryData(['projects', projectId, 'otp', 'settings'], data),
  });
}

export function useOtpPlayground(projectId: string) {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ['projects', projectId, 'otp'] });
  const send = useMutation({
    mutationFn: ({ environment, to }: { environment: VerifyEnvironment; to: string }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/otp', {
          params: { path: { projectId }, query: { environment } },
          body: { to },
        }),
      ),
    onSuccess: refresh,
  });
  const verify = useMutation({
    mutationFn: ({
      environment,
      id,
      code,
    }: {
      environment: VerifyEnvironment;
      id: string;
      code: string;
    }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/otp/verify', {
          params: { path: { projectId }, query: { environment } },
          body: { id, code },
        }),
      ),
    onSuccess: refresh,
  });
  return { send, verify };
}
