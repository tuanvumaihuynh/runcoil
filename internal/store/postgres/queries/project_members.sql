-- name: ProjectMemberListByUser :many
SELECT p.id, p.name, m.role
FROM project_members m
JOIN projects p ON p.id = m.project_id
WHERE m.user_id = @user_id::uuid
ORDER BY p.name;
