-- name: CreateProject :one
INSERT INTO projects (id, organization_id, name, slug)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ProjectSlugExists :one
SELECT EXISTS (SELECT 1 FROM projects WHERE organization_id = $1 AND slug = $2);

-- name: ListProjects :many
SELECT * FROM projects WHERE organization_id = $1 ORDER BY created_at, id;

-- name: GetProjectForUser :one
SELECT p.id, p.organization_id, p.name, p.slug, p.created_at, p.updated_at, m.role
FROM projects p
JOIN organization_members m ON m.organization_id = p.organization_id
WHERE p.id = @project_id AND m.user_id = @user_id;

-- name: UpdateProjectName :one
UPDATE projects SET name = @name, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;
