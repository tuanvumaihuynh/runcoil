-- name: RunCreate :one
INSERT INTO runs (
	id,
	worker_id,
	task_id,
	status,
	queued_reason,
	source,
	input,
	due_at,
	created_at,
	worker_wait_since,
	rerun_of
) VALUES (
	@id,
	@worker_id,
	@task_id,
	@status,
	@queued_reason,
	@source,
	@input,
	@due_at,
	@created_at,
	CASE WHEN @queued_reason::text = 'waiting_for_worker' THEN @created_at::timestamptz ELSE NULL END,
	@rerun_of
)
RETURNING *;

-- name: RunInsertTick :execrows
INSERT INTO runs (
	id,
	worker_id,
	task_id,
	status,
	queued_reason,
	source,
	due_at,
	created_at,
	worker_wait_since
) VALUES (
	@id,
	@worker_id,
	@task_id,
	'queued',
	@queued_reason,
	'schedule',
	@due_at,
	@created_at,
	CASE WHEN @queued_reason::text = 'waiting_for_worker' THEN @created_at::timestamptz ELSE NULL END
)
ON CONFLICT (worker_id, task_id, due_at) WHERE source = 'schedule' DO NOTHING;

-- name: RunHasUnfinishedScheduled :one
SELECT EXISTS (
	SELECT 1 FROM runs
	WHERE worker_id = @worker_id
		AND task_id = @task_id
		AND source = 'schedule'
		AND status IN ('queued', 'running')
);

-- name: RunGetStatus :one
-- The status and finish time only: a waiting call polls this, and reads the whole Run once at the end.
SELECT status, finished_at FROM runs
WHERE id = @id;

-- name: RunGet :one
SELECT * FROM runs
WHERE id = @id;

-- name: RunLock :one
SELECT * FROM runs
WHERE id = @id
FOR UPDATE;

-- name: RunUpdate :exec
UPDATE runs
SET status = @status,
	queued_reason = @queued_reason,
	attempts = @attempts,
	output = @output,
	failure = @failure,
	lease_token = @lease_token,
	lease_expires_at = @lease_expires_at,
	instance_id = @instance_id,
	acked_at = @acked_at,
	next_attempt_at = @next_attempt_at,
	started_at = @started_at,
	finished_at = @finished_at,
	handler_attempts_used = @handler_attempts_used,
	ended_token = @ended_token,
	ended_kind = @ended_kind,
	ended_at = @ended_at,
	running_step_id = @running_step_id,
	running_step_attempt = @running_step_attempt,
	running_step_policy = @running_step_policy,
	worker_wait_since = @worker_wait_since
WHERE id = @id;

-- A claim does not take a Run that never started and is past its Task's start deadline: that
-- Run is left for RunExpireNeverStarted, which fails it. The deadline test is the one that
-- statement uses. The join reads the Task by its key, and the read still starts from
-- runs_queued_ready_idx; the deadline test only filters the rows it reads. The row lock is
-- taken on runs alone, so a claim does not lock the Task.
-- name: RunClaimNext :one
WITH next AS (
	SELECT runs.id FROM runs
	JOIN tasks AS t ON t.worker_id = runs.worker_id AND t.task_id = runs.task_id
	WHERE runs.status = 'queued'
		AND runs.worker_id = @worker_id
		AND runs.task_id = @task_id
		AND COALESCE(runs.next_attempt_at, runs.due_at) <= @now
		AND NOT (
			runs.started_at IS NULL
			AND t.start_deadline_ns IS NOT NULL
			AND runs.due_at <= CAST(@now AS timestamptz) - make_interval(secs => t.start_deadline_ns / 1e9)
		)
	ORDER BY COALESCE(runs.next_attempt_at, runs.due_at) ASC, runs.created_at ASC
	LIMIT 1
	FOR UPDATE OF runs SKIP LOCKED
)
UPDATE runs AS r
SET status = 'running',
	queued_reason = '',
	attempts = r.attempts + 1,
	lease_token = @lease_token,
	lease_expires_at = @lease_expires_at,
	instance_id = @instance_id,
	acked_at = NULL,
	running_step_id = '',
	running_step_attempt = 0,
	running_step_policy = NULL
FROM next
WHERE r.id = next.id
RETURNING r.*;

-- The lease is renewed even when it is past its expiry, until the sweep takes the attempt back.
-- The restart grace relies on this: a worker attached before a restart renews the leases that
-- expired while the server was down.
-- name: RunRenewLeaseStep :execrows
UPDATE runs
SET lease_expires_at = @lease_expires_at,
	running_step_id = @running_step_id,
	running_step_attempt = @running_step_attempt,
	running_step_policy = @running_step_policy
WHERE id = @id
	AND lease_token = @lease_token
	AND instance_id = @instance_id
	AND status = 'running';

-- name: RunListExpiredLeases :many
SELECT id FROM runs
WHERE status = 'running' AND lease_expires_at <= @now
ORDER BY lease_expires_at ASC
LIMIT @row_limit;

-- The start-deadline rule lives here, not in the domain: a queued Run that never
-- started fails when its due time plus its Task's start deadline has passed. The
-- domain keeps only the failure text, so the two stay in step through this statement.
-- The read starts from the Tasks that have a deadline and locks their due Runs as it
-- reads them, so no Run is read when no Task has a deadline. The read uses
-- runs_queued_never_started_idx, so only the Runs past their deadline are read. The update
-- takes the locked Runs by their IDs.
-- name: RunExpireNeverStarted :many
WITH due AS (
	SELECT x.id FROM tasks AS t
	CROSS JOIN LATERAL (
		SELECT q.id FROM runs AS q
		WHERE q.status = 'queued'
			AND q.worker_id = t.worker_id
			AND q.task_id = t.task_id
			AND q.started_at IS NULL
			AND q.due_at <= CAST(@now AS timestamptz) - make_interval(secs => t.start_deadline_ns / 1e9)
		FOR UPDATE
	) AS x
	WHERE t.start_deadline_ns IS NOT NULL
	LIMIT @row_limit
)
UPDATE runs AS r
SET status = 'failed',
	queued_reason = '',
	next_attempt_at = NULL,
	worker_wait_since = NULL,
	finished_at = @now,
	failure = jsonb_build_object(
		'code', @failure_code::text,
		'message', @failure_message::text,
		'attempts', r.attempts
	)
WHERE r.id = ANY(ARRAY(SELECT due.id FROM due))
RETURNING r.id, r.worker_id, r.task_id;

-- The worker-wait rule lives here, not in the domain: a queued Run that waited for a
-- worker past the limit fails. The wait counts only while no worker is ready, and the
-- dispatcher does not run this statement during the restart grace. The domain keeps only
-- the failure text. The read locks one batch of waiting Runs, and the update takes them
-- by their IDs.
-- name: RunExpireWorkerWaits :many
WITH due AS (
	SELECT w.id FROM runs AS w
	WHERE w.status = 'queued'
		AND w.queued_reason = 'waiting_for_worker'
		AND w.worker_wait_since < @cutoff
	LIMIT @row_limit
	FOR UPDATE
)
UPDATE runs AS r
SET status = 'failed',
	queued_reason = '',
	next_attempt_at = NULL,
	worker_wait_since = NULL,
	finished_at = @now,
	failure = jsonb_build_object(
		'code', @failure_code::text,
		'message', @failure_message::text,
		'attempts', r.attempts
	)
WHERE r.id = ANY(ARRAY(SELECT due.id FROM due))
RETURNING r.id, r.worker_id, r.task_id;

-- The Tasks with a queued Run, found one Task at a time: each step probes
-- runs_queued_ready_idx for the first queued Run after the last Task found. The cost is
-- one index probe per Task, not one visit per queued Run, and the plan does not depend
-- on table statistics.
-- name: RunListQueuedTasks :many
WITH RECURSIVE queued AS (
	(
		SELECT worker_id, task_id FROM runs
		WHERE status = 'queued'
		ORDER BY worker_id, task_id
		LIMIT 1
	)
	UNION ALL
	SELECT n.worker_id, n.task_id FROM queued AS p
	CROSS JOIN LATERAL (
		SELECT worker_id, task_id FROM runs
		WHERE status = 'queued' AND (worker_id, task_id) > (p.worker_id, p.task_id)
		ORDER BY worker_id, task_id
		LIMIT 1
	) AS n
)
SELECT worker_id, task_id FROM queued
ORDER BY worker_id, task_id;

-- name: RunSetQueuedReason :execrows
UPDATE runs
SET queued_reason = @queued_reason,
	worker_wait_since = CASE
		WHEN @queued_reason::text = 'waiting_for_worker' AND queued_reason <> 'waiting_for_worker' THEN @now::timestamptz
		ELSE worker_wait_since
	END
WHERE status = 'queued'
	AND worker_id = @worker_id
	AND task_id = @task_id
	AND COALESCE(next_attempt_at, due_at) <= @now
	AND queued_reason <> @queued_reason;

-- name: RunCountRunning :one
SELECT count(*) FROM runs
WHERE status = 'running' AND instance_id = @instance_id;

-- name: RunListPage :many
-- Input and output are left out: a page of Runs must not carry payloads of up to 1 MiB each.
SELECT id, worker_id, task_id, status, queued_reason, source, attempts,
	failure, instance_id, due_at, created_at, started_at, finished_at, next_attempt_at, rerun_of
FROM runs
WHERE (@worker_id::text = '' OR worker_id = @worker_id::text)
	AND (@task_id::text = '' OR task_id = @task_id::text)
	AND (COALESCE(cardinality(@statuses::text[]), 0) = 0 OR status = ANY(@statuses::text[]))
	-- Set when every status is unfinished. It lets the planner use runs_unfinished_created_idx.
	AND (NOT @unfinished_only::boolean OR finished_at IS NULL)
	AND (sqlc.narg(created_after)::timestamptz IS NULL OR created_at >= sqlc.narg(created_after)::timestamptz)
	AND (sqlc.narg(created_before)::timestamptz IS NULL OR created_at < sqlc.narg(created_before)::timestamptz)
	AND (NOT @has_cursor::boolean OR (created_at, id) < (@cursor_created_at::timestamptz, @cursor_id::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: RunListFinishedBefore :many
SELECT id FROM runs
WHERE status IN ('succeeded', 'failed') AND finished_at < @cutoff
ORDER BY finished_at ASC
LIMIT @row_limit;

-- name: StepDeleteByRunIDs :execrows
DELETE FROM steps
WHERE run_id = ANY(@run_ids::text[]);

-- name: RunDeleteByIDs :execrows
DELETE FROM runs
WHERE id = ANY(@run_ids::text[]);
