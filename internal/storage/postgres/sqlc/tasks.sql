-- name: TaskUpsert :exec
INSERT INTO tasks (
	id,
	worker_id,
	task_id,
	retry_policy,
	start_deadline_ns,
	registered_at
) VALUES (
	@id,
	@worker_id,
	@task_id,
	@retry_policy,
	@start_deadline_ns,
	now()
)
ON CONFLICT (worker_id, task_id) DO UPDATE SET
	retry_policy = EXCLUDED.retry_policy,
	start_deadline_ns = EXCLUDED.start_deadline_ns,
	registered_at = EXCLUDED.registered_at;

-- name: TaskGet :one
SELECT * FROM tasks
WHERE worker_id = @worker_id AND task_id = @task_id;

-- name: TaskListByWorkerID :many
SELECT * FROM tasks
WHERE worker_id = @worker_id
ORDER BY task_id ASC;
