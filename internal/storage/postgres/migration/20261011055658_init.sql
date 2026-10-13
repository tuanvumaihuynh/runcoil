-- +goose Up
-- +goose StatementBegin
-- The schema of Runcoil. IDs are text: the application generates every ID, so the
-- tables have no database default for them.

-- Tasks that workers registered. The timing of a Task is in schedules.
CREATE TABLE tasks (
    id                TEXT PRIMARY KEY,
    worker_id         TEXT NOT NULL CHECK (worker_id <> ''),
    task_id           TEXT NOT NULL CHECK (task_id <> ''),
    retry_policy      JSONB,
    start_deadline_ns BIGINT CHECK (start_deadline_ns > 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tasks_worker_id_task_id_key UNIQUE (worker_id, task_id)
);

-- One row per Run. A rerun keeps the reference to its original Run; retention removes old
-- Runs, so the reference is cleared then instead of blocking the removal.
CREATE TABLE runs (
    id                    TEXT PRIMARY KEY,
    worker_id             TEXT NOT NULL,
    task_id               TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    queued_reason         TEXT NOT NULL DEFAULT ''
                          CHECK (queued_reason IN ('', 'waiting_for_worker', 'waiting_for_capacity', 'retry_backoff')),
    source                TEXT NOT NULL CHECK (source IN ('invoke', 'schedule', 'rerun')),
    attempts              INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    input                 BYTEA,
    output                BYTEA,
    failure               JSONB,
    lease_token           TEXT NOT NULL DEFAULT '',
    lease_expires_at      TIMESTAMPTZ,
    instance_id           TEXT NOT NULL DEFAULT '',
    acked_at              TIMESTAMPTZ,
    due_at                TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    rerun_of              TEXT REFERENCES runs (id) ON DELETE SET NULL,
    -- Failed tries of handler code outside any Step, and the token and kind of the attempt
    -- that last ended with a retry or a release. They make a repeated answer possible after
    -- the Run has been dispatched again.
    handler_attempts_used INTEGER NOT NULL DEFAULT 0 CHECK (handler_attempts_used >= 0),
    ended_token           TEXT NOT NULL DEFAULT '',
    ended_kind            TEXT NOT NULL DEFAULT '' CHECK (ended_kind IN ('', 'released', 'failed')),
    -- The earliest start of the next attempt of a queued Run. due_at is when the Run was due,
    -- and never changes after the Run is created.
    next_attempt_at       TIMESTAMPTZ,
    -- The Step that the current attempt is running, as its last heartbeat named it, and its
    -- try number.
    running_step_id       TEXT NOT NULL DEFAULT '',
    running_step_attempt  INTEGER NOT NULL DEFAULT 0 CHECK (running_step_attempt >= 0),
    -- When the current spell of waiting for a worker began. NULL when not waiting.
    worker_wait_since     TIMESTAMPTZ,
    -- When the retry or release answered by ended_token was due.
    ended_at              TIMESTAMPTZ,
    -- The retry policy of the Step that the current attempt runs, as its last heartbeat
    -- named it. A lost attempt counts its try under this policy first.
    running_step_policy   JSONB,
    CONSTRAINT runs_task_fkey FOREIGN KEY (worker_id, task_id) REFERENCES tasks (worker_id, task_id),
    -- A Run has a finished time exactly when its status is finished. The index of the
    -- unfinished Runs relies on it, so every write keeps it.
    CONSTRAINT runs_finished_status_check CHECK ((finished_at IS NULL) = (status IN ('queued', 'running')))
);

-- The dispatcher takes a queued Run when it is ready: at its next attempt, or at its due time.
CREATE INDEX runs_queued_ready_idx ON runs (worker_id, task_id, (COALESCE(next_attempt_at, due_at)))
    WHERE status = 'queued';

-- The dispatcher counts the attempts an instance holds.
CREATE INDEX runs_running_instance_idx ON runs (instance_id) WHERE status = 'running';

-- The sweeper finds the running attempts whose leases have expired.
CREATE INDEX runs_running_lease_idx ON runs (lease_expires_at) WHERE status = 'running';

-- A tick creates at most one Run: a schedule's Runs are unique by Task and tick.
CREATE UNIQUE INDEX runs_schedule_tick_key ON runs (worker_id, task_id, due_at)
    WHERE source = 'schedule';

-- The overlap check looks only at the unfinished schedule Runs of one Task.
CREATE INDEX runs_unfinished_schedule_idx ON runs (worker_id, task_id)
    WHERE source = 'schedule' AND status IN ('queued', 'running');

-- ListRuns pages newest first.
CREATE INDEX runs_created_idx ON runs (created_at DESC, id DESC);

-- ListRuns of one Task pages newest first.
CREATE INDEX runs_task_created_idx ON runs (worker_id, task_id, created_at DESC, id DESC);

-- ListRuns of the unfinished Runs pages newest first. The predicate is on finished_at and
-- not on status, so that the dispatcher's statements, which filter by status, cannot be
-- planned onto this index.
CREATE INDEX runs_unfinished_created_idx ON runs (created_at DESC, id DESC)
    WHERE finished_at IS NULL;

-- The start deadline expiry reads the queued Runs that never started, by Task and due time.
CREATE INDEX runs_queued_never_started_idx ON runs (worker_id, task_id, due_at)
    WHERE status = 'queued' AND started_at IS NULL;

-- Retention removes the finished Runs that are oldest.
CREATE INDEX runs_finished_idx ON runs (finished_at)
    WHERE status IN ('succeeded', 'failed');

-- The worker wait expiry looks only at queued Runs that wait for a worker.
CREATE INDEX runs_worker_wait_idx ON runs (worker_wait_since)
    WHERE status = 'queued' AND queued_reason = 'waiting_for_worker';

-- Deleting a Run clears the rerun_of of the Runs that point at it, and this index finds them.
CREATE INDEX runs_rerun_of_idx ON runs (rerun_of) WHERE rerun_of IS NOT NULL;

-- One row per Step of a Run. A pending Step has failed tries and no final record.
CREATE TABLE steps (
    id               TEXT PRIMARY KEY,
    run_id           TEXT NOT NULL REFERENCES runs (id),
    step_id          TEXT NOT NULL CHECK (step_id <> ''),
    position         INTEGER NOT NULL CHECK (position >= 1),
    status           TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempts_used    INTEGER NOT NULL CHECK (attempts_used >= 1),
    output           BYTEA,
    failure          JSONB,
    retry_not_before TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT steps_run_step_id_key UNIQUE (run_id, step_id),
    CONSTRAINT steps_run_position_key UNIQUE (run_id, position),
    CONSTRAINT steps_retry_time_check CHECK (status <> 'pending' OR retry_not_before IS NOT NULL)
);

-- The schedule of a Task with its next tick. Only the schedules due soon are read into the
-- server's memory. One schedule per Task for now.
CREATE TABLE schedules (
    id           TEXT PRIMARY KEY,
    worker_id    TEXT NOT NULL,
    task_id      TEXT NOT NULL,
    -- The schedule without its overlap rule: kind, cron, every_ns, daily.
    spec         JSONB NOT NULL,
    skip_overlap BOOLEAN NOT NULL DEFAULT false,
    -- The next tick. NULL when the schedule has no further tick.
    next_run_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT schedules_worker_id_task_id_key UNIQUE (worker_id, task_id),
    CONSTRAINT schedules_task_fkey FOREIGN KEY (worker_id, task_id) REFERENCES tasks (worker_id, task_id)
);

-- The poller reads the schedules whose next tick is near.
CREATE INDEX schedules_next_run_at_idx ON schedules (next_run_at)
    WHERE next_run_at IS NOT NULL;

-- Caller keys, unique across the server. A key whose expires_at has passed counts as absent.
CREATE TABLE idempotency_keys (
    kind             TEXT NOT NULL CHECK (kind IN ('caller')),
    idempotency_key  TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    fingerprint      TEXT NOT NULL CHECK (fingerprint <> ''),
    run_id           TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (kind, idempotency_key)
);
CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE idempotency_keys;
DROP TABLE steps;
DROP TABLE schedules;
DROP INDEX runs_rerun_of_idx;
DROP INDEX runs_queued_never_started_idx;
DROP INDEX runs_unfinished_created_idx;
DROP INDEX runs_task_created_idx;
DROP TABLE runs;
DROP TABLE tasks;
-- +goose StatementEnd
