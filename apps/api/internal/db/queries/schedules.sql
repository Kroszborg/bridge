-- name: InsertSchedule :one
INSERT INTO scheduled_messages (
    id, project_id, environment, name, recipient, body, device_id, kind, at_time, days, day_of_month,
    run_date, time_zone, ends_at, paused, next_run_at, api_key_id, created_by
) VALUES (
    @id, @project_id, @environment, @name, @recipient, @body, sqlc.narg(device_id), @kind, @at_time, @days, sqlc.narg(day_of_month),
    sqlc.narg(run_date), @time_zone, sqlc.narg(ends_at), @paused, sqlc.narg(next_run_at), sqlc.narg(api_key_id), sqlc.narg(created_by)
)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM scheduled_messages WHERE id = @id AND project_id = @project_id;

-- name: GetScheduleByID :one
SELECT * FROM scheduled_messages WHERE id = $1;

-- name: ListSchedules :many
SELECT * FROM scheduled_messages
WHERE project_id = @project_id AND environment = @environment
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: UpdateSchedule :one
UPDATE scheduled_messages SET
    name = @name, recipient = @recipient, body = @body, device_id = sqlc.narg(device_id),
    kind = @kind, at_time = @at_time, days = @days, day_of_month = sqlc.narg(day_of_month),
    run_date = sqlc.narg(run_date), time_zone = @time_zone, ends_at = sqlc.narg(ends_at),
    paused = @paused, next_run_at = sqlc.narg(next_run_at), updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteSchedule :execrows
DELETE FROM scheduled_messages WHERE id = @id AND project_id = @project_id;

-- name: ClaimDueSchedules :many
-- Locks due schedules; concurrent workers skip each other's rows.
SELECT * FROM scheduled_messages
WHERE NOT paused AND next_run_at IS NOT NULL AND next_run_at <= @now::timestamptz
ORDER BY next_run_at
LIMIT @row_limit
FOR UPDATE SKIP LOCKED;

-- name: AdvanceSchedule :exec
UPDATE scheduled_messages SET
    next_run_at = sqlc.narg(next_run_at), last_run_at = @ran_at::timestamptz, run_count = run_count + 1, updated_at = now()
WHERE id = @id;

-- name: RecordScheduleResult :exec
UPDATE scheduled_messages SET
    last_message_id = COALESCE(sqlc.narg(last_message_id)::text, last_message_id),
    last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = @id;
