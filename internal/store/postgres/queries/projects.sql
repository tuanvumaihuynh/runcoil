-- name: ProjectCreateDefault :exec
INSERT INTO projects (id, name)
VALUES (@id::uuid, 'default')
ON CONFLICT (name) DO NOTHING;

-- name: ProjectList :many
SELECT id, name, created_at
FROM projects
ORDER BY name;

-- name: ProjectCreate :one
INSERT INTO projects (id, name)
VALUES (@id::uuid, @name::text)
RETURNING id, name, created_at;

-- name: ProjectGetRoleOfUser :one
SELECT p.id, m.role
FROM projects p
LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = @user_id::uuid
WHERE p.id = @project_id::uuid;
