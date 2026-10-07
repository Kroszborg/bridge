-- name: InsertOTP :one
INSERT INTO otp_verifications (
    id, project_id, app_id, environment, api_key_id, recipient, code_hash, test_code,
    code_length, max_attempts, message_id, metadata, expires_at
) VALUES (
    @id, @project_id, @app_id::text, @environment, sqlc.narg(api_key_id), @recipient, @code_hash, sqlc.narg(test_code),
    @code_length, @max_attempts, @message_id, @metadata, @expires_at
)
RETURNING *;

-- name: CancelPendingOTPs :exec
-- A new code replaces any code of the same app still pending for the same number.
UPDATE otp_verifications SET status = 'canceled', code_hash = NULL, updated_at = now()
WHERE project_id = @project_id AND app_id = @app_id::text AND environment = @environment
  AND recipient = @recipient AND status = 'pending';

-- name: LatestOTPForRecipient :one
SELECT * FROM otp_verifications
WHERE project_id = @project_id AND app_id = @app_id::text AND environment = @environment AND recipient = @recipient
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LatestPendingOTPForUpdate :one
SELECT * FROM otp_verifications
WHERE project_id = @project_id AND environment = @environment AND recipient = @recipient AND status = 'pending'
  AND (sqlc.narg(app_id)::text IS NULL OR app_id = sqlc.narg(app_id))
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: GetOTPForUpdate :one
SELECT * FROM otp_verifications
WHERE id = @id AND project_id = @project_id AND environment = @environment
  AND (sqlc.narg(app_id)::text IS NULL OR app_id = sqlc.narg(app_id))
FOR UPDATE;

-- name: GetOTPByID :one
SELECT * FROM otp_verifications WHERE id = $1;

-- name: SetOTPFailover :execrows
-- Claims a verification's one failover; a second claim changes nothing.
UPDATE otp_verifications SET failover_message_id = @failover_message_id::text, updated_at = now()
WHERE id = @id AND failover_message_id IS NULL AND status = 'pending';

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
SELECT o.*, m.status AS message_status, fm.status AS failover_message_status
FROM otp_verifications o
LEFT JOIN messages m ON m.id = o.message_id
LEFT JOIN messages fm ON fm.id = o.failover_message_id
WHERE o.id = @id AND o.project_id = @project_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR o.environment = sqlc.narg(environment));

-- name: ListOTPViews :many
SELECT o.*, m.status AS message_status, fm.status AS failover_message_status
FROM otp_verifications o
LEFT JOIN messages m ON m.id = o.message_id
LEFT JOIN messages fm ON fm.id = o.failover_message_id
WHERE o.project_id = @project_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR o.environment = sqlc.narg(environment))
  AND (sqlc.narg(app_id)::text IS NULL OR o.app_id = sqlc.narg(app_id))
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
       count(*) FILTER (WHERE failover_message_id IS NOT NULL)::int AS failovers,
       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM verified_at - created_at))
                FILTER (WHERE status = 'verified'), 0)::float8 AS median_seconds_to_verify
FROM otp_verifications
WHERE project_id = @project_id AND environment = @environment AND created_at >= @since
  AND (sqlc.narg(app_id)::text IS NULL OR app_id = sqlc.narg(app_id));

-- name: RedactMessage :exec
-- Erases an OTP message's body once it has left the phone.
UPDATE messages SET body = '', body_vars = NULL, body_redacted_at = now(), updated_at = now()
WHERE id = @id AND body_redacted_at IS NULL AND status IN ('sent', 'delivered', 'failed');

-- name: InsertVerifyApp :one
INSERT INTO verify_apps (
    id, project_id, slug, name, app_name, template, code_length, ttl_seconds, max_attempts, web_otp_domain,
    failover_after_seconds, allowed_countries, ip_hourly_limit, range_hourly_limit, country_hourly_limit,
    publishable_key, allowed_origins, redirect_uris, widget_environment, turnstile_site_key, turnstile_secret, secret
) VALUES (
    @id, @project_id, @slug, @name, sqlc.narg(app_name), sqlc.narg(template), @code_length, @ttl_seconds, @max_attempts, sqlc.narg(web_otp_domain),
    @failover_after_seconds, @allowed_countries, @ip_hourly_limit, @range_hourly_limit, sqlc.narg(country_hourly_limit),
    @publishable_key, @allowed_origins, @redirect_uris, @widget_environment, sqlc.narg(turnstile_site_key), sqlc.narg(turnstile_secret), sqlc.narg(secret)
)
RETURNING *;

-- name: InsertDefaultVerifyApp :exec
-- Creates the project's default app unless another request just did.
INSERT INTO verify_apps (id, project_id, slug, name, publishable_key, secret)
VALUES (@id, @project_id, 'default', 'Default', @publishable_key, sqlc.narg(secret))
ON CONFLICT (project_id, slug) DO NOTHING;

-- name: GetVerifyApp :one
SELECT * FROM verify_apps WHERE id = @id AND project_id = @project_id;

-- name: GetVerifyAppBySlug :one
SELECT * FROM verify_apps WHERE project_id = @project_id AND slug = @slug;

-- name: GetVerifyAppByID :one
SELECT * FROM verify_apps WHERE id = $1;

-- name: GetVerifyAppByPublishableKey :one
SELECT * FROM verify_apps WHERE publishable_key = $1;

-- name: ListVerifyApps :many
SELECT * FROM verify_apps WHERE project_id = $1 ORDER BY (slug = 'default') DESC, created_at, id;

-- name: VerifyAppSlugExists :one
SELECT EXISTS (SELECT 1 FROM verify_apps WHERE project_id = @project_id AND slug = @slug);

-- name: UpdateVerifyApp :one
UPDATE verify_apps SET
    name                   = @name,
    app_name               = sqlc.narg(app_name),
    template               = sqlc.narg(template),
    code_length            = @code_length,
    ttl_seconds            = @ttl_seconds,
    max_attempts           = @max_attempts,
    web_otp_domain         = sqlc.narg(web_otp_domain),
    failover_after_seconds = @failover_after_seconds,
    allowed_countries      = @allowed_countries,
    ip_hourly_limit        = @ip_hourly_limit,
    range_hourly_limit     = @range_hourly_limit,
    country_hourly_limit   = sqlc.narg(country_hourly_limit),
    allowed_origins        = @allowed_origins,
    redirect_uris          = @redirect_uris,
    widget_environment     = @widget_environment,
    turnstile_site_key     = sqlc.narg(turnstile_site_key),
    turnstile_secret       = sqlc.narg(turnstile_secret),
    updated_at             = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: SetVerifyAppSecret :one
UPDATE verify_apps SET secret = @secret, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: SetVerifyAppSecretIfMissing :execrows
UPDATE verify_apps SET secret = @secret, updated_at = now()
WHERE id = @id AND secret IS NULL;

-- name: DeleteVerifyApp :execrows
-- The default app cannot be deleted.
DELETE FROM verify_apps WHERE id = @id AND project_id = @project_id AND slug <> 'default';

-- name: InsertOTPBlock :one
INSERT INTO otp_blocks (id, project_id, app_id, environment, recipient, client_ip, country, reason)
VALUES (@id, @project_id, @app_id, @environment, @recipient, sqlc.narg(client_ip), sqlc.narg(country), @reason)
RETURNING *;

-- name: GetOTPBlock :one
SELECT * FROM otp_blocks WHERE id = @id AND app_id = @app_id;

-- name: ListOTPBlocks :many
SELECT * FROM otp_blocks
WHERE app_id = @app_id
  AND (sqlc.narg(environment)::api_environment IS NULL OR environment = sqlc.narg(environment))
  AND (sqlc.narg(reason)::text IS NULL OR reason = sqlc.narg(reason))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: OTPBlockCounts :many
SELECT reason, count(*)::int AS blocks
FROM otp_blocks
WHERE project_id = @project_id AND environment = @environment AND created_at >= @since
  AND (sqlc.narg(app_id)::text IS NULL OR app_id = sqlc.narg(app_id))
GROUP BY reason;

-- name: DeleteOldOTPBlocks :execrows
DELETE FROM otp_blocks WHERE created_at < @before;
