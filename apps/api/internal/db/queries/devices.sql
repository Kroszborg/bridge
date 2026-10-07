-- name: CreatePairingToken :one
INSERT INTO device_pairing_tokens (id, project_id, token_hash, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ClaimPairingToken :one
-- Locks a valid, unused token for the pairing transaction.
SELECT * FROM device_pairing_tokens
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
FOR UPDATE;

-- name: MarkPairingTokenUsed :exec
UPDATE device_pairing_tokens SET used_at = now(), device_id = @device_id WHERE id = @id;

-- name: GetDeviceByInstallation :one
SELECT * FROM devices WHERE project_id = $1 AND installation_id = $2;

-- name: CreateDevice :one
INSERT INTO devices (id, project_id, name, installation_id, credential_hash, device_model, android_version, app_version, app_flavor)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: RepairDevice :one
-- Re-pairing the same installation rotates its credential and reactivates it.
UPDATE devices SET
    credential_hash = @credential_hash,
    name = @name,
    device_model = @device_model,
    android_version = @android_version,
    app_version = @app_version,
    app_flavor = @app_flavor,
    revoked_at = NULL,
    status = 'offline',
    connection_id = NULL,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetDeviceForAuth :one
SELECT sqlc.embed(devices), projects.organization_id, projects.name AS project_name
FROM devices
JOIN projects ON projects.id = devices.project_id
WHERE devices.credential_hash = $1;

-- name: GetDevice :one
SELECT * FROM devices WHERE id = @id AND project_id = @project_id;

-- name: ListDevices :many
SELECT * FROM devices
WHERE project_id = $1
ORDER BY (revoked_at IS NOT NULL), created_at DESC, id DESC;

-- name: RenameDevice :one
UPDATE devices SET name = @name, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: RevokeDevice :one
UPDATE devices SET revoked_at = now(), status = 'disabled', connection_id = NULL, updated_at = now()
WHERE id = @id AND project_id = @project_id AND revoked_at IS NULL
RETURNING *;

-- name: MarkDeviceConnected :exec
UPDATE devices SET
    status = 'online', connection_id = @connection_id, connected_at = now(), last_seen_at = now(), updated_at = now()
WHERE id = @id AND revoked_at IS NULL;

-- name: MarkDeviceDisconnected :execrows
-- Only the connection that currently owns the device may mark it offline.
UPDATE devices SET status = 'offline', connection_id = NULL, last_seen_at = now(), updated_at = now()
WHERE id = @id AND connection_id = @connection_id;

-- name: RecordHeartbeat :exec
UPDATE devices SET
    last_heartbeat_at = now(),
    last_seen_at = now(),
    heartbeat_interval = @heartbeat_interval,
    battery_level = @battery_level,
    is_charging = @is_charging,
    network_type = @network_type,
    carrier_name = @carrier_name,
    sim_count = @sim_count,
    device_model = COALESCE(@device_model, device_model),
    android_version = COALESCE(@android_version, android_version),
    app_version = COALESCE(@app_version, app_version),
    sims = COALESCE(sqlc.narg(sims)::jsonb, sims),
    updated_at = now()
WHERE id = @id AND revoked_at IS NULL;

-- name: UpdateDevicePush :exec
UPDATE devices SET
    push_provider = @push_provider,
    push_endpoint = @push_endpoint,
    push_p256dh = @push_p256dh,
    push_auth = @push_auth,
    push_updated_at = now(),
    updated_at = now()
WHERE id = @id AND revoked_at IS NULL;

-- name: SweepStaleDevices :execrows
-- Marks devices offline whose connection vanished without a clean close
-- (for example an API instance crash).
UPDATE devices SET status = 'offline', connection_id = NULL, updated_at = now()
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => heartbeat_interval * 3 + 60);

-- name: GetServerKey :one
SELECT private_key FROM server_keys WHERE name = $1;

-- name: InsertServerKey :exec
INSERT INTO server_keys (name, private_key) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING;

-- name: GetDeviceByID :one
SELECT * FROM devices WHERE id = $1;
