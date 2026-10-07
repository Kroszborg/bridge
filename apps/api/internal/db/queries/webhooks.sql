-- name: CreateWebhookEndpoint :one
INSERT INTO webhook_endpoints (id, project_id, url, description, events, secret)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListWebhookEndpoints :many
SELECT * FROM webhook_endpoints WHERE project_id = $1 ORDER BY created_at DESC, id DESC;

-- name: GetWebhookEndpoint :one
SELECT * FROM webhook_endpoints WHERE id = @id AND project_id = @project_id;

-- name: GetWebhookEndpointByID :one
SELECT * FROM webhook_endpoints WHERE id = $1;

-- name: UpdateWebhookEndpoint :one
UPDATE webhook_endpoints SET
    url = COALESCE(sqlc.narg(url)::text, url),
    description = COALESCE(sqlc.narg(description)::text, description),
    events = COALESCE(sqlc.narg(events)::text[], events),
    enabled = COALESCE(sqlc.narg(enabled)::bool, enabled),
    disabled_reason = CASE WHEN sqlc.narg(enabled)::bool THEN NULL ELSE disabled_reason END,
    failing_since = CASE WHEN sqlc.narg(enabled)::bool THEN NULL ELSE failing_since END,
    updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteWebhookEndpoint :execrows
DELETE FROM webhook_endpoints WHERE id = @id AND project_id = @project_id;

-- name: SubscribedEndpoints :many
SELECT id FROM webhook_endpoints
WHERE project_id = @project_id AND enabled
  AND (cardinality(events) = 0 OR @event_type::text = ANY(events));

-- name: InsertWebhookEvent :one
INSERT INTO webhook_events (id, project_id, type, payload) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetWebhookEvent :one
SELECT * FROM webhook_events WHERE id = $1;

-- name: InsertWebhookDelivery :exec
INSERT INTO webhook_deliveries (id, endpoint_id, event_id, attempt, succeeded, response_status, response_body, error, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListWebhookDeliveries :many
SELECT d.*, e.type AS event_type
FROM webhook_deliveries d
JOIN webhook_events e ON e.id = d.event_id
WHERE d.endpoint_id = $1
ORDER BY d.created_at DESC, d.id DESC
LIMIT $2;

-- name: MarkWebhookSuccess :exec
UPDATE webhook_endpoints SET last_success_at = now(), failing_since = NULL, updated_at = now() WHERE id = $1;

-- name: MarkWebhookFailure :one
UPDATE webhook_endpoints SET
    last_failure_at = now(),
    failing_since = COALESCE(failing_since, now()),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DisableWebhookEndpoint :exec
UPDATE webhook_endpoints SET enabled = false, disabled_reason = $2, updated_at = now() WHERE id = $1;

-- name: InsertInboundMessage :one
INSERT INTO messages (
    id, project_id, environment, direction, status, provider, device_id, recipient, sender, body,
    segments, encoding, idempotency_key, sim_slot, body_sha256, body_length, metadata
) VALUES (
    @id, @project_id, 'live', 'inbound', 'received', 'android', @device_id, '', @sender, @body,
    @segments, @encoding, @idempotency_key, sqlc.narg(sim_slot), @body_sha256, @body_length, @metadata
)
ON CONFLICT (project_id, idempotency_key) DO NOTHING
RETURNING *;

-- name: DeleteOldWebhookEvents :execrows
DELETE FROM webhook_events WHERE created_at < @before;

-- name: PresenceChanges :many
-- Devices whose presence differs from the last device.* webhook. Offline is
-- reported only after a grace period so a quick reconnect is not an event.
SELECT * FROM devices
WHERE revoked_at IS NULL
  AND ((status = 'online' AND notified_presence IS DISTINCT FROM 'online')
    OR (status <> 'online' AND notified_presence = 'online' AND last_seen_at < @offline_before))
LIMIT 500;

-- name: SetNotifiedPresence :execrows
UPDATE devices SET notified_presence = @presence
WHERE id = @id AND notified_presence IS DISTINCT FROM @presence;

-- name: RotateWebhookSecret :one
UPDATE webhook_endpoints SET secret = @secret, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;
