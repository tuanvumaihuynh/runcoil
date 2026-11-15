-- +goose Up
-- +goose StatementBegin
CREATE TABLE projects (
    id         uuid PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    id         uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id),
    name       text NOT NULL,
    key_hash   bytea NOT NULL UNIQUE, -- sha256 of the key
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE dispatchers (
    id           uuid PRIMARY KEY,
    started_at   timestamptz NOT NULL DEFAULT now(),
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    stopped_at   timestamptz
);

CREATE TABLE workers (
    id            uuid PRIMARY KEY,
    project_id    uuid NOT NULL REFERENCES projects (id),
    api_key_id    uuid NOT NULL REFERENCES api_keys (id),
    token_hash    bytea NOT NULL UNIQUE, -- sha256 of the session token
    app           text NOT NULL,
    task_ids      uuid[] NOT NULL,
    slots         integer NOT NULL CHECK (slots > 0),
    dispatcher_id uuid,
    stream_id     uuid, -- NULL until the first Listen
    connected_at  timestamptz NOT NULL DEFAULT now(),
    heartbeat_at  timestamptz NOT NULL DEFAULT now(),
    ended_at      timestamptz,
    end_reason    text CHECK (end_reason IN ('deregistered', 'heartbeat_timeout'))
);
CREATE INDEX workers_live_idx ON workers (id) WHERE ended_at IS NULL;

CREATE TABLE tasks (
    id                 uuid PRIMARY KEY,
    project_id         uuid NOT NULL REFERENCES projects (id),
    app                text NOT NULL,
    name               text NOT NULL,
    retry_policy       jsonb,
    start_deadline     interval,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    last_registered_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tasks_project_id_app_name_key UNIQUE (project_id, app, name)
);

CREATE TABLE runs (
    id            uuid PRIMARY KEY,
    created_at    timestamptz NOT NULL,
    project_id    uuid NOT NULL,
    task_id       uuid NOT NULL,
    status        text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    input         bytea,
    config        jsonb NOT NULL, -- the Task's settings when the Run was created
    output        bytea,
    error_code    text,
    error_message text,
    started_at    timestamptz,
    ended_at      timestamptz
);

CREATE TABLE steps (
    run_id        uuid NOT NULL,
    step_id       text NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('do')),
    status        text NOT NULL CHECK (status IN ('succeeded', 'failed')),
    output        bytea,
    error_code    text,
    error_message text,
    try_count     integer NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT steps_pkey PRIMARY KEY (run_id, step_id)
);

CREATE SEQUENCE lease_epoch_seq;

CREATE TABLE run_queue (
    run_id            uuid PRIMARY KEY,
    project_id        uuid NOT NULL,
    task_id           uuid NOT NULL,
    ready_at          timestamptz NOT NULL,
    retry_step_id     text,
    retry_count       smallint NOT NULL DEFAULT 0,
    lost_count        smallint NOT NULL DEFAULT 0,
    start_deadline_at timestamptz -- first start only
);
CREATE INDEX run_queue_task_id_ready_at_idx ON run_queue (task_id, ready_at);
CREATE INDEX run_queue_start_deadline_at_idx ON run_queue (start_deadline_at) WHERE start_deadline_at IS NOT NULL;

CREATE TABLE run_delayed (
    run_id            uuid PRIMARY KEY,
    project_id        uuid NOT NULL,
    task_id           uuid NOT NULL,
    ready_at          timestamptz NOT NULL,
    retry_step_id     text,
    retry_count       smallint NOT NULL DEFAULT 0,
    lost_count        smallint NOT NULL DEFAULT 0,
    start_deadline_at timestamptz
);
CREATE INDEX run_delayed_ready_at_idx ON run_delayed (ready_at);

CREATE TABLE run_leases (
    run_id        uuid PRIMARY KEY,
    project_id    uuid NOT NULL,
    task_id       uuid NOT NULL,
    worker_id     uuid NOT NULL,
    lease_epoch   bigint NOT NULL DEFAULT nextval('lease_epoch_seq'),
    started_at    timestamptz NOT NULL DEFAULT now(),
    retry_step_id text,
    retry_count   smallint NOT NULL DEFAULT 0,
    lost_count    smallint NOT NULL DEFAULT 0
);
CREATE INDEX run_leases_worker_id_idx ON run_leases (worker_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE run_leases;
DROP TABLE run_delayed;
DROP TABLE run_queue;
DROP SEQUENCE lease_epoch_seq;
DROP TABLE steps;
DROP TABLE runs;
DROP TABLE tasks;
DROP TABLE workers;
DROP TABLE dispatchers;
DROP TABLE api_keys;
DROP TABLE projects;
-- +goose StatementEnd
