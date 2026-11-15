-- +goose Up
-- +goose StatementBegin
CREATE TABLE schedules (
    id             uuid PRIMARY KEY,
    project_id     uuid NOT NULL REFERENCES projects (id),
    -- One schedule per task, declared in code. Several per task would relax this UNIQUE to
    -- (task_id, cron).
    task_id        uuid NOT NULL UNIQUE REFERENCES tasks (id) ON DELETE CASCADE,
    -- Read in UTC: five fields, or six with seconds first, joined by single spaces.
    cron           text NOT NULL,
    overlap_policy text NOT NULL DEFAULT 'allow' CHECK (overlap_policy IN ('allow', 'skip')),
    paused_at      timestamptz,
    -- After downtime: the next tick after now(), so a schedule fires once (catch-up latest).
    next_tick_at   timestamptz NOT NULL,
    last_run_id    uuid, -- read by overlap_policy = 'skip'
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX schedules_next_tick_at_idx ON schedules (next_tick_at) WHERE paused_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE schedules;
-- +goose StatementEnd
