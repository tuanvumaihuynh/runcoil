package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.TaskStore = (*Store)(nil)

// ListTasks returns at most params.Limit Tasks of the project, oldest first, each with its
// schedule, its newest Run and its count of live workers.
func (s *Store) ListTasks(ctx context.Context, params store.ListTasksParams) ([]domain.TaskSummary, error) {
	rows, err := s.queries.TaskList(ctx, s.db, gen.TaskListParams{
		ProjectID: params.ProjectID,
		AfterID:   params.AfterID,
		PageLimit: params.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]domain.TaskSummary, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, taskSummaryFromRow(row))
	}

	return tasks, nil
}

// GetTaskDetail returns the Task of the project with its settings, schedule, newest Run
// and live workers. It returns domain.ErrTaskNotFound when the project has no such Task.
func (s *Store) GetTaskDetail(ctx context.Context, projectID, taskID uuid.UUID) (domain.TaskDetail, error) {
	row, err := s.queries.TaskGet(ctx, s.db, gen.TaskGetParams{ProjectID: projectID, ID: taskID})
	if err != nil {
		if isNoRowsError(err) {
			return domain.TaskDetail{}, domain.ErrTaskNotFound
		}

		return domain.TaskDetail{}, fmt.Errorf("get task: %w", err)
	}

	workers, err := s.queries.WorkerListLiveByTask(ctx, s.db, gen.WorkerListLiveByTaskParams{ProjectID: projectID, TaskID: taskID})
	if err != nil {
		return domain.TaskDetail{}, fmt.Errorf("list workers of task: %w", err)
	}

	task, err := taskDetailFromRow(row, workers)
	if err != nil {
		return domain.TaskDetail{}, fmt.Errorf("read task %s: %w", taskID, err)
	}

	return task, nil
}
