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
WITH page AS (
    SELECT t.id, t.project_id, t.app, t.name, t.last_registered_at
    FROM tasks t
    WHERE t.project_id = @project_id::uuid
      AND (sqlc.narg(after_id)::uuid IS NULL OR t.id > sqlc.narg(after_id)::uuid)
    ORDER BY t.id
    LIMIT @page_limit::integer
),
live AS (
    SELECT declared.task_id, count(*) AS worker_count
    FROM workers w
    CROSS JOIN LATERAL unnest(w.task_ids) AS declared (task_id)
    WHERE w.project_id = @project_id::uuid AND w.ended_at IS NULL
    GROUP BY declared.task_id
)
SELECT t.id, t.app, t.name, t.last_registered_at,
       s.cron AS schedule_cron, s.overlap_policy AS schedule_overlap_policy,
       s.next_tick_at AS schedule_next_tick_at, s.paused_at AS schedule_paused_at,
       lr.id AS last_run_id, lr.status AS last_run_status, lr.created_at AS last_run_created_at,
       coalesce(live.worker_count, 0)::integer AS live_worker_count
FROM page t
LEFT JOIN schedules s ON s.task_id = t.id
LEFT JOIN runs lr ON lr.id = (
    SELECT r.id
    FROM runs r
    WHERE r.project_id = t.project_id AND r.task_id = t.id
    ORDER BY r.id DESC
    LIMIT 1
)
LEFT JOIN live ON live.task_id = t.id
ORDER BY t.id;

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
