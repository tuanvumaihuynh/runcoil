-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    username        text NOT NULL UNIQUE,
    password_hash   text NOT NULL,
    is_server_admin boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
