-- name: InsertBroadcast :one
INSERT INTO broadcasts (
    id, project_id, environment, name, template, device_id, status, scheduled_at,
    total_recipients, skipped_opted_out, duplicates, total_segments, api_key_id, created_by, started_at
) VALUES (
    @id, @project_id, @environment, @name, @template, sqlc.narg(device_id), @status, sqlc.narg(scheduled_at),
    @total_recipients, @skipped_opted_out, @duplicates, @total_segments, sqlc.narg(api_key_id), sqlc.narg(created_by),
    CASE WHEN @status::text = 'sending' THEN now() END
)
RETURNING *;

-- name: InsertBroadcastRecipients :copyfrom
INSERT INTO broadcast_recipients (broadcast_id, position, recipient, vars) VALUES ($1, $2, $3, $4);

-- name: GetBroadcast :one
SELECT * FROM broadcasts WHERE id = @id AND project_id = @project_id;

-- name: GetBroadcastByID :one
SELECT * FROM broadcasts WHERE id = $1;

-- name: ListBroadcasts :many
SELECT * FROM broadcasts
WHERE project_id = @project_id AND environment = @environment
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: BroadcastCounts :many
-- Progress of broadcasts, from their recipients and the messages created for them.
SELECT r.broadcast_id,
       count(*) FILTER (WHERE r.status = 'pending')::int                                        AS waiting,
       count(*) FILTER (WHERE m.status IN ('created', 'queued', 'sending'))::int                AS in_flight,
       count(*) FILTER (WHERE m.status = 'sent')::int                                           AS sent,
       count(*) FILTER (WHERE m.status = 'delivered')::int                                      AS delivered,
       count(*) FILTER (WHERE m.status = 'failed' AND m.error_code IS DISTINCT FROM 'canceled')::int AS failed,
       count(*) FILTER (WHERE r.status = 'canceled' OR (m.status = 'failed' AND m.error_code = 'canceled'))::int AS canceled,
       count(*) FILTER (WHERE r.status = 'skipped')::int                                        AS skipped
FROM broadcast_recipients r
LEFT JOIN messages m ON m.id = r.message_id
WHERE r.broadcast_id = ANY(@ids::text[])
GROUP BY r.broadcast_id;

-- name: BroadcastInFlight :one
-- Messages of a broadcast that no phone or provider has finished with yet.
SELECT count(*)::int FROM broadcast_recipients r
JOIN messages m ON m.id = r.message_id
WHERE r.broadcast_id = $1 AND m.status IN ('created', 'queued', 'sending');

-- name: PendingBroadcastRecipients :many
SELECT * FROM broadcast_recipients
WHERE broadcast_id = @broadcast_id AND status = 'pending'
ORDER BY position
LIMIT @row_limit;

-- name: MarkRecipientQueued :execrows
UPDATE broadcast_recipients SET status = 'queued', message_id = @message_id, vars = NULL
WHERE broadcast_id = @broadcast_id AND position = @position AND status = 'pending';

-- name: MarkRecipientSkipped :exec
UPDATE broadcast_recipients SET status = 'skipped', skip_reason = @skip_reason, vars = NULL
WHERE broadcast_id = @broadcast_id AND position = @position AND status = 'pending';

-- name: StartBroadcast :one
UPDATE broadcasts SET status = 'sending', started_at = now(), updated_at = now()
WHERE id = $1 AND status = 'scheduled'
RETURNING *;

-- name: CompleteBroadcast :one
UPDATE broadcasts SET status = 'completed', completed_at = now(), updated_at = now()
WHERE id = $1 AND status = 'sending'
RETURNING *;

-- name: CancelBroadcast :one
UPDATE broadcasts SET status = 'canceled', canceled_at = now(), updated_at = now()
WHERE id = @id AND project_id = @project_id AND status IN ('scheduled', 'sending')
RETURNING *;

-- name: CancelPendingRecipients :execrows
UPDATE broadcast_recipients SET status = 'canceled', vars = NULL
WHERE broadcast_id = $1 AND status = 'pending';

-- name: UndispatchedBroadcastMessages :many
-- Messages of a broadcast still waiting in the queue, not yet with a phone or provider.
SELECT m.* FROM broadcast_recipients r
JOIN messages m ON m.id = r.message_id
WHERE r.broadcast_id = $1 AND m.status = 'queued' AND m.device_id IS NULL
  AND m.provider IN ('android', 'simulator');

-- name: ProjectSendCapacity :one
-- Messages the project's phones may send per send-limit window, together.
SELECT COALESCE(sum(send_limit_count), 0)::int FROM devices
WHERE project_id = @project_id AND revoked_at IS NULL
  AND (sqlc.narg(device_id)::text IS NULL OR id = sqlc.narg(device_id));
