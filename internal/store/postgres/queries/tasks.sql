-- name: TaskUpsert :one
INSERT INTO tasks (id, project_id, app, name, retry_policy, start_deadline)
VALUES (@id::uuid, @project_id::uuid, @app::text, @name::text, sqlc.narg(retry_policy)::jsonb,
        sqlc.narg(start_deadline)::interval)
ON CONFLICT (project_id, app, name) DO UPDATE
SET retry_policy       = EXCLUDED.retry_policy,
    start_deadline     = EXCLUDED.start_deadline,
    updated_at         = CASE
                           WHEN (tasks.retry_policy, tasks.start_deadline)
                                IS DISTINCT FROM
                                (EXCLUDED.retry_policy, EXCLUDED.start_deadline)
                           THEN now()
                           ELSE tasks.updated_at
                         END,
    last_registered_at = now()
RETURNING id;

-- name: TaskListNamesByIDs :many
SELECT id, name
FROM tasks
WHERE project_id = @project_id::uuid
  AND id = ANY (@ids::uuid[]);

-- name: TaskList :many
SELECT t.id, t.app, t.name, t.last_registered_at,
       s.cron AS schedule_cron, s.overlap_policy AS schedule_overlap_policy,
       s.next_tick_at AS schedule_next_tick_at, s.paused_at AS schedule_paused_at,
       lr.id AS last_run_id, lr.status AS last_run_status, lr.created_at AS last_run_created_at,
       (SELECT count(*)
        FROM workers w
        WHERE w.project_id = t.project_id AND w.ended_at IS NULL AND t.id = ANY (w.task_ids)
       )::integer AS live_worker_count
FROM tasks t
LEFT JOIN schedules s ON s.task_id = t.id
LEFT JOIN runs lr ON lr.id = (
    SELECT r.id
    FROM runs r
    WHERE r.project_id = t.project_id AND r.task_id = t.id
    ORDER BY r.id DESC
    LIMIT 1
)
WHERE t.project_id = @project_id::uuid
  AND (sqlc.narg(after_id)::uuid IS NULL OR t.id > sqlc.narg(after_id)::uuid)
ORDER BY t.id
LIMIT @page_limit::integer;

-- name: TaskGet :one
SELECT t.id, t.app, t.name, t.retry_policy, t.start_deadline, t.created_at, t.last_registered_at,
       s.cron AS schedule_cron, s.overlap_policy AS schedule_overlap_policy,
       s.next_tick_at AS schedule_next_tick_at, s.paused_at AS schedule_paused_at,
       lr.id AS last_run_id, lr.status AS last_run_status, lr.created_at AS last_run_created_at
FROM tasks t
LEFT JOIN schedules s ON s.task_id = t.id
LEFT JOIN runs lr ON lr.id = (
    SELECT r.id
    FROM runs r
    WHERE r.project_id = t.project_id AND r.task_id = t.id
    ORDER BY r.id DESC
    LIMIT 1
)
WHERE t.project_id = @project_id::uuid
  AND t.id = @id::uuid;
