-- name: LeaseClaim :many
-- @slots rows at most per Task through (task_id, ready_at), then @slots in all. Rows
-- locked but not picked are released at COMMIT. A Run past its start deadline is never
-- claimed.
WITH picked AS (
    SELECT q.run_id
    FROM unnest(@task_ids::uuid[]) AS t(task_id)
    CROSS JOIN LATERAL (
        SELECT run_id, ready_at
        FROM run_queue
        WHERE task_id = t.task_id
          AND (start_deadline_at IS NULL OR start_deadline_at > now())
        ORDER BY ready_at
        LIMIT @slots::integer
        FOR UPDATE SKIP LOCKED
    ) q
    ORDER BY q.ready_at
    LIMIT @slots::integer
), moved AS (
    DELETE FROM run_queue
    WHERE run_id IN (SELECT run_id FROM picked)
    RETURNING run_id, project_id, task_id, retry_step_id, retry_count, lost_count
)
INSERT INTO run_leases (run_id, project_id, task_id, worker_id, retry_step_id, retry_count, lost_count)
SELECT run_id, project_id, task_id, @worker_id::uuid, retry_step_id, retry_count, lost_count
FROM moved
RETURNING run_id, lease_epoch;

-- name: LeaseReleaseUnknown :many
-- Leases the worker did not report go back to the queue, without counting as lost.
WITH orphaned AS (
    DELETE FROM run_leases
    WHERE worker_id = @worker_id::uuid AND lease_epoch <> ALL (@held_epochs::bigint[])
    RETURNING run_id, project_id, task_id, retry_step_id, retry_count, lost_count
)
INSERT INTO run_queue (run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count)
SELECT run_id, project_id, task_id, now(), retry_step_id, retry_count, lost_count
FROM orphaned
RETURNING run_id;

-- name: LeaseListNotHeld :many
-- The Runs the worker reports but no longer holds.
SELECT r.run_id::uuid AS run_id, r.lease_epoch::bigint AS lease_epoch
FROM (SELECT unnest(@run_ids::uuid[]) AS run_id, unnest(@lease_epochs::bigint[]) AS lease_epoch) AS r
WHERE NOT EXISTS (
    SELECT 1 FROM run_leases l
    WHERE l.run_id = r.run_id AND l.lease_epoch = r.lease_epoch AND l.worker_id = @worker_id::uuid
);

-- name: LeaseLock :one
-- First statement of every worker call on a Run. config is the Task's settings when the
-- Run was created.
SELECT l.retry_step_id, l.retry_count, r.config
FROM run_leases l
JOIN runs r ON r.id = l.run_id
WHERE l.run_id = @run_id::uuid AND l.lease_epoch = @lease_epoch::bigint AND l.worker_id = @worker_id::uuid
FOR UPDATE OF l;

-- name: LeaseScheduleStepRetry :one
-- Ends the attempt and puts the Run in run_delayed until @delay has passed. retry_count
-- counts the failed tries of @step_id, and starts again at 1 when another Step fails.
WITH released AS (
    DELETE FROM run_leases
    WHERE run_id = @run_id::uuid AND lease_epoch = @lease_epoch::bigint AND worker_id = @worker_id::uuid
    RETURNING run_id, project_id, task_id, retry_step_id, retry_count, lost_count
)
INSERT INTO run_delayed (run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count)
SELECT run_id, project_id, task_id, now() + @delay::interval, @step_id::text,
       CASE WHEN retry_step_id = @step_id::text THEN retry_count + 1 ELSE 1 END,
       lost_count
FROM released
RETURNING retry_count, ready_at;

-- name: LeaseResetStepRetry :exec
UPDATE run_leases
SET retry_step_id = NULL, retry_count = 0
WHERE run_id = @run_id::uuid AND retry_step_id = @step_id::text;

-- name: LeaseReleaseOfWorker :many
-- The leases of a worker that deregistered go back to the queue. Not the worker's fault:
-- lost_count stays as it is.
WITH released AS (
    DELETE FROM run_leases
    WHERE run_id IN (
        SELECT run_id FROM run_leases
        WHERE worker_id = @worker_id::uuid
        ORDER BY run_id
        FOR UPDATE
    )
    RETURNING run_id, project_id, task_id, retry_step_id, retry_count, lost_count
)
INSERT INTO run_queue (run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count)
SELECT run_id, project_id, task_id, now(), retry_step_id, retry_count, lost_count
FROM released
RETURNING run_id;

-- name: LeaseRequeueLost :many
-- The leases of dead workers: lost_count + 1, then back to the queue, or, above
-- @max_lost, the Run fails with attempt_lost. lost is true for a Run that failed.
WITH lost AS (
    DELETE FROM run_leases
    WHERE run_id IN (
        SELECT run_id FROM run_leases
        WHERE worker_id = ANY (@worker_ids::uuid[])
        ORDER BY run_id
        FOR UPDATE
    )
    RETURNING run_id, project_id, task_id, retry_step_id, retry_count, lost_count + 1 AS lost_count
), failed AS (
    UPDATE runs
    SET status = 'failed', error_code = 'attempt_lost',
        error_message = 'the run was lost by too many workers that stopped while running it',
        ended_at = now()
    FROM lost
    WHERE runs.id = lost.run_id AND lost.lost_count > @max_lost::integer
    RETURNING runs.id
), requeued AS (
    INSERT INTO run_queue (run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count)
    SELECT run_id, project_id, task_id, now(), retry_step_id, retry_count, lost_count
    FROM lost
    WHERE lost_count <= @max_lost::integer
    RETURNING run_id
)
SELECT id AS run_id, true AS failed FROM failed
UNION ALL
SELECT run_id, false AS failed FROM requeued;
