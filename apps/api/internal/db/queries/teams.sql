-- name: RoleForProject :one
SELECT m.role FROM projects p
JOIN organization_members m ON m.organization_id = p.organization_id
WHERE p.id = @project_id AND m.user_id = @user_id;

-- name: RoleForOrganization :one
SELECT role FROM organization_members WHERE organization_id = @organization_id AND user_id = @user_id;

-- name: UpdateOrganizationName :one
UPDATE organizations SET name = @name, updated_at = now() WHERE id = @id RETURNING *;

-- name: ListMembers :many
SELECT m.id, m.organization_id, m.user_id, m.role, m.created_at, u.email, u.name,
       COALESCE((SELECT max(s.last_seen_at) FROM sessions s WHERE s.user_id = u.id), m.created_at)::timestamptz AS last_active_at
FROM organization_members m
JOIN users u ON u.id = m.user_id
WHERE m.organization_id = $1
ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, m.created_at, m.id;

-- name: GetMember :one
SELECT * FROM organization_members WHERE id = @id AND organization_id = @organization_id;

-- name: CountOwners :one
SELECT count(*)::int FROM organization_members WHERE organization_id = $1 AND role = 'owner';

-- name: UpdateMemberRole :one
UPDATE organization_members SET role = @role WHERE id = @id AND organization_id = @organization_id RETURNING *;

-- name: DeleteMember :execrows
DELETE FROM organization_members WHERE id = @id AND organization_id = @organization_id;

-- name: CreateInvite :one
INSERT INTO organization_invites (id, organization_id, token_hash, role, email, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListPendingInvites :many
SELECT i.*, COALESCE(u.name, '')::text AS inviter_name, COALESCE(u.email, '')::text AS inviter_email
FROM organization_invites i
LEFT JOIN users u ON u.id = i.invited_by
WHERE i.organization_id = $1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now()
ORDER BY i.created_at DESC;

-- name: RevokeInvite :execrows
UPDATE organization_invites SET revoked_at = now()
WHERE id = @id AND organization_id = @organization_id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: GetInviteByToken :one
SELECT i.*, o.name AS organization_name, COALESCE(u.name, '')::text AS inviter_name
FROM organization_invites i
JOIN organizations o ON o.id = i.organization_id
LEFT JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1;

-- name: ClaimInvite :one
-- Marks the invite used, once, in the accepting transaction.
UPDATE organization_invites SET accepted_at = now(), accepted_by = @user_id
WHERE token_hash = @token_hash AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListSessionsForUser :many
SELECT * FROM sessions WHERE user_id = $1 AND expires_at > now() ORDER BY last_seen_at DESC;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = @id AND user_id = @user_id;

-- name: DeleteOtherSessions :execrows
DELETE FROM sessions WHERE user_id = @user_id AND id <> @keep_id;

-- name: UpdateUserName :one
UPDATE users SET name = @name, updated_at = now() WHERE id = @id RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = @password_hash, updated_at = now() WHERE id = @id;

-- name: OrganizationsBlockingDeletion :many
-- Organizations the user owns alone while others are members: ownership must move first.
SELECT o.id, o.name FROM organizations o
JOIN organization_members m ON m.organization_id = o.id AND m.user_id = @user_id AND m.role = 'owner'
WHERE (SELECT count(*) FROM organization_members x WHERE x.organization_id = o.id AND x.role = 'owner') = 1
  AND (SELECT count(*) FROM organization_members x WHERE x.organization_id = o.id) > 1;

-- name: DeleteSoleMemberOrganizations :execrows
DELETE FROM organizations o
WHERE EXISTS (SELECT 1 FROM organization_members m WHERE m.organization_id = o.id AND m.user_id = @user_id)
  AND (SELECT count(*) FROM organization_members m WHERE m.organization_id = o.id) = 1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1;

-- name: CountProjects :one
SELECT count(*)::int FROM projects WHERE organization_id = $1;

-- name: ListAuditLogs :many
SELECT a.*,
       COALESCE(u.name, k.name, d.name, '')::text AS actor_name,
       COALESCE(u.email, '')::text AS actor_email,
       COALESCE(p.name, '')::text AS project_name
FROM audit_logs a
LEFT JOIN users u ON a.actor_type = 'user' AND u.id = a.actor_id
LEFT JOIN api_keys k ON a.actor_type = 'api_key' AND k.id = a.actor_id
LEFT JOIN devices d ON a.actor_type = 'device' AND d.id = a.actor_id
LEFT JOIN projects p ON p.id = a.project_id
WHERE a.organization_id = @organization_id
  AND (sqlc.narg(project_id)::text IS NULL OR a.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(action_prefix)::text IS NULL OR starts_with(a.action, sqlc.narg(action_prefix)))
  AND (sqlc.narg(actor_id)::text IS NULL OR a.actor_id = sqlc.narg(actor_id))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @row_limit;

-- name: GetAuditLog :one
SELECT * FROM audit_logs WHERE id = @id AND organization_id = @organization_id;

-- name: ProjectDevices :many
SELECT id FROM devices WHERE project_id = $1 AND revoked_at IS NULL;
