-- name: ProjectMemberListByUser :many
SELECT p.id, p.name, p.created_at, m.role
FROM project_members m
JOIN projects p ON p.id = m.project_id
WHERE m.user_id = @user_id::uuid
ORDER BY p.name;

-- name: ProjectMemberList :many
SELECT u.id, u.username, u.email, m.role
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = @project_id::uuid
ORDER BY u.username;

-- name: ProjectMemberAdd :exec
INSERT INTO project_members (project_id, user_id, role)
VALUES (@project_id::uuid, @user_id::uuid, @role::text);

-- name: ProjectMemberUpdateRole :one
WITH updated AS (
    UPDATE project_members
    SET role = @role::text
    WHERE project_id = @project_id::uuid
      AND user_id = @user_id::uuid
    RETURNING user_id, role
)
SELECT u.id, u.username, u.email, updated.role
FROM updated
JOIN users u ON u.id = updated.user_id;

-- name: ProjectMemberRemove :execrows
DELETE FROM project_members
WHERE project_id = @project_id::uuid
  AND user_id = @user_id::uuid;
