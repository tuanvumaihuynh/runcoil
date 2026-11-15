-- name: StepGet :one
SELECT step_id, kind, status, output, error_code, error_message
FROM steps
WHERE run_id = @run_id::uuid AND step_id = @step_id::text;

-- name: StepCreate :one
-- Same kind: the first write wins, so a resend changes nothing. Another kind: the code
-- changed between deploys, so the new record replaces the old one.
INSERT INTO steps (run_id, step_id, kind, status, output, error_code, error_message, try_count)
VALUES (@run_id::uuid, @step_id::text, 'do', @status::text, sqlc.narg(output)::bytea,
        sqlc.narg(error_code)::text, sqlc.narg(error_message)::text, @try_count::integer)
ON CONFLICT (run_id, step_id) DO UPDATE
    SET kind = EXCLUDED.kind, status = EXCLUDED.status, output = EXCLUDED.output,
        error_code = EXCLUDED.error_code, error_message = EXCLUDED.error_message,
        try_count = EXCLUDED.try_count, created_at = now()
    WHERE steps.kind <> EXCLUDED.kind
RETURNING step_id;

-- name: StepListForRuns :many
SELECT run_id, step_id, kind, status, output, error_code, error_message
FROM steps
WHERE run_id = ANY (@run_ids::uuid[])
ORDER BY run_id, created_at;
