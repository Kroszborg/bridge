-- name: InsertProviderAccount :one
INSERT INTO provider_accounts (id, project_id, kind, name, priority, credentials, config, credential_hint, callback_token)
VALUES (@id, @project_id, @kind, @name, @priority, @credentials, @config, @credential_hint, @callback_token)
RETURNING *;

-- name: ListProviderAccounts :many
SELECT * FROM provider_accounts WHERE project_id = $1 ORDER BY priority, created_at;

-- name: EnabledProviderAccounts :many
SELECT * FROM provider_accounts WHERE project_id = $1 AND enabled ORDER BY priority, created_at;

-- name: GetProviderAccount :one
SELECT * FROM provider_accounts WHERE id = @id AND project_id = @project_id;

-- name: GetProviderAccountByID :one
SELECT * FROM provider_accounts WHERE id = $1;

-- name: UpdateProviderAccount :one
UPDATE provider_accounts SET
    name            = COALESCE(sqlc.narg(name), name),
    enabled         = COALESCE(sqlc.narg(enabled), enabled),
    priority        = COALESCE(sqlc.narg(priority), priority),
    credentials     = COALESCE(sqlc.narg(credentials), credentials),
    credential_hint = COALESCE(sqlc.narg(credential_hint), credential_hint),
    config          = COALESCE(sqlc.narg(config), config),
    updated_at      = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteProviderAccount :execrows
DELETE FROM provider_accounts WHERE id = @id AND project_id = @project_id;

-- name: RecordProviderResult :exec
UPDATE provider_accounts SET
    last_used_at = CASE WHEN sqlc.narg(error)::text IS NULL THEN now() ELSE last_used_at END,
    last_error   = sqlc.narg(error),
    updated_at   = now()
WHERE id = @id;

-- name: GetRouting :one
SELECT * FROM project_routing WHERE project_id = $1;

-- name: UpsertRouting :one
INSERT INTO project_routing (project_id, mode, fallback_after_seconds)
VALUES (@project_id, @mode, @fallback_after_seconds)
ON CONFLICT (project_id) DO UPDATE SET
    mode = EXCLUDED.mode, fallback_after_seconds = EXCLUDED.fallback_after_seconds, updated_at = now()
RETURNING *;

-- name: SetMessageProvider :one
-- Records which provider took a message, and its ID there.
UPDATE messages SET
    provider            = @provider,
    provider_account_id = @provider_account_id,
    provider_message_id = @provider_message_id,
    device_id           = NULL,
    updated_at          = now()
WHERE id = @id
RETURNING *;

-- name: GetMessageByProviderID :one
SELECT * FROM messages WHERE provider_account_id = @provider_account_id AND provider_message_id = @provider_message_id;
