-- name: DelayedPromoteDue :many
-- Moves at most @batch_size Runs whose wait is over to run_queue, oldest first.
WITH due AS (
    DELETE FROM run_delayed
    WHERE run_id IN (
        SELECT run_id FROM run_delayed
        WHERE ready_at <= now()
        ORDER BY ready_at
        LIMIT @batch_size::integer
        FOR UPDATE SKIP LOCKED
    )
    RETURNING run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count,
              start_deadline_at
)
INSERT INTO run_queue (run_id, project_id, task_id, ready_at, retry_step_id, retry_count,
                       lost_count, start_deadline_at)
SELECT run_id, project_id, task_id, ready_at, retry_step_id, retry_count, lost_count,
       start_deadline_at
FROM due
RETURNING run_id;
