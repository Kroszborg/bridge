-- Operator Insights: instance-wide aggregates for GET /v1/system/insights.
-- Every query is bounded by created_at >= @since or by a LIMIT, and none
-- returns message bodies or phone numbers.

-- name: InsightsTotals :one
SELECT (SELECT count(*) FROM users)::int AS users,
       (SELECT count(*) FROM users WHERE email_verified_at IS NOT NULL)::int AS users_email_verified,
       (SELECT count(*) FROM users WHERE phone_verified_at IS NOT NULL)::int AS users_with_phone,
       (SELECT count(*) FROM users u WHERE u.created_at >= @since)::int AS users_in_range,
       (SELECT count(*) FROM organizations)::int AS organizations,
       (SELECT count(*) FROM organizations o WHERE o.created_at >= @since)::int AS organizations_in_range,
       (SELECT count(*) FROM projects)::int AS projects,
       (SELECT count(*) FROM api_keys
         WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now()))::int AS api_keys,
       (SELECT count(*) FROM subscriptions WHERE status = 'active')::int AS subscriptions_active,
       (SELECT count(DISTINCT user_id) FROM sessions
         WHERE last_seen_at >= now() - interval '7 days')::int AS active_users_7d;

-- name: InsightsActiveOrganizations :one
-- Workspaces that sent at least one message (live or test) in the last 7 and 30 days.
WITH recent AS (
    SELECT project_id, max(created_at) AS last_sent
    FROM messages
    WHERE created_at >= now() - interval '30 days' AND direction = 'outbound'
    GROUP BY project_id
)
SELECT (count(DISTINCT p.organization_id) FILTER (WHERE r.last_sent >= now() - interval '7 days'))::int AS active_7d,
       count(DISTINCT p.organization_id)::int AS active_30d
FROM recent r
JOIN projects p ON p.id = r.project_id;

-- name: InsightsSignupsDaily :many
-- One row per day from @first_day to @last_day, including days without sign-ups.
-- @since is the start of @first_day in UTC; days are UTC calendar days.
WITH days AS (
    SELECT d::date AS day
    FROM generate_series(sqlc.arg(first_day)::date, sqlc.arg(last_day)::date, interval '1 day') AS d
), u AS (
    SELECT (su.created_at AT TIME ZONE 'UTC')::date AS day, count(*) AS n
    FROM users su WHERE su.created_at >= @since GROUP BY 1
), o AS (
    SELECT (so.created_at AT TIME ZONE 'UTC')::date AS day, count(*) AS n
    FROM organizations so WHERE so.created_at >= @since GROUP BY 1
)
SELECT to_char(days.day, 'YYYY-MM-DD')::text AS date,
       COALESCE(u.n, 0)::int AS users,
       COALESCE(o.n, 0)::int AS organizations
FROM days
LEFT JOIN u ON u.day = days.day
LEFT JOIN o ON o.day = days.day
ORDER BY days.day;

-- name: InsightsMessagesDaily :many
-- One row per UTC day, like InsightsSignupsDaily. Statuses count live outgoing
-- messages only; test messages are simulated.
WITH days AS (
    SELECT d::date AS day
    FROM generate_series(sqlc.arg(first_day)::date, sqlc.arg(last_day)::date, interval '1 day') AS d
), m AS (
    SELECT (created_at AT TIME ZONE 'UTC')::date AS day,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'live') AS live,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'test') AS test,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'live' AND status = 'delivered') AS delivered,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'live' AND status = 'sent') AS sent,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'live' AND status = 'failed') AS failed,
           count(*) FILTER (WHERE direction = 'outbound' AND environment = 'live'
                            AND status IN ('created', 'queued', 'sending')) AS pending,
           count(*) FILTER (WHERE direction = 'inbound') AS inbound
    FROM messages
    WHERE created_at >= @since
    GROUP BY 1
)
SELECT to_char(days.day, 'YYYY-MM-DD')::text AS date,
       COALESCE(m.live, 0)::int AS live,
       COALESCE(m.test, 0)::int AS test,
       COALESCE(m.delivered, 0)::int AS delivered,
       COALESCE(m.sent, 0)::int AS sent,
       COALESCE(m.failed, 0)::int AS failed,
       COALESCE(m.pending, 0)::int AS pending,
       COALESCE(m.inbound, 0)::int AS inbound
FROM days
LEFT JOIN m ON m.day = days.day
ORDER BY days.day;

-- name: InsightsMessagesByProvider :many
-- Outgoing messages per route; "simulator" is test mode, "android" a paired phone.
SELECT provider, count(*)::int AS messages
FROM messages
WHERE created_at >= @since AND direction = 'outbound'
GROUP BY provider
ORDER BY 2 DESC, 1
LIMIT 20;

-- name: InsightsTopErrors :many
SELECT COALESCE(error_code, 'unknown')::text AS error_code, count(*)::int AS messages
FROM messages
WHERE created_at >= @since AND environment = 'live' AND direction = 'outbound' AND status = 'failed'
GROUP BY 1
ORDER BY 2 DESC, 1
LIMIT 8;

-- name: InsightsVerify :one
SELECT count(*)::int AS started,
       (count(*) FILTER (WHERE status = 'verified'))::int AS verified
FROM otp_verifications
WHERE created_at >= @since;

-- name: InsightsTopOrganizations :many
-- The ten workspaces that sent the most live messages since @since, with their
-- paired phones and the plan in effect (@free_plan without an active subscription).
WITH by_project AS (
    SELECT bm.project_id, count(*) AS n
    FROM messages bm
    WHERE bm.created_at >= @since AND bm.environment = 'live' AND bm.direction = 'outbound'
    GROUP BY bm.project_id
), top AS (
    SELECT p.organization_id, sum(b.n) AS messages
    FROM by_project b
    JOIN projects p ON p.id = b.project_id
    GROUP BY p.organization_id
    ORDER BY 2 DESC, 1
    LIMIT 10
)
SELECT t.organization_id,
       o.name,
       t.messages::int AS messages,
       (SELECT count(*) FROM devices d JOIN projects dp ON dp.id = d.project_id
         WHERE dp.organization_id = t.organization_id AND d.revoked_at IS NULL)::int AS phones,
       pl.name AS plan_name
FROM top t
JOIN organizations o ON o.id = t.organization_id
LEFT JOIN subscriptions s ON s.organization_id = t.organization_id
JOIN plans pl ON pl.id = CASE WHEN s.status IN ('active', 'past_due') THEN s.plan_id ELSE sqlc.arg(free_plan)::text END
ORDER BY t.messages DESC, t.organization_id;

-- name: InsightsRecentUsers :many
SELECT u.id, u.email, u.name, u.created_at, u.email_verified_at, u.phone_verified_at,
       (SELECT count(*) FROM organization_members m WHERE m.user_id = u.id)::int AS organizations
FROM users u
ORDER BY u.created_at DESC, u.id DESC
LIMIT 15;

-- name: InsightsPlanMix :many
-- Workspaces per plan in effect: active and past_due subscriptions keep their
-- plan, everything else is on @free_plan.
WITH effective AS (
    SELECT CASE WHEN s.status IN ('active', 'past_due') THEN s.plan_id ELSE sqlc.arg(free_plan)::text END AS plan_id
    FROM organizations o
    LEFT JOIN subscriptions s ON s.organization_id = o.id
)
SELECT pl.id, pl.name, pl.price_cents, pl.currency, count(e.plan_id)::int AS organizations
FROM plans pl
LEFT JOIN effective e ON e.plan_id = pl.id
GROUP BY pl.id
ORDER BY pl.sort_order, pl.id;

-- name: InsightsMRR :one
-- Monthly recurring revenue of active subscriptions, in the plans' smallest unit.
SELECT COALESCE(sum(pl.price_cents), 0)::bigint AS mrr_cents
FROM subscriptions s
JOIN plans pl ON pl.id = s.plan_id
WHERE s.status = 'active';

-- name: InsightsRecentSubscriptions :many
SELECT s.organization_id, o.name AS organization_name, pl.name AS plan_name, s.status,
       s.cancel_at_period_end, s.updated_at
FROM subscriptions s
JOIN organizations o ON o.id = s.organization_id
JOIN plans pl ON pl.id = s.plan_id
ORDER BY s.updated_at DESC, s.organization_id
LIMIT 10;
