-- +goose Up
-- +goose StatementBegin
ALTER TABLE schedules ALTER COLUMN overlap_policy SET DEFAULT 'skip';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE schedules ALTER COLUMN overlap_policy SET DEFAULT 'allow';
-- +goose StatementEnd
