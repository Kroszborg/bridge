-- name: InsertOTP :one
INSERT INTO otp_verifications (
    id, project_id, environment, api_key_id, recipient, code_hash, test_code,
    code_length, max_attempts, message_id, metadata, expires_at
) VALUES (
    @id, @project_id, @environment, sqlc.narg(api_key_id), @recipient, @code_hash, sqlc.narg(test_code),
    @code_length, @max_attempts, @message_id, @metadata, @expires_at
)
RETURNING *;

-- name: CancelPendingOTPs :exec
-- A new code replaces any code still pending for the same number.
UPDATE otp_verifications SET status = 'canceled', code_hash = NULL, updated_at = now()
WHERE project_id = @project_id AND environment = @environment AND recipient = @recipient AND status = 'pending';

-- name: LatestOTPForRecipient :one
SELECT * FROM otp_verifications
WHERE project_id = @project_id AND environment = @environment AND recipient = @recipient
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LatestPendingOTPForUpdate :one
SELECT * FROM otp_verifications
WHERE project_id = @project_id AND environment = @environment AND recipient = @recipient AND status = 'pending'
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: GetOTPForUpdate :one
SELECT * FROM otp_verifications
WHERE id = @id AND project_id = @project_id AND environment = @environment
FOR UPDATE;

-- name: FinishOTPAttempt :one
-- Records one verification attempt. Once the verification leaves 'pending',
-- the code hash is erased.
UPDATE otp_verifications SET
    status      = @status,
    attempts    = attempts + 1,
    verified_at = CASE WHEN @status::otp_status = 'verified' THEN now() ELSE verified_at END,
    code_hash   = CASE WHEN @status::otp_status = 'pending' THEN code_hash ELSE NULL END,
    updated_at  = now()
WHERE id = @id
RETURNING *;

-- name: ExpireOTP :one
UPDATE otp_verifications SET status = 'expired', code_hash = NULL, updated_at = now()
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: ExpireOTPs :many
UPDATE otp_verifications SET status = 'expired', code_hash = NULL, updated_at = now()
WHERE status = 'pending' AND expires_at < now()
RETURNING *;

-- name: DeleteOldOTPs :execrows
DELETE FROM otp_verifications WHERE created_at < @before;

-- name: GetOTPView :one
SELECT o.*, m.status AS message_status
FROM otp_verifications o
LEFT JOIN messages m ON m.id = o.message_id
WHERE o.id = @id AND o.project_id = @project_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR o.environment = sqlc.narg(environment));

-- name: ListOTPViews :many
SELECT o.*, m.status AS message_status
FROM otp_verifications o
LEFT JOIN messages m ON m.id = o.message_id
WHERE o.project_id = @project_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR o.environment = sqlc.narg(environment))
  AND (sqlc.narg(status)::otp_status IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(recipient)::text IS NULL OR o.recipient = sqlc.narg(recipient))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (o.created_at, o.id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY o.created_at DESC, o.id DESC
LIMIT @row_limit;

-- name: OTPStats :one
SELECT count(*)::int                                     AS total,
       count(*) FILTER (WHERE status = 'verified')::int AS verified,
       count(*) FILTER (WHERE status = 'pending')::int  AS pending,
       count(*) FILTER (WHERE status = 'expired')::int  AS expired,
       count(*) FILTER (WHERE status = 'failed')::int   AS failed,
       count(*) FILTER (WHERE status = 'canceled')::int AS canceled,
       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM verified_at - created_at))
                FILTER (WHERE status = 'verified'), 0)::float8 AS median_seconds_to_verify
FROM otp_verifications
WHERE project_id = @project_id AND environment = @environment AND created_at >= @since;

-- name: GetOTPSettings :one
SELECT * FROM otp_settings WHERE project_id = $1;

-- name: UpsertOTPSettings :one
INSERT INTO otp_settings (project_id, app_name, template, code_length, ttl_seconds, max_attempts, web_otp_domain)
VALUES (@project_id, sqlc.narg(app_name), sqlc.narg(template), @code_length, @ttl_seconds, @max_attempts, sqlc.narg(web_otp_domain))
ON CONFLICT (project_id) DO UPDATE SET
    app_name       = EXCLUDED.app_name,
    template       = EXCLUDED.template,
    code_length    = EXCLUDED.code_length,
    ttl_seconds    = EXCLUDED.ttl_seconds,
    max_attempts   = EXCLUDED.max_attempts,
    web_otp_domain = EXCLUDED.web_otp_domain,
    updated_at     = now()
RETURNING *;

-- name: RedactMessage :exec
-- Erases an OTP message's body once it has left the phone.
UPDATE messages SET body = '', body_vars = NULL, body_redacted_at = now(), updated_at = now()
WHERE id = @id AND body_redacted_at IS NULL AND status IN ('sent', 'delivered', 'failed');
