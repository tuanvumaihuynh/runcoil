-- name: ScheduleUpsert :exec
INSERT INTO schedules (
	id,
	worker_id,
	task_id,
	spec,
	skip_overlap,
	next_run_at
) VALUES (
	@id,
	@worker_id,
	@task_id,
	@spec,
	@skip_overlap,
	@next_run_at
)
ON CONFLICT (worker_id, task_id) DO UPDATE SET
	next_run_at = CASE
		WHEN schedules.spec = EXCLUDED.spec AND schedules.skip_overlap = EXCLUDED.skip_overlap
			THEN schedules.next_run_at
		ELSE EXCLUDED.next_run_at
	END,
	spec = EXCLUDED.spec,
	skip_overlap = EXCLUDED.skip_overlap,
	updated_at = now();

-- name: ScheduleDeleteExcept :exec
DELETE FROM schedules
WHERE worker_id = @worker_id
	AND NOT (task_id = ANY(@task_ids::text[]));

-- name: ScheduleLoadDue :many
SELECT worker_id, task_id, next_run_at FROM schedules
WHERE next_run_at IS NOT NULL
	AND next_run_at <= @until
ORDER BY next_run_at ASC
LIMIT @row_limit;

-- name: ScheduleGet :one
SELECT worker_id, task_id, spec, skip_overlap, next_run_at FROM schedules
WHERE worker_id = @worker_id AND task_id = @task_id;

-- name: ScheduleListByWorkerID :many
SELECT worker_id, task_id, spec, skip_overlap, next_run_at FROM schedules
WHERE worker_id = @worker_id
ORDER BY task_id ASC;

-- name: ScheduleAdvance :execrows
UPDATE schedules
SET next_run_at = @next_run_at,
	updated_at = now()
WHERE worker_id = @worker_id
	AND task_id = @task_id
	AND next_run_at = @tick;

-- name: ScheduleListMissed :many
SELECT worker_id, task_id, spec, skip_overlap, next_run_at FROM schedules
WHERE next_run_at IS NOT NULL
	AND next_run_at < @now
	AND (worker_id, task_id) > (@after_worker_id::text, @after_task_id::text)
ORDER BY worker_id ASC, task_id ASC
LIMIT @row_limit;
