-- name: ListRequestLogs :many
SELECT * FROM api_request_logs
WHERE project_id = @project_id
  AND environment = @environment
  AND (sqlc.narg(method)::text IS NULL OR method = sqlc.narg(method))
  AND (sqlc.narg(status_min)::int IS NULL OR status >= sqlc.narg(status_min))
  AND (sqlc.narg(status_max)::int IS NULL OR status <= sqlc.narg(status_max))
  AND (sqlc.narg(api_key_id)::text IS NULL OR api_key_id = sqlc.narg(api_key_id))
  AND (sqlc.narg(path_prefix)::text IS NULL OR starts_with(path, sqlc.narg(path_prefix)))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: GetRequestLog :one
SELECT * FROM api_request_logs WHERE id = @id AND project_id = @project_id;

-- name: DeleteOldRequestLogs :execrows
DELETE FROM api_request_logs WHERE created_at < @before;

-- name: DailyRequestUsage :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day,
       count(*)::int AS requests,
       count(*) FILTER (WHERE status >= 400 AND status < 500)::int AS client_errors,
       count(*) FILTER (WHERE status >= 500)::int AS server_errors,
       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 AS p95_ms
FROM api_request_logs
WHERE project_id = @project_id AND environment = @environment AND created_at >= @since
GROUP BY 1
ORDER BY 1;
