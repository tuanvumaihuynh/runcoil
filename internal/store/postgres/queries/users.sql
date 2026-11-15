-- name: UserCreateFirstServerAdmin :execrows
INSERT INTO users (id, username, password_hash, is_server_admin)
SELECT @id::uuid, @username::text, @password_hash::text, true
WHERE NOT EXISTS (SELECT 1 FROM users)
ON CONFLICT (username) DO NOTHING;

-- name: UserGetByUsername :one
SELECT id, username, email, password_hash, is_server_admin, disabled_at, created_at
FROM users
WHERE username = @username::text;

-- name: UserGetByEmail :one
SELECT id, username, email, password_hash, is_server_admin, disabled_at, created_at
FROM users
WHERE email = @email::text;

-- name: UserUpdatePassword :execrows
UPDATE users
SET password_hash = @new_password_hash::text
WHERE id = @id::uuid
  AND password_hash = @current_password_hash::text;

-- name: UserLockForUpdate :one
SELECT id
FROM users
WHERE id = @id::uuid
FOR UPDATE;

-- name: UserGetByID :one
SELECT id, username, email, password_hash, is_server_admin, disabled_at, created_at
FROM users
WHERE id = @id::uuid;

-- name: UserCreate :one
INSERT INTO users (id, username, email, password_hash, is_server_admin)
VALUES (@id::uuid, @username::text, sqlc.narg(email)::text, @password_hash::text, @is_server_admin::boolean)
RETURNING id, username, email, password_hash, is_server_admin, disabled_at, created_at;

-- name: UserList :many
SELECT id, username, email, password_hash, is_server_admin, disabled_at, created_at
FROM users
WHERE sqlc.narg(after_id)::uuid IS NULL
   OR id > sqlc.narg(after_id)::uuid
ORDER BY id
LIMIT @page_limit::integer;

-- name: UserLockActiveServerAdmins :many
SELECT id
FROM users
WHERE is_server_admin
  AND disabled_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: UserUpdate :one
UPDATE users
SET email = CASE WHEN @set_email::boolean THEN sqlc.narg(email)::text ELSE email END,
    is_server_admin = COALESCE(sqlc.narg(is_server_admin)::boolean, is_server_admin),
    disabled_at = CASE
        WHEN sqlc.narg(disabled)::boolean IS NULL THEN disabled_at
        WHEN sqlc.narg(disabled)::boolean THEN COALESCE(disabled_at, now())
        ELSE NULL
    END
WHERE id = @id::uuid
RETURNING id, username, email, password_hash, is_server_admin, disabled_at, created_at;

-- name: UserSetPassword :execrows
UPDATE users
SET password_hash = @password_hash::text
WHERE id = @id::uuid;
