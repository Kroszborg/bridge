-- name: CreateEmailVerification :one
INSERT INTO email_verifications (id, user_id, purpose, email, code_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: LatestEmailVerification :one
-- The newest code of a purpose sent to the user, for the resend cooldown.
SELECT * FROM email_verifications
WHERE user_id = @user_id AND purpose = @purpose
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ExpireEmailVerifications :exec
-- A new code makes the open codes of its purpose useless.
UPDATE email_verifications SET consumed_at = now()
WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL;

-- name: ExpireAllEmailVerifications :exec
-- A changed address makes every open code useless.
UPDATE email_verifications SET consumed_at = now() WHERE user_id = $1 AND consumed_at IS NULL;

-- name: PendingEmailVerificationForUpdate :one
SELECT * FROM email_verifications
WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: RecordEmailVerificationAttempt :one
-- Counts a wrong code; the last allowed attempt also uses the code up.
UPDATE email_verifications
SET attempts = attempts + 1,
    consumed_at = CASE WHEN attempts + 1 >= @max_attempts::integer THEN now() ELSE consumed_at END
WHERE id = @id
RETURNING *;

-- name: ConsumeEmailVerification :exec
UPDATE email_verifications SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL;

-- name: DeleteOldEmailVerifications :execrows
DELETE FROM email_verifications WHERE expires_at < @before;
