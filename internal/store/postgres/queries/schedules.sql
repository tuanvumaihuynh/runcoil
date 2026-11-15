-- name: ScheduleUpsert :exec
-- @next_tick_at is the first tick after ScheduleNow, computed in Go; it is used only when
-- the cron changed. A changed cron keeps the schedule's pause and last Run.
INSERT INTO schedules (id, project_id, task_id, cron, overlap_policy, next_tick_at)
VALUES (@id::uuid, @project_id::uuid, @task_id::uuid, @cron::text, @overlap_policy::text,
        @next_tick_at::timestamptz)
ON CONFLICT (task_id) DO UPDATE
SET cron           = EXCLUDED.cron,
    overlap_policy = EXCLUDED.overlap_policy,
    next_tick_at   = CASE WHEN schedules.cron IS DISTINCT FROM EXCLUDED.cron
                          THEN EXCLUDED.next_tick_at
                          ELSE schedules.next_tick_at
                     END,
    updated_at     = CASE WHEN (schedules.cron, schedules.overlap_policy)
                               IS DISTINCT FROM (EXCLUDED.cron, EXCLUDED.overlap_policy)
                          THEN now()
                          ELSE schedules.updated_at
                     END;

-- name: ScheduleDelete :exec
DELETE FROM schedules WHERE task_id = @task_id::uuid;

-- name: ScheduleNow :one
-- The database clock, which a schedule's first tick starts from.
SELECT now()::timestamptz AS now;

-- name: ScheduleTakeDue :many
-- At most @batch_size schedules whose tick has come, locked until COMMIT, so no two
-- servers fire the same tick. It reads no other table: when another server committed a
-- schedule after this statement's snapshot, PostgreSQL rechecks the newest version of the
-- locked row but keeps joined rows from the old snapshot. So last_run_id is current, and
-- RunListActive reads that Run in a later statement. now is the clock the ticks are
-- compared with: the next tick is computed from it, so it is always after the tick taken.
SELECT id, task_id, cron, overlap_policy, last_run_id, now()::timestamptz AS now
FROM schedules
WHERE paused_at IS NULL AND next_tick_at <= now()
ORDER BY next_tick_at
LIMIT @batch_size::integer
FOR UPDATE SKIP LOCKED;

-- name: ScheduleAdvance :exec
-- Moves each schedule to its next tick. The nil uuid as run id keeps the last Run: the
-- tick was skipped.
UPDATE schedules s
SET next_tick_at = v.next_tick_at,
    last_run_id  = COALESCE(NULLIF(v.run_id, '00000000-0000-0000-0000-000000000000'::uuid), s.last_run_id)
FROM (SELECT unnest(@ids::uuid[]) AS id,
             unnest(@next_tick_ats::timestamptz[]) AS next_tick_at,
             unnest(@run_ids::uuid[]) AS run_id) AS v
WHERE s.id = v.id;
