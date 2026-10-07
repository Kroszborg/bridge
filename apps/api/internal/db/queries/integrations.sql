-- name: InsertIntegration :one
INSERT INTO integrations (id, project_id, kind, environment)
VALUES (@id, @project_id, @kind, @environment)
RETURNING *;

-- name: ListIntegrations :many
SELECT * FROM integrations WHERE project_id = $1 ORDER BY created_at;

-- name: GetIntegration :one
SELECT * FROM integrations WHERE id = @id AND project_id = @project_id;

-- name: GetIntegrationByID :one
SELECT * FROM integrations WHERE id = $1;

-- name: SetIntegrationSecret :one
UPDATE integrations SET secret = @secret, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: SetIntegrationEnvironment :one
UPDATE integrations SET environment = @environment, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteIntegration :execrows
DELETE FROM integrations WHERE id = @id AND project_id = @project_id;

-- name: RecordIntegrationUse :exec
UPDATE integrations SET
    last_used_at = now(),
    last_error   = sqlc.narg(error),
    updated_at   = now()
WHERE id = @id;
