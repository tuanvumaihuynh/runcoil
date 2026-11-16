-- name: ApiKeyGetForRegister :one
SELECT id, project_id
FROM api_keys
WHERE key_hash = @key_hash::bytea
  AND revoked_at IS NULL;

-- name: ApiKeyList :many
SELECT k.id, k.name, k.key_hint, u.username AS created_by, k.created_at, k.last_used_at, k.revoked_at
FROM api_keys k
JOIN users u ON u.id = k.created_by
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
JOIN users u ON u.id = created.created_by;

-- name: ApiKeyRevoke :one
-- A second revoke keeps the first time.
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, now())
WHERE project_id = @project_id::uuid
  AND id = @id::uuid
RETURNING id;

-- name: ApiKeyTouch :exec
-- Locks the keys in ID order, so two servers flushing the same keys cannot deadlock.
WITH used AS (
    SELECT unnest(@ids::uuid[]) AS id, unnest(@used_at::timestamptz[]) AS used_at
), locked AS (
    SELECT id FROM api_keys
    WHERE id IN (SELECT id FROM used) AND revoked_at IS NULL
    ORDER BY id
    FOR NO KEY UPDATE
)
UPDATE api_keys k
SET last_used_at = GREATEST(k.last_used_at, used.used_at)
FROM used
WHERE k.id = used.id
  AND k.id IN (SELECT id FROM locked);
