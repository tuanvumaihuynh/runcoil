-- name: WorkerCreate :exec
INSERT INTO workers (id, project_id, api_key_id, token_hash, app, task_ids, slots)
VALUES (@id::uuid, @project_id::uuid, @api_key_id::uuid, @token_hash::bytea, @app::text,
        @task_ids::uuid[], @slots::integer);

-- name: WorkerGetBySessionToken :one
SELECT id, project_id, app, task_ids, slots
FROM workers
WHERE token_hash = @token_hash::bytea
  AND ended_at IS NULL;

-- name: WorkerAttachStream :one
-- FOR UPDATE, not the UPDATE's own lock: only FOR UPDATE conflicts with the claim's
-- FOR KEY SHARE, so a claim in flight on this worker commits first. A worker that opens a
-- stream is alive, so the attach counts as a heartbeat: the reaper does not end a worker
-- that moved to this server before the server's first flush.
UPDATE workers
SET dispatcher_id = @dispatcher_id::uuid, stream_id = @stream_id::uuid, heartbeat_at = now()
WHERE id IN (
    SELECT id FROM workers
    WHERE id = @id::uuid AND ended_at IS NULL
    FOR UPDATE
)
RETURNING id, task_ids, slots;

-- name: WorkerLockForClaim :one
-- FOR KEY SHARE conflicts only with FOR UPDATE: a worker moving to another stream either
-- waits for this claim to commit and then reconciles its leases, or makes this fence fail.
-- The heartbeat flush is a plain UPDATE and does not wait.
SELECT id
FROM workers
WHERE id = @id::uuid
  AND dispatcher_id = @dispatcher_id::uuid
  AND stream_id = @stream_id::uuid
  AND ended_at IS NULL
FOR KEY SHARE;

-- name: WorkerFlushHeartbeats :exec
UPDATE workers w
SET heartbeat_at = GREATEST(w.heartbeat_at, v.ts)
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@beats::timestamptz[]) AS ts) AS v
WHERE w.id = v.id AND w.ended_at IS NULL;

-- name: WorkerDeregister :one
-- FOR UPDATE: a claim in flight on this worker commits first, then its fence fails.
UPDATE workers
SET ended_at = now(), end_reason = 'deregistered', stream_id = NULL
WHERE id IN (
    SELECT id FROM workers
    WHERE id = @id::uuid AND ended_at IS NULL
    FOR UPDATE
)
RETURNING id;

-- name: WorkerEndDead :many
-- Ends at most @batch_size sessions with no heartbeat for longer than @ttl. SKIP LOCKED: a
-- worker with a claim or a Listen in flight is taken on a later sweep.
UPDATE workers
SET ended_at = now(), end_reason = 'heartbeat_timeout', stream_id = NULL
WHERE id IN (
    SELECT id FROM workers
    WHERE ended_at IS NULL AND heartbeat_at < now() - @ttl::interval
    ORDER BY heartbeat_at
    LIMIT @batch_size::integer
    FOR UPDATE SKIP LOCKED
)
RETURNING id;
