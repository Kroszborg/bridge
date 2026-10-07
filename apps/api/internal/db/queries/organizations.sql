-- name: CreateOrganization :one
INSERT INTO organizations (id, name, slug)
VALUES ($1, $2, $3)
RETURNING *;

-- name: OrganizationSlugExists :one
SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1);

-- name: AddOrganizationMember :one
INSERT INTO organization_members (id, organization_id, user_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListOrganizationsForUser :many
SELECT o.id, o.name, o.slug, o.created_at, o.updated_at, m.role
FROM organizations o
JOIN organization_members m ON m.organization_id = o.id
WHERE m.user_id = $1
ORDER BY o.created_at, o.id;

-- name: GetOrganizationForUser :one
SELECT o.id, o.name, o.slug, o.created_at, o.updated_at, m.role
FROM organizations o
JOIN organization_members m ON m.organization_id = o.id
WHERE o.id = @organization_id AND m.user_id = @user_id;
