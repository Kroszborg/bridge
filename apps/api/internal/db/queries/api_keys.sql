-- name: CreateAPIKey :one
INSERT INTO api_keys (id, project_id, name, environment, key_prefix, key_hash, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAPIKeys :many
SELECT * FROM api_keys
WHERE project_id = $1
ORDER BY (revoked_at IS NOT NULL), created_at DESC, id DESC;

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = @id AND project_id = @project_id;

-- name: RevokeAPIKey :one
UPDATE api_keys SET revoked_at = now()
WHERE id = @id AND project_id = @project_id AND revoked_at IS NULL
RETURNING *;

-- name: GetAPIKeyForAuth :one
SELECT k.id, k.project_id, k.name, k.environment, k.expires_at, k.revoked_at, k.last_used_at,
       p.organization_id, p.name AS project_name
FROM api_keys k
JOIN projects p ON p.id = k.project_id
WHERE k.key_hash = $1;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;
