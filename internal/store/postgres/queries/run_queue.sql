-- name: QueueExpireStartDeadlines :many
-- Fails at most @batch_size queued Runs that did not start before their start deadline.
WITH expired AS (
    DELETE FROM run_queue
    WHERE run_id IN (
        SELECT run_id FROM run_queue
        WHERE start_deadline_at <= now()
        ORDER BY start_deadline_at
        LIMIT @batch_size::integer
        FOR UPDATE SKIP LOCKED
    )
    RETURNING run_id
)
UPDATE runs
SET status = 'failed', error_code = 'start_deadline',
    error_message = 'the run did not start before its start deadline', ended_at = now()
FROM expired
WHERE runs.id = expired.run_id
RETURNING runs.id;

-- name: QueueExpireRuns :many
-- Fails the Runs among @run_ids that are queued past their start deadline. No SKIP
-- LOCKED: a claim that holds a queue row commits first, and its Run is then not failed.
WITH expired AS (
    DELETE FROM run_queue
    WHERE run_id IN (
        SELECT run_id FROM run_queue
        WHERE run_id = ANY (@run_ids::uuid[]) AND start_deadline_at <= now()
        ORDER BY run_id
        FOR UPDATE
    )
    RETURNING run_id
)
UPDATE runs
SET status = 'failed', error_code = 'start_deadline',
    error_message = 'the run did not start before its start deadline', ended_at = now()
FROM expired
WHERE runs.id = expired.run_id
RETURNING runs.id;
