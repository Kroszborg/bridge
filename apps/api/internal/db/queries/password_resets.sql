-- name: CreatePasswordReset :one
INSERT INTO password_resets (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ClaimPasswordReset :one
-- Uses the link once, in the transaction that sets the new password.
UPDATE password_resets SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ExpirePasswordResets :exec
-- A new password makes every other open link for the user useless.
UPDATE password_resets SET used_at = now() WHERE user_id = $1 AND used_at IS NULL;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;
