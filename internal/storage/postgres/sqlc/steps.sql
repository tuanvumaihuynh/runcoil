-- name: StepListByRun :many
SELECT * FROM steps
WHERE run_id = @run_id
ORDER BY position ASC;

-- name: StepUpsert :exec
INSERT INTO steps (
	id,
	run_id,
	step_id,
	position,
	status,
	attempts_used,
	output,
	failure,
	retry_not_before,
	updated_at
) VALUES (
	@id,
	@run_id,
	@step_id,
	@position,
	@status,
	@attempts_used,
	@output,
	@failure,
	@retry_not_before,
	now()
)
ON CONFLICT (run_id, step_id) DO UPDATE SET
	status = EXCLUDED.status,
	attempts_used = EXCLUDED.attempts_used,
	output = EXCLUDED.output,
	failure = EXCLUDED.failure,
	retry_not_before = EXCLUDED.retry_not_before,
	updated_at = EXCLUDED.updated_at;
