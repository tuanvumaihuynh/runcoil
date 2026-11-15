-- +goose Up
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN email       text UNIQUE,
    ADD COLUMN disabled_at timestamptz;

CREATE TABLE project_members (
    project_id uuid NOT NULL REFERENCES projects (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    role       text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    CONSTRAINT project_members_pkey PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_id_idx ON project_members (user_id);

CREATE TABLE sessions (
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
DROP TABLE project_members;

ALTER TABLE users
    DROP COLUMN disabled_at,
    DROP COLUMN email;
-- +goose StatementEnd
