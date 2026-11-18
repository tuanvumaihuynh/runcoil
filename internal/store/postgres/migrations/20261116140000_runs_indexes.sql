-- +goose Up
-- +goose StatementBegin
CREATE INDEX runs_project_id_id_idx ON runs (project_id, id DESC);
CREATE INDEX runs_project_id_status_id_idx ON runs (project_id, status, id DESC);
CREATE INDEX runs_project_id_task_id_id_idx ON runs (project_id, task_id, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX runs_project_id_task_id_id_idx;
DROP INDEX runs_project_id_status_id_idx;
DROP INDEX runs_project_id_id_idx;
-- +goose StatementEnd
