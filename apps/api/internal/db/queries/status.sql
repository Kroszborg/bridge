-- name: UpsertHeartbeat :exec
INSERT INTO instance_heartbeats (instance_id, kind, version, hostname, started_at, seen_at)
VALUES (@instance_id, @kind, @version, @hostname, @started_at, now())
ON CONFLICT (instance_id) DO UPDATE SET seen_at = now(), version = EXCLUDED.version;

-- name: DeleteHeartbeat :exec
DELETE FROM instance_heartbeats WHERE instance_id = $1;

-- name: ListHeartbeats :many
SELECT * FROM instance_heartbeats WHERE seen_at > now() - interval '1 hour' ORDER BY kind, started_at;

-- name: InsertStatusSample :exec
INSERT INTO status_samples (component, sampled_at, status, value, detail)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (component, sampled_at) DO UPDATE SET status = EXCLUDED.status, value = EXCLUDED.value, detail = EXCLUDED.detail;

-- name: LatestStatusSamples :many
SELECT DISTINCT ON (component) * FROM status_samples
WHERE sampled_at > now() - interval '10 minutes'
ORDER BY component, sampled_at DESC;

-- name: DailyStatus :many
-- Per component and UTC day: samples taken and how many were not an outage
-- or not degraded. Days without samples are missing (no data, not downtime).
SELECT component,
       to_char(sampled_at AT TIME ZONE 'UTC', 'YYYY-MM-DD')::text AS day,
       count(*)::int AS samples,
       count(*) FILTER (WHERE status <> 'outage')::int AS up,
       count(*) FILTER (WHERE status = 'operational')::int AS operational
FROM status_samples
WHERE sampled_at >= @since
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: DeleteOldStatusSamples :execrows
DELETE FROM status_samples WHERE sampled_at < @before;

-- name: FleetStats :one
SELECT count(*) FILTER (WHERE revoked_at IS NULL)::int AS total,
       count(*) FILTER (WHERE revoked_at IS NULL AND status = 'online')::int AS online
FROM devices;

-- name: MessageBacklog :one
SELECT count(*)::int AS waiting,
       COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 AS oldest_seconds
FROM messages WHERE status = 'queued' AND device_id IS NULL AND provider = 'android';

-- name: FirstUserEmail :one
SELECT email FROM users ORDER BY created_at, id LIMIT 1;
