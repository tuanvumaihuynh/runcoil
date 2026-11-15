-- name: ProjectCreateDefault :exec
INSERT INTO projects (id, name)
VALUES (@id::uuid, 'default')
ON CONFLICT (name) DO NOTHING;

-- name: ProjectList :many
SELECT id, name
FROM projects
ORDER BY name;
