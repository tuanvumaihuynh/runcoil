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

-- name: RunClaimNext :one
WITH next AS (
	SELECT runs.id FROM runs
	WHERE runs.status = 'queued'
		AND runs.worker_id = @worker_id
		AND runs.task_id = @task_id
		AND COALESCE(runs.next_attempt_at, runs.due_at) <= @now
	ORDER BY COALESCE(runs.next_attempt_at, runs.due_at) ASC, runs.created_at ASC
	LIMIT 1
	FOR UPDATE SKIP LOCKED
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
-- name: RunExpireNeverStarted :many
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
WHERE r.status = 'queued'
	AND r.started_at IS NULL
	AND r.id IN (
		SELECT q.id FROM runs AS q
		JOIN tasks AS t ON t.worker_id = q.worker_id AND t.task_id = q.task_id
		WHERE q.status = 'queued'
			AND q.started_at IS NULL
			AND t.start_deadline_ns IS NOT NULL
			AND q.due_at + make_interval(secs => t.start_deadline_ns / 1e9) <= @now
		LIMIT @row_limit
	)
RETURNING r.id, r.worker_id, r.task_id;

-- The worker-wait rule lives here, not in the domain: a queued Run that waited for a
-- worker past the limit fails. The wait counts only while no worker is ready, and the
-- dispatcher does not run this statement during the restart grace. The domain keeps only
-- the failure text.
-- name: RunExpireWorkerWaits :many
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
WHERE r.status = 'queued'
	AND r.queued_reason = 'waiting_for_worker'
	AND r.worker_wait_since < @cutoff
	AND r.id IN (
		SELECT w.id FROM runs AS w
		WHERE w.status = 'queued'
			AND w.queued_reason = 'waiting_for_worker'
			AND w.worker_wait_since < @cutoff
		LIMIT @row_limit
	)
RETURNING r.id, r.worker_id, r.task_id;

-- name: RunListQueuedTasks :many
SELECT DISTINCT worker_id, task_id FROM runs
WHERE status = 'queued'
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
	AND (@created_after::timestamptz IS NULL OR created_at >= @created_after::timestamptz)
	AND (@created_before::timestamptz IS NULL OR created_at < @created_before::timestamptz)
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
