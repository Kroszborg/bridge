'use client';

import type {
  ApiKey,
  AutoReplyRule,
  AutoReplyRuleCreateInput,
  AutoReplyRuleUpdateInput,
  Broadcast,
  BroadcastCreateInput,
  BroadcastPreview,
  BroadcastStatus,
  components,
  ErrorBody,
  ForwardingRuleCreateInput,
  ForwardingRuleUpdateInput,
  Integration,
  Message,
  OptOut,
  Organization,
  OtpSettingsInput,
  Project,
  ProviderAccount,
  Routing,
  ScheduleCreateInput,
  ScheduleUpdateInput,
  VerifyAppCreateInput,
  VerifyAppList,
  VerifyAppUpdateInput,
  VerifyBlockReason,
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
import { api, BridgeApiError, unwrap } from './api';

export const keys = {
  projects: (orgId: string) => ['organizations', orgId, 'projects'] as const,
  project: (projectId: string) => ['projects', projectId] as const,
  apiKeys: (projectId: string) => ['projects', projectId, 'api-keys'] as const,
  devices: (projectId: string) => ['projects', projectId, 'devices'] as const,
  messages: (projectId: string) => ['projects', projectId, 'messages'] as const,
  usage: (projectId: string, env: string) => ['projects', projectId, 'usage', env] as const,
  webhooks: (projectId: string) => ['projects', projectId, 'webhooks'] as const,
  routing: (projectId: string) => ['projects', projectId, 'routing'] as const,
  providers: (projectId: string) => ['projects', projectId, 'providers'] as const,
  integrations: (projectId: string) => ['projects', projectId, 'integrations'] as const,
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
  /** Verify app ID or slug. */
  app?: string;
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
    mutationFn: ({
      environment,
      to,
      app,
    }: {
      environment: VerifyEnvironment;
      to: string;
      app?: string;
    }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/otp', {
          params: { path: { projectId }, query: { environment } },
          body: { to, app },
        }),
      ),
    onSuccess: refresh,
  });
  const verify = useMutation({
    mutationFn: ({
      environment,
      id,
      code,
      app,
    }: {
      environment: VerifyEnvironment;
      id: string;
      code: string;
      app?: string;
    }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/otp/verify', {
          params: { path: { projectId }, query: { environment } },
          body: { id, code, app },
        }),
      ),
    onSuccess: refresh,
  });
  return { send, verify };
}

// ---- Verify apps ---------------------------------------------------------------

const verifyAppsKey = (projectId: string) => ['projects', projectId, 'verify-apps'] as const;

export function useVerifyApps(projectId: string) {
  return useQuery({
    queryKey: verifyAppsKey(projectId),
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/verify-apps', { params: { path: { projectId } } }),
      ).then((r) => r.data),
    enabled: projectId !== '',
  });
}

export function useCreateVerifyApp(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: VerifyAppCreateInput) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/verify-apps', { params: { path: { projectId } }, body }),
      ),
    onSuccess: ({ secret: _secret, ...created }) => {
      // Add it at once so the page can switch to it before the list refetches.
      qc.setQueryData<VerifyAppList['data']>(verifyAppsKey(projectId), (list) =>
        list ? [...list, created] : list,
      );
      qc.invalidateQueries({ queryKey: verifyAppsKey(projectId) });
    },
  });
}

export function useUpdateVerifyApp(projectId: string, appId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: VerifyAppUpdateInput) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/verify-apps/{appId}', {
          params: { path: { projectId, appId } },
          body,
        }),
      ),
    onSuccess: (updated) => {
      qc.setQueryData<VerifyAppList['data']>(verifyAppsKey(projectId), (list) =>
        list?.map((a) => (a.id === updated.id ? updated : a)),
      );
      // The default app also backs /otp/settings.
      if (updated.is_default) {
        qc.invalidateQueries({ queryKey: ['projects', projectId, 'otp', 'settings'] });
      }
    },
  });
}

export function useDeleteVerifyApp(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (appId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/verify-apps/{appId}', {
          params: { path: { projectId, appId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: verifyAppsKey(projectId) }),
  });
}

/** Reveal (audited) or rotate an app's token signing secret. */
export function useVerifyAppSecret(projectId: string, appId: string) {
  const qc = useQueryClient();
  const path = { projectId, appId };
  return useMutation({
    mutationFn: (action: 'reveal' | 'rotate') =>
      action === 'reveal'
        ? unwrap(
            api.GET('/v1/projects/{projectId}/verify-apps/{appId}/secret', { params: { path } }),
          )
        : unwrap(
            api.POST('/v1/projects/{projectId}/verify-apps/{appId}/secret', { params: { path } }),
          ),
    // A first reveal creates the secret, which flips secret_set.
    onSuccess: () => qc.invalidateQueries({ queryKey: verifyAppsKey(projectId) }),
  });
}

export function useVerifyAppStats(
  projectId: string,
  appId: string,
  environment: VerifyEnvironment,
) {
  return useQuery({
    queryKey: ['projects', projectId, 'otp', 'app-stats', appId, environment],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/verify-apps/{appId}/stats', {
          params: { path: { projectId, appId }, query: { environment } },
        }),
      ),
    enabled: appId !== '',
    refetchInterval: 15_000,
  });
}

export type BlockFilters = { environment: VerifyEnvironment; reason?: VerifyBlockReason };

export function useVerifyAppBlocks(projectId: string, appId: string, filters: BlockFilters) {
  return useInfiniteQuery({
    queryKey: ['projects', projectId, 'otp', 'blocks', appId, filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/verify-apps/{appId}/blocks', {
          params: {
            path: { projectId, appId },
            query: { ...filters, limit: 25, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    enabled: appId !== '',
    refetchInterval: 15_000,
  });
}

// ---- providers and routing --------------------------------------------------

/** Supported SMS providers and the settings each one needs. Static per server. */
export function useProviderKinds() {
  return useQuery({
    queryKey: ['provider-kinds'],
    queryFn: () => unwrap(api.GET('/v1/provider-kinds')),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export function useRouting(projectId: string) {
  return useQuery({
    queryKey: keys.routing(projectId),
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/routing', { params: { path: { projectId } } })),
    enabled: projectId !== '',
  });
}

export type RoutingInput = Pick<Routing, 'mode' | 'fallback_after_seconds'>;

export function useUpdateRouting(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RoutingInput) =>
      unwrap(
        api.PUT('/v1/projects/{projectId}/routing', {
          params: { path: { projectId } },
          // secret_key_set is read-only: the server ignores it on writes.
          body: body as Routing,
        }),
      ),
    onSuccess: (data) => qc.setQueryData(keys.routing(projectId), data),
  });
}

export function useProviders(projectId: string) {
  return useQuery({
    queryKey: keys.providers(projectId),
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/providers', { params: { path: { projectId } } })),
    enabled: projectId !== '',
    refetchInterval: 15_000,
  });
}

export type NewProvider = components['schemas']['AddProviderRequest'];
export type ProviderUpdate = components['schemas']['ProviderInput'];

export function useAddProvider(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: NewProvider) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/providers', { params: { path: { projectId } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.providers(projectId) }),
  });
}

export function useUpdateProvider(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ providerId, ...body }: ProviderUpdate & { providerId: string }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/providers/{providerId}', {
          params: { path: { projectId, providerId } },
          body,
        }),
      ),
    onSuccess: (updated) =>
      qc.setQueryData<ProviderAccount[]>(keys.providers(projectId), (list) =>
        list?.map((p) => (p.id === updated.id ? updated : p)),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.providers(projectId) }),
  });
}

export function useRemoveProvider(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (providerId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/providers/{providerId}', {
          params: { path: { projectId, providerId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.providers(projectId) }),
  });
}

/** Asks the provider about the account without sending anything. */
export function useCheckProvider(projectId: string) {
  return useMutation({
    mutationFn: (providerId: string) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/providers/{providerId}/check', {
          params: { path: { projectId, providerId } },
        }),
      ),
  });
}

// ---- integrations -------------------------------------------------------------

export function useIntegrations(projectId: string) {
  return useQuery({
    queryKey: keys.integrations(projectId),
    queryFn: () =>
      unwrap(api.GET('/v1/projects/{projectId}/integrations', { params: { path: { projectId } } })),
    enabled: projectId !== '',
    refetchInterval: 15_000,
  });
}

export function useCreateIntegration(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { kind: Integration['kind']; environment: Integration['environment'] }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/integrations', {
          params: { path: { projectId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.integrations(projectId) }),
  });
}

export function useUpdateIntegration(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      integrationId,
      ...body
    }: {
      integrationId: string;
      secret?: string;
      environment?: Integration['environment'];
    }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/integrations/{integrationId}', {
          params: { path: { projectId, integrationId } },
          body,
        }),
      ),
    onSuccess: (updated) =>
      qc.setQueryData<Integration[]>(keys.integrations(projectId), (list) =>
        list?.map((i) => (i.id === updated.id ? updated : i)),
      ),
  });
}

export function useDeleteIntegration(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (integrationId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/integrations/{integrationId}', {
          params: { path: { projectId, integrationId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.integrations(projectId) }),
  });
}

// ---- messaging tools: broadcasts, schedules, opt-outs, automation -------------

export type Environment = 'live' | 'test';

const broadcastsKey = (projectId: string) => ['projects', projectId, 'broadcasts'] as const;
const schedulesKey = (projectId: string) => ['projects', projectId, 'schedules'] as const;
const optOutsKey = (projectId: string) => ['projects', projectId, 'opt-outs'] as const;
const autoRepliesKey = (projectId: string) => ['projects', projectId, 'auto-replies'] as const;
const forwardingKey = (projectId: string) => ['projects', projectId, 'forwarding-rules'] as const;

const broadcastActive = (b: Broadcast) => b.status === 'scheduled' || b.status === 'sending';

/** Broadcasts in one environment; refreshes every 3s while one is sending. */
export function useBroadcasts(
  projectId: string,
  environment: Environment,
  status?: BroadcastStatus,
) {
  return useInfiniteQuery({
    queryKey: [...broadcastsKey(projectId), environment, status ?? 'all'],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/broadcasts', {
          params: {
            path: { projectId },
            query: { environment, status, limit: 25, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: (q) =>
      q.state.data?.pages.some((p) => p.data.some((b) => b.status === 'sending')) ? 3_000 : 15_000,
  });
}

export function useBroadcast(projectId: string, broadcastId: string | null) {
  return useQuery({
    queryKey: [...broadcastsKey(projectId), 'detail', broadcastId],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/broadcasts/{broadcastId}', {
          params: { path: { projectId, broadcastId: broadcastId ?? '' } },
        }),
      ),
    enabled: broadcastId !== null,
    refetchInterval: (q) => (q.state.data && broadcastActive(q.state.data) ? 2_000 : false),
  });
}

export type BroadcastRequest = { environment: Environment; body: BroadcastCreateInput };

/** dry_run: validates and renders without creating anything. */
export function usePreviewBroadcast(projectId: string) {
  return useMutation({
    mutationFn: ({ environment, body }: BroadcastRequest) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/broadcasts', {
          params: { path: { projectId }, query: { environment } },
          body: { ...body, dry_run: true },
        }),
      ) as Promise<BroadcastPreview>,
  });
}

export function useCreateBroadcast(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ environment, body }: BroadcastRequest) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/broadcasts', {
          params: { path: { projectId }, query: { environment } },
          body: { ...body, dry_run: false },
        }),
      ) as Promise<Broadcast>,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: broadcastsKey(projectId) });
      qc.invalidateQueries({ queryKey: keys.messages(projectId) });
    },
  });
}

export function useCancelBroadcast(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (broadcastId: string) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/broadcasts/{broadcastId}/cancel', {
          params: { path: { projectId, broadcastId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: broadcastsKey(projectId) }),
  });
}

export function useSchedules(projectId: string, environment: Environment) {
  return useInfiniteQuery({
    queryKey: [...schedulesKey(projectId), environment],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/schedules', {
          params: {
            path: { projectId },
            query: { environment, limit: 50, starting_after: pageParam },
          },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: 15_000,
  });
}

export function useCreateSchedule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ environment, body }: { environment: Environment; body: ScheduleCreateInput }) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/schedules', {
          params: { path: { projectId }, query: { environment } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: schedulesKey(projectId) }),
  });
}

export function useUpdateSchedule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ scheduleId, body }: { scheduleId: string; body: ScheduleUpdateInput }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/schedules/{scheduleId}', {
          params: { path: { projectId, scheduleId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: schedulesKey(projectId) }),
  });
}

export function useDeleteSchedule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (scheduleId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/schedules/{scheduleId}', {
          params: { path: { projectId, scheduleId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: schedulesKey(projectId) }),
  });
}

export type ScheduleAction = 'pause' | 'resume' | 'run';

/** Pause, resume, or send once now. */
export function useScheduleAction(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ scheduleId, action }: { scheduleId: string; action: ScheduleAction }) => {
      const params = { params: { path: { projectId, scheduleId } } };
      if (action === 'pause') {
        return unwrap(api.POST('/v1/projects/{projectId}/schedules/{scheduleId}/pause', params));
      }
      if (action === 'resume') {
        return unwrap(api.POST('/v1/projects/{projectId}/schedules/{scheduleId}/resume', params));
      }
      return unwrap(api.POST('/v1/projects/{projectId}/schedules/{scheduleId}/run', params));
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: schedulesKey(projectId) });
      qc.invalidateQueries({ queryKey: keys.messages(projectId) });
    },
  });
}

export function useOptOuts(projectId: string, source?: OptOut['source']) {
  return useInfiniteQuery({
    queryKey: [...optOutsKey(projectId), source ?? 'all'],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/opt-outs', {
          params: { path: { projectId }, query: { source, limit: 100, starting_after: pageParam } },
        }),
      ),
    getNextPageParam: (last) => (last.has_more ? last.data.at(-1)?.id : undefined),
    refetchInterval: 30_000,
  });
}

export function useAddOptOut(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (number: string) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/opt-outs', {
          params: { path: { projectId } },
          body: { number },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: optOutsKey(projectId) }),
  });
}

export function useRemoveOptOut(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (number: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/opt-outs/{number}', {
          params: { path: { projectId, number } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: optOutsKey(projectId) }),
  });
}

/** Downloads the opt-out list as CSV through the dashboard's API proxy. */
export async function downloadOptOuts(projectId: string): Promise<void> {
  let res: Response;
  try {
    res = await fetch(`/api/v1/projects/${encodeURIComponent(projectId)}/opt-outs/export`, {
      credentials: 'same-origin',
    });
  } catch {
    throw new BridgeApiError(0, {
      code: 'network_error',
      message: 'Could not reach Bridge. Check your connection and that the API is running.',
    });
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => undefined)) as { error?: ErrorBody } | undefined;
    throw new BridgeApiError(res.status, body?.error, res.headers.get('x-request-id') ?? undefined);
  }
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = `opt-outs-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1_000);
}

export function useAutoReplies(projectId: string) {
  return useQuery({
    queryKey: autoRepliesKey(projectId),
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/auto-replies', { params: { path: { projectId } } }),
      ).then((r) => r.data),
    enabled: projectId !== '',
  });
}

export function useCreateAutoReply(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AutoReplyRuleCreateInput) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/auto-replies', {
          params: { path: { projectId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: autoRepliesKey(projectId) }),
  });
}

export function useUpdateAutoReply(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ruleId, ...body }: AutoReplyRuleUpdateInput & { ruleId: string }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/auto-replies/{ruleId}', {
          params: { path: { projectId, ruleId } },
          body,
        }),
      ),
    onSuccess: (updated) =>
      qc.setQueryData<AutoReplyRule[]>(autoRepliesKey(projectId), (list) =>
        list?.map((r) => (r.id === updated.id ? updated : r)),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: autoRepliesKey(projectId) }),
  });
}

export function useDeleteAutoReply(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/auto-replies/{ruleId}', {
          params: { path: { projectId, ruleId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: autoRepliesKey(projectId) }),
  });
}

export function useForwardingRules(projectId: string) {
  return useQuery({
    queryKey: forwardingKey(projectId),
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/forwarding-rules', { params: { path: { projectId } } }),
      ),
    enabled: projectId !== '',
    refetchInterval: 15_000,
  });
}

export function useCreateForwardingRule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ForwardingRuleCreateInput) =>
      unwrap(
        api.POST('/v1/projects/{projectId}/forwarding-rules', {
          params: { path: { projectId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: forwardingKey(projectId) }),
  });
}

export function useUpdateForwardingRule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ruleId, ...body }: ForwardingRuleUpdateInput & { ruleId: string }) =>
      unwrap(
        api.PATCH('/v1/projects/{projectId}/forwarding-rules/{ruleId}', {
          params: { path: { projectId, ruleId } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: forwardingKey(projectId) }),
  });
}

export function useDeleteForwardingRule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) =>
      unwrap(
        api.DELETE('/v1/projects/{projectId}/forwarding-rules/{ruleId}', {
          params: { path: { projectId, ruleId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: forwardingKey(projectId) }),
  });
}

/** A rule's recent deliveries, newest first; faster refresh while any is in flight. */
export function useForwardingDeliveries(projectId: string, ruleId: string, limit = 50) {
  return useQuery({
    queryKey: [...forwardingKey(projectId), ruleId, 'deliveries', limit],
    queryFn: () =>
      unwrap(
        api.GET('/v1/projects/{projectId}/forwarding-rules/{ruleId}/deliveries', {
          params: { path: { projectId, ruleId }, query: { limit } },
        }),
      ).then((r) => r.data),
    refetchInterval: (q) =>
      q.state.data?.some((d) => d.status === 'pending' || d.status === 'retrying') ? 3_000 : 15_000,
  });
}

/** Reveals a rule's webhook signing secret. Every reveal is audited. */
export function useForwardingSecret(projectId: string) {
  return useMutation({
    mutationFn: (ruleId: string) =>
      unwrap(
        api.GET('/v1/projects/{projectId}/forwarding-rules/{ruleId}/secret', {
          params: { path: { projectId, ruleId } },
        }),
      ),
  });
}
