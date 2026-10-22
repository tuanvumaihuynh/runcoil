-- name: IdempotencyInsert :execrows
INSERT INTO idempotency_keys (
	kind,
	idempotency_key,
	fingerprint,
	run_id,
	created_at,
	expires_at
) VALUES (
	'caller',
	@idempotency_key,
	@fingerprint,
	@run_id,
	@created_at,
	@expires_at
)
ON CONFLICT (kind, idempotency_key) DO UPDATE
SET fingerprint = EXCLUDED.fingerprint,
	run_id      = EXCLUDED.run_id,
	created_at  = EXCLUDED.created_at,
	expires_at  = EXCLUDED.expires_at
WHERE idempotency_keys.expires_at <= EXCLUDED.created_at;

-- name: IdempotencyGet :one
SELECT run_id, fingerprint FROM idempotency_keys
WHERE kind = 'caller' AND idempotency_key = @idempotency_key AND expires_at > @now;

-- name: RunGetByIdempotencyKey :one
SELECT r.* FROM runs AS r
JOIN idempotency_keys AS k ON k.run_id = r.id
WHERE k.kind = 'caller' AND k.idempotency_key = @idempotency_key AND k.expires_at > @now;

-- name: IdempotencyPruneExpired :execrows
WITH victims AS (
	SELECT k.kind, k.idempotency_key FROM idempotency_keys AS k
	WHERE k.expires_at <= @now
	LIMIT @row_limit
)
DELETE FROM idempotency_keys AS d
USING victims AS v
WHERE d.kind = v.kind AND d.idempotency_key = v.idempotency_key
	-- Checked on the locked row: a key that IdempotencyInsert renewed while this waits is kept.
	AND d.expires_at <= @now;
