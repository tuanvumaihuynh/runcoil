-- name: ApiKeyGetForRegister :one
-- First statement of the Register transaction. FOR SHARE makes a later revoke wait for
-- the worker this transaction creates.
SELECT id, project_id
FROM api_keys
WHERE key_hash = @key_hash::bytea
FOR SHARE;
