-- +goose Up
-- +goose StatementBegin
ALTER TABLE api_keys
    ADD COLUMN key_hint     text NOT NULL,
    ADD COLUMN created_by   uuid NOT NULL REFERENCES users (id),
    ADD COLUMN last_used_at timestamptz,
    ADD COLUMN revoked_at   timestamptz;

ALTER TABLE workers DROP CONSTRAINT workers_end_reason_check;
ALTER TABLE workers ADD CONSTRAINT workers_end_reason_check
    CHECK (end_reason IN ('deregistered', 'heartbeat_timeout', 'revoked'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
LOCK TABLE api_keys;

UPDATE workers SET end_reason = 'deregistered' WHERE end_reason = 'revoked';

ALTER TABLE workers DROP CONSTRAINT workers_end_reason_check;
ALTER TABLE workers ADD CONSTRAINT workers_end_reason_check
    CHECK (end_reason IN ('deregistered', 'heartbeat_timeout'));

UPDATE api_keys SET key_hash = '\x00'::bytea || key_hash WHERE revoked_at IS NOT NULL;

ALTER TABLE api_keys
    DROP COLUMN revoked_at,
    DROP COLUMN last_used_at,
    DROP COLUMN created_by,
    DROP COLUMN key_hint;
-- +goose StatementEnd
