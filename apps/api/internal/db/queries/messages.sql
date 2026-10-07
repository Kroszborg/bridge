-- name: InsertMessage :one
INSERT INTO messages (
    id, project_id, environment, direction, status, provider, api_key_id, requested_device_id,
    recipient, body, segments, encoding, metadata, idempotency_key, idempotency_hash, sim_slot,
    body_sha256, body_length, purpose, display_body, body_vars, queued_at
) VALUES (
    @id, @project_id, @environment, 'outbound', 'queued', @provider, sqlc.narg(api_key_id), sqlc.narg(requested_device_id),
    @recipient, @body, @segments, @encoding, @metadata, sqlc.narg(idempotency_key), sqlc.narg(idempotency_hash), sqlc.narg(sim_slot),
    @body_sha256, @body_length, @purpose, sqlc.narg(display_body), sqlc.narg(body_vars), now()
)
RETURNING *;

-- name: GetMessageByIdempotencyKey :one
SELECT * FROM messages WHERE project_id = $1 AND idempotency_key = $2;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = @id AND project_id = @project_id;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = $1;

-- name: ListMessages :many
SELECT * FROM messages
WHERE project_id = @project_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR environment = sqlc.narg(environment))
  AND (sqlc.narg(status)::message_status IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(direction)::message_direction IS NULL OR direction = sqlc.narg(direction))
  AND (sqlc.narg(device_id)::text IS NULL OR device_id = sqlc.narg(device_id))
  AND (sqlc.narg(recipient)::text IS NULL OR recipient = sqlc.narg(recipient))
  AND (sqlc.narg(sender)::text IS NULL OR sender = sqlc.narg(sender))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: ListMessageEvents :many
SELECT * FROM message_events WHERE message_id = $1 ORDER BY created_at, id;

-- name: InsertMessageEvent :exec
INSERT INTO message_events (id, message_id, project_id, type, from_status, to_status, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: TransitionMessage :one
-- Moves a message to @to_status only from one of @from_statuses. The single
-- place statuses change; callers validate the transition first.
UPDATE messages SET
    status        = @to_status::message_status,
    sending_at    = CASE WHEN @to_status::message_status = 'sending'   THEN now() ELSE sending_at END,
    sent_at       = CASE WHEN @to_status::message_status = 'sent'      THEN now() ELSE sent_at END,
    delivered_at  = CASE WHEN @to_status::message_status = 'delivered' THEN now() ELSE delivered_at END,
    failed_at     = CASE WHEN @to_status::message_status = 'failed'    THEN now() ELSE failed_at END,
    queued_at     = CASE WHEN @to_status::message_status = 'queued'    THEN now() ELSE queued_at END,
    error_code    = CASE WHEN @to_status::message_status = 'failed'    THEN sqlc.narg(error_code)::text ELSE error_code END,
    error_message = CASE WHEN @to_status::message_status = 'failed'    THEN sqlc.narg(error_message)::text ELSE error_message END,
    segments      = COALESCE(sqlc.narg(segments)::smallint, segments),
    -- Returning to the queue releases the device assignment.
    device_id     = CASE WHEN @to_status::message_status = 'queued' THEN NULL ELSE device_id END,
    assigned_at   = CASE WHEN @to_status::message_status = 'queued' THEN NULL ELSE assigned_at END,
    updated_at    = now()
WHERE id = @id AND status::text = ANY(@from_statuses::text[])
RETURNING *;

-- name: AssignMessage :one
UPDATE messages SET device_id = @device_id, assigned_at = now(), attempts = attempts + 1, updated_at = now()
WHERE id = @id AND status = 'queued' AND device_id IS NULL
RETURNING *;

-- name: ReleaseMessage :one
-- Takes a queued message back from a device that did not accept it in time.
UPDATE messages SET device_id = NULL, assigned_at = NULL, updated_at = now()
WHERE id = @id AND status = 'queued' AND device_id = @device_id
RETURNING *;

-- name: DispatchCandidates :many
SELECT sqlc.embed(devices),
       (SELECT count(*) FROM messages m
         WHERE m.device_id = devices.id
           AND m.assigned_at > now() - make_interval(secs => devices.send_limit_window_seconds))::int AS recent_sends,
       -- Epoch stands for "none" so the columns are never NULL.
       COALESCE((SELECT min(m.assigned_at) FROM messages m
         WHERE m.device_id = devices.id
           AND m.assigned_at > now() - make_interval(secs => devices.send_limit_window_seconds)), to_timestamp(0))::timestamptz AS oldest_in_window,
       COALESCE((SELECT max(m.assigned_at) FROM messages m WHERE m.device_id = devices.id), to_timestamp(0))::timestamptz AS last_assigned
FROM devices
WHERE devices.project_id = $1 AND devices.revoked_at IS NULL;

-- name: PendingForDevice :many
SELECT * FROM messages
WHERE device_id = $1 AND status IN ('queued', 'sending')
ORDER BY created_at
LIMIT 100;

-- name: StaleAssignments :many
SELECT * FROM messages
WHERE status = 'queued' AND device_id IS NOT NULL AND assigned_at < @before
ORDER BY assigned_at
LIMIT 200;

-- name: RedactMessageBodies :execrows
UPDATE messages SET body = '', body_vars = NULL, body_redacted_at = now(), updated_at = now()
WHERE body_redacted_at IS NULL
  AND (created_at < @before
       -- OTP bodies contain the code. A Verify code's message is kept until its
       -- verification finishes (which redacts it) or the longest code lifetime has
       -- passed, so it can be resent through another route; codes delivered for
       -- other systems go as soon as the phone is done with them.
       OR (purpose = 'otp' AND (created_at < @otp_before OR NOT (metadata ? 'otp_id'))))
  AND status IN ('sent', 'delivered', 'failed', 'received');

-- name: DeviceWindowCounts :many
SELECT d.id,
       (SELECT count(*) FROM messages m
         WHERE m.device_id = d.id
           AND m.assigned_at > now() - make_interval(secs => d.send_limit_window_seconds))::int AS recent_sends,
       (SELECT count(*) FROM messages m WHERE m.device_id = d.id AND m.status IN ('sent', 'delivered'))::int AS total_sent,
       (SELECT count(*) FROM messages m WHERE m.device_id = d.id AND m.status = 'failed')::int AS total_failed
FROM devices d
WHERE d.project_id = $1;

-- name: MessageStats :one
SELECT
    count(*)::int                                                      AS total,
    count(*) FILTER (WHERE status = 'delivered')::int                  AS delivered,
    count(*) FILTER (WHERE status = 'sent')::int                       AS sent,
    count(*) FILTER (WHERE status = 'failed')::int                     AS failed,
    count(*) FILTER (WHERE status IN ('created', 'queued', 'sending'))::int AS pending,
    COALESCE(avg(EXTRACT(EPOCH FROM (sent_at - created_at))) FILTER (WHERE sent_at IS NOT NULL), 0)::float8 AS avg_send_seconds
FROM messages
WHERE project_id = @project_id AND environment = @environment AND direction = 'outbound' AND created_at >= @since;

-- name: UpdateDeviceSettings :one
UPDATE devices SET
    name = COALESCE(sqlc.narg(name)::text, name),
    preferred_sim_slot = CASE WHEN @set_sim::bool THEN sqlc.narg(preferred_sim_slot)::smallint ELSE preferred_sim_slot END,
    send_limit_count = COALESCE(sqlc.narg(send_limit_count)::int, send_limit_count),
    forward_inbound = COALESCE(sqlc.narg(forward_inbound)::bool, forward_inbound),
    updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: UpdateDeviceSims :exec
UPDATE devices SET sims = $2, updated_at = now() WHERE id = $1;

-- name: WaitingMessages :many
-- Queued messages not yet assigned to a phone.
SELECT id FROM messages
WHERE project_id = $1 AND status = 'queued' AND device_id IS NULL AND provider = 'android'
ORDER BY created_at
LIMIT 100;

-- name: DailyMessageUsage :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day,
       count(*) FILTER (WHERE direction = 'outbound')::int AS outbound,
       count(*) FILTER (WHERE direction = 'outbound' AND status = 'delivered')::int AS delivered,
       count(*) FILTER (WHERE direction = 'outbound' AND status = 'sent')::int AS sent,
       count(*) FILTER (WHERE direction = 'outbound' AND status = 'failed')::int AS failed,
       count(*) FILTER (WHERE direction = 'outbound' AND status IN ('created', 'queued', 'sending'))::int AS pending,
       count(*) FILTER (WHERE direction = 'inbound')::int AS inbound,
       COALESCE(sum(segments) FILTER (WHERE direction = 'outbound'), 0)::int AS segments
FROM messages
WHERE project_id = @project_id AND environment = @environment AND created_at >= @since
GROUP BY 1
ORDER BY 1;

-- name: DeviceMessageUsage :many
SELECT m.device_id::text AS device_id,
       COALESCE(max(d.name), '')::text AS device_name,
       count(*) FILTER (WHERE m.direction = 'outbound')::int AS outbound,
       count(*) FILTER (WHERE m.direction = 'outbound' AND m.status = 'delivered')::int AS delivered,
       count(*) FILTER (WHERE m.direction = 'outbound' AND m.status = 'failed')::int AS failed,
       count(*) FILTER (WHERE m.direction = 'inbound')::int AS inbound,
       COALESCE(sum(m.segments) FILTER (WHERE m.direction = 'outbound'), 0)::int AS segments
FROM messages m
LEFT JOIN devices d ON d.id = m.device_id
WHERE m.project_id = @project_id AND m.environment = @environment AND m.created_at >= @since AND m.device_id IS NOT NULL
GROUP BY m.device_id
ORDER BY outbound DESC, inbound DESC;

-- name: LockUnassignedMessage :one
-- Locks a queued message no phone has taken, so dispatch waits while it is canceled.
SELECT * FROM messages WHERE id = $1 AND status = 'queued' AND device_id IS NULL FOR UPDATE;
