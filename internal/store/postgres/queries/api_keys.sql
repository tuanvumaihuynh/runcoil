-- name: ApiKeyGetForRegister :one
-- First statement of the Register transaction. FOR SHARE makes a later revoke wait for
-- the worker this transaction creates; a revoke that commits first leaves no row.
SELECT id, project_id
FROM api_keys
WHERE key_hash = @key_hash::bytea
  AND revoked_at IS NULL
FOR SHARE;

-- name: ApiKeyTouch :exec
-- After the Register transaction, so two Registers of one key do not wait for each other.
UPDATE api_keys
SET last_used_at = now()
WHERE key_hash = @key_hash::bytea
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ApiKeyList :many
SELECT k.id, k.name, k.key_hint, u.username AS created_by, k.created_at, k.last_used_at, k.revoked_at
FROM api_keys k
LEFT JOIN users u ON u.id = k.created_by
WHERE k.project_id = @project_id::uuid
ORDER BY k.id DESC;

-- name: ApiKeyCreate :one
WITH created AS (
    INSERT INTO api_keys (id, project_id, name, key_hash, key_hint, created_by)
    VALUES (@id::uuid, @project_id::uuid, @name::text, @key_hash::bytea, @key_hint::text,
            @created_by::uuid)
    RETURNING id, name, key_hint, created_by, created_at, last_used_at, revoked_at
)
SELECT created.id, created.name, created.key_hint, u.username AS created_by, created.created_at,
       created.last_used_at, created.revoked_at
FROM created
LEFT JOIN users u ON u.id = created.created_by;

-- name: ApiKeyRevoke :one
-- A second revoke keeps the first time.
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, now())
WHERE project_id = @project_id::uuid
  AND id = @id::uuid
RETURNING id;
