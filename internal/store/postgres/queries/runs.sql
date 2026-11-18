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

-- name: RunList :many
SELECT r.id, r.status, r.task_id, t.app AS task_app, t.name AS task_name, r.error_code,
       r.created_at, r.started_at, r.ended_at
FROM runs r
JOIN tasks t ON t.id = r.task_id
WHERE r.project_id = @project_id::uuid
  AND (cardinality(@statuses::text[]) = 0 OR r.status = ANY (@statuses::text[]))
  AND (sqlc.narg(task_id)::uuid IS NULL OR r.task_id = sqlc.narg(task_id)::uuid)
  AND (sqlc.narg(after_id)::uuid IS NULL OR r.id < sqlc.narg(after_id)::uuid)
ORDER BY r.id DESC
LIMIT @page_limit::integer;

-- name: RunGetWithAttempt :one
-- One statement, so the Run is seen in at most one of run_queue, run_delayed and
-- run_leases.
SELECT r.id, r.status, r.task_id, t.app AS task_app, t.name AS task_name, r.input, r.config,
       r.output, r.error_code, r.error_message, r.created_at, r.started_at, r.ended_at,
       q.ready_at AS queue_ready_at, q.retry_step_id AS queue_retry_step_id,
       q.retry_count AS queue_retry_count, q.start_deadline_at AS queue_start_deadline_at,
       d.ready_at AS delayed_ready_at, d.retry_step_id AS delayed_retry_step_id,
       d.retry_count AS delayed_retry_count, d.start_deadline_at AS delayed_start_deadline_at,
       l.worker_id AS lease_worker_id, l.started_at AS lease_started_at
FROM runs r
JOIN tasks t ON t.id = r.task_id
LEFT JOIN run_queue q ON q.run_id = r.id
LEFT JOIN run_delayed d ON d.run_id = r.id
LEFT JOIN run_leases l ON l.run_id = r.id
WHERE r.project_id = @project_id::uuid
  AND r.id = @id::uuid;

-- name: RunReadSnapshot :exec
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
