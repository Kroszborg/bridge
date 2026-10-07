-- name: InsertAuditLog :exec
INSERT INTO audit_logs (id, organization_id, project_id, actor_type, actor_id, action, target_type, target_id, metadata, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListAuditLogsForProject :many
SELECT * FROM audit_logs
WHERE project_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;
