package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres/sqlc"
)

// TaskStore keeps the Tasks that workers registered. The timing of each Task is kept by
// the ScheduleStore, not here.
type TaskStore struct {
	db DB
	q  *sqlc.Queries
}

// NewTaskStore creates a TaskStore on db.
func NewTaskStore(db DB) *TaskStore {
	return &TaskStore{db: db, q: sqlc.New()}
}

// UpsertTasks stores the Tasks of one registration in one transaction. A Task that
// was stored before is updated, and it stays known when the registration does not
// name it.
func (s *TaskStore) UpsertTasks(ctx context.Context, tasks []domain.Task) error {
	return s.db.WithTx(ctx, func(tx DB) error {
		for _, task := range tasks {
			params, err := taskUpsertParams(task)
			if err != nil {
				return fmt.Errorf("encode task %s: %w", task.TaskID, err)
			}

			if err := s.q.TaskUpsert(ctx, tx, params); err != nil {
				return fmt.Errorf("upsert task %s: %w", task.TaskID, err)
			}
		}

		return nil
	})
}

// GetTask returns the stored Task, or domain.ErrNotFound.
func (s *TaskStore) GetTask(ctx context.Context, workerID, taskID string) (domain.Task, error) {
	row, err := s.q.TaskGet(ctx, s.db, sqlc.TaskGetParams{WorkerID: workerID, TaskID: taskID})
	if isNoRowsError(err) {
		return domain.Task{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Task{}, fmt.Errorf("get task: %w", err)
	}

	return taskFromRow(row)
}

// ListTasks returns the Tasks of a worker ID, ordered by task ID. The timing is not included.
func (s *TaskStore) ListTasks(ctx context.Context, workerID string) ([]domain.Task, error) {
	rows, err := s.q.TaskListByWorkerID(ctx, s.db, workerID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		task, err := taskFromRow(row)
		if err != nil {
			return nil, err
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func taskUpsertParams(task domain.Task) (sqlc.TaskUpsertParams, error) {
	rowID, err := uuid.NewV7()
	if err != nil {
		return sqlc.TaskUpsertParams{}, fmt.Errorf("new task id: %w", err)
	}

	params := sqlc.TaskUpsertParams{ID: rowID.String(), WorkerID: task.WorkerID, TaskID: task.TaskID}

	if task.Retry != nil {
		raw, err := retryRawParams(task.Retry)
		if err != nil {
			return params, err
		}

		params.RetryPolicy = raw
	}

	if task.StartDeadline != nil {
		ns := int64(*task.StartDeadline)
		params.StartDeadlineNs = &ns
	}

	return params, nil
}

func taskFromRow(row sqlc.Task) (domain.Task, error) {
	task := domain.Task{
		WorkerID:     row.WorkerID,
		TaskID:       row.TaskID,
		RegisteredAt: row.RegisteredAt,
	}

	if row.RetryPolicy != nil {
		retry, err := retryFromRaw(*row.RetryPolicy)
		if err != nil {
			return domain.Task{}, err
		}

		task.Retry = &retry
	}

	if row.StartDeadlineNs != nil {
		deadline := time.Duration(*row.StartDeadlineNs)
		task.StartDeadline = &deadline
	}

	return task, nil
}

// retryJSON is how a RetryPolicy is stored: in a Task's retry_policy, a Step's policy on a heartbeat.
type retryJSON struct {
	MaxAttempts int     `json:"max_attempts"`
	DelaysNs    []int64 `json:"delays_ns"`
}

// retryRawParams encodes a policy for a jsonb column. A nil policy is a NULL.
func retryRawParams(p *domain.RetryPolicy) (*json.RawMessage, error) {
	if p == nil {
		return nil, nil
	}

	raw, err := json.Marshal(retryToJSON(*p))
	if err != nil {
		return nil, fmt.Errorf("marshal retry policy: %w", err)
	}

	msg := json.RawMessage(raw)
	return &msg, nil
}

func retryFromRaw(raw json.RawMessage) (domain.RetryPolicy, error) {
	var rj retryJSON
	if err := json.Unmarshal(raw, &rj); err != nil {
		return domain.RetryPolicy{}, fmt.Errorf("unmarshal retry policy: %w", err)
	}

	return retryFromJSON(rj), nil
}

func retryToJSON(r domain.RetryPolicy) retryJSON {
	out := retryJSON{MaxAttempts: r.MaxAttempts}
	for _, d := range r.Delays {
		out.DelaysNs = append(out.DelaysNs, int64(d))
	}

	return out
}

func retryFromJSON(rj retryJSON) domain.RetryPolicy {
	out := domain.RetryPolicy{MaxAttempts: rj.MaxAttempts}
	for _, ns := range rj.DelaysNs {
		out.Delays = append(out.Delays, time.Duration(ns))
	}

	return out
}
