# Integrations

Bridge works with auth platforms and automation tools in three ways:

| How | Who generates and checks the code | Guides |
| --- | --- | --- |
| **Built-in hook.** The other service calls a URL Bridge gives you. No code to write. | The other service | [Supabase Auth](supabase.md) |
| **A few lines of code** in the other service's callback, using the [SDK](../../packages/sdk/README.md) or HTTP. | The other service, or Bridge [Verify](../otp/README.md) | [Better Auth](better-auth.md), [Auth0](auth0.md) |
| **HTTP requests and webhooks** from a workflow tool. | Bridge Verify, or none | [n8n, Zapier and Make](no-code.md) |

Firebase does not let you replace the SMS sender for its own phone sign-in; use Bridge Verify next
to it. Clerk lets you deliver its SMS yourself from a webhook. Both are in
[Firebase and Clerk](firebase-clerk.md).

Whichever way a message reaches Bridge, it is routed like any other: through your phones, or an
[SMS provider](../providers/README.md) when routing says so.

## Codes and privacy

- Codes that go through **Verify** or the **Supabase hook** are sent as one-time-password messages
  (`purpose: otp`): the API, dashboard and webhooks show them masked (`•••••• is your Acme code…`),
  and the stored text is erased once the SMS is sent.
- Codes you send yourself with `POST /v1/messages` (Better Auth, Auth0, Clerk, workflow tools) are
  ordinary messages. Bridge does not know the text contains a code, so it is shown in full and kept
  for `BRIDGE_MESSAGE_RETENTION` like any other message. Prefer Verify where the platform allows it.

## Integrations in the API

Built-in hooks are managed under **Integrations** in the dashboard, or with these
session-authenticated routes. Only owners and admins can change them.

| Method and path | Purpose |
| --- | --- |
| `GET`, `POST /v1/projects/{projectId}/integrations` | List, or create (`kind`, `environment`). |
| `PATCH /v1/projects/{projectId}/integrations/{integrationId}` | Store the other service's signing `secret` (write-only), or switch `environment`. |
| `DELETE /v1/projects/{projectId}/integrations/{integrationId}` | Remove it. The hook URL stops working. |

An integration's `environment` is `live` (sends real SMS) or `test` (nothing is sent; messages go
to the simulator). Creating one needs `BRIDGE_SECRET_KEY`, because the signing secret is stored
encrypted like [provider credentials](../providers/README.md#before-you-start). Changes are
audit-logged as `integration.created`, `integration.updated` and `integration.deleted`.
