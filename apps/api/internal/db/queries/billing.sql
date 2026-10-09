-- name: ListPlans :many
SELECT * FROM plans ORDER BY sort_order, id;

-- name: GetPlan :one
SELECT * FROM plans WHERE id = $1;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE organization_id = $1;

-- name: GetSubscriptionByProviderID :one
SELECT * FROM subscriptions WHERE provider_subscription_id = $1;

-- name: GetSubscriptionByCustomer :one
SELECT * FROM subscriptions WHERE provider_customer_id = $1 ORDER BY updated_at DESC LIMIT 1;

-- name: UpsertSubscription :one
INSERT INTO subscriptions (
    organization_id, plan_id, status, provider_customer_id, provider_subscription_id,
    current_period_end, cancel_at_period_end
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (organization_id) DO UPDATE SET
    plan_id = EXCLUDED.plan_id,
    status = EXCLUDED.status,
    provider_customer_id = COALESCE(EXCLUDED.provider_customer_id, subscriptions.provider_customer_id),
    provider_subscription_id = EXCLUDED.provider_subscription_id,
    current_period_end = EXCLUDED.current_period_end,
    cancel_at_period_end = EXCLUDED.cancel_at_period_end,
    updated_at = now()
RETURNING *;

-- name: ClaimBillingWebhookEvent :one
INSERT INTO billing_webhook_events (id, type) VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
RETURNING id;

-- name: IncrementLiveUsage :one
-- Counts one live message, unless the organization already reached max_messages.
-- No row returned means the limit was reached.
INSERT INTO organization_usage AS u (organization_id, period, live_messages)
SELECT sqlc.arg(organization_id)::text, sqlc.arg(period)::date, 1
WHERE sqlc.arg(max_messages)::integer > 0
ON CONFLICT (organization_id, period) DO UPDATE SET live_messages = u.live_messages + 1
WHERE u.live_messages < sqlc.arg(max_messages)::integer
RETURNING live_messages;

-- name: GetLiveUsage :one
SELECT COALESCE((
    SELECT live_messages FROM organization_usage WHERE organization_id = $1 AND period = $2
), 0)::integer AS live_messages;

-- name: CountOrganizationPhones :one
SELECT count(*) FROM devices d
JOIN projects p ON p.id = d.project_id
WHERE p.organization_id = $1 AND d.revoked_at IS NULL;

-- name: CountOrganizationProjects :one
SELECT count(*) FROM projects WHERE organization_id = $1;

-- name: CountOrganizationSeats :one
-- Members plus invitations that are still open, so invites cannot exceed the plan.
SELECT (
    (SELECT count(*) FROM organization_members m WHERE m.organization_id = $1) +
    (SELECT count(*) FROM organization_invites i
      WHERE i.organization_id = $1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now())
)::bigint AS seats;

-- name: CountOrganizationMembers :one
SELECT count(*) FROM organization_members WHERE organization_id = $1;

-- name: GetSubscriptionForUpdate :one
-- Locks the workspace's subscription row while a webhook decides what to store.
SELECT * FROM subscriptions WHERE organization_id = $1 FOR UPDATE;

-- name: OrganizationExists :one
SELECT EXISTS (SELECT 1 FROM organizations WHERE id = $1);
