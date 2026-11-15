-- name: RunMarkStarted :exec
UPDATE runs
SET status = 'running', started_at = now()
WHERE id = ANY (@ids::uuid[]) AND status = 'queued';

-- name: RunListForAssign :many
SELECT r.id, t.name AS task_name, r.input, r.created_at
FROM runs r
JOIN tasks t ON t.id = r.task_id
WHERE r.id = ANY (@ids::uuid[]);

-- name: RunFinish :one
-- Ends the attempt that holds the lease. No row: the lease is not this attempt's, or the
-- Run already ended.
WITH released AS (
    DELETE FROM run_leases
    WHERE run_id = @run_id::uuid AND lease_epoch = @lease_epoch::bigint AND worker_id = @worker_id::uuid
    RETURNING run_id
)
UPDATE runs r
SET status        = @status::text,
    output        = sqlc.narg(output)::bytea,
    error_code    = sqlc.narg(error_code)::text,
    error_message = sqlc.narg(error_message)::text,
    ended_at      = now()
FROM released
WHERE r.id = released.run_id
RETURNING r.id;

-- name: RunGetStatus :one
SELECT status FROM runs WHERE id = @id::uuid;

-- name: RunListActive :many
-- The Runs among @ids that are queued or running. FireDueSchedules reads the last Runs of
-- the schedules it locked with it, in a statement of its own, so its snapshot sees every
-- Run committed before the lock.
SELECT id FROM runs WHERE id = ANY (@ids::uuid[]) AND status IN ('queued', 'running');

-- name: RunCreateScheduled :exec
-- One queued Run per schedule tick, with no input. config keeps the Task's retry policy,
-- and the queue row its start deadline, as they are now.
WITH created AS (
    INSERT INTO runs (id, created_at, project_id, task_id, status, config)
    SELECT v.id, now(), t.project_id, t.id, 'queued', jsonb_build_object('retry', t.retry_policy)
    FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@task_ids::uuid[]) AS task_id) AS v
    JOIN tasks t ON t.id = v.task_id
    RETURNING id, project_id, task_id
)
INSERT INTO run_queue (run_id, project_id, task_id, ready_at, start_deadline_at)
SELECT c.id, c.project_id, c.task_id, now(), now() + t.start_deadline
FROM created c
JOIN tasks t ON t.id = c.task_id;
