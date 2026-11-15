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
