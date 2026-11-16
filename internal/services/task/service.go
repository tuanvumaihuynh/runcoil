package task

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

// The errors the service returns to clients.
var (
	ErrTaskNotFound     = apperr.NotFound(apperr.CodeNotFound, "task not found")
	ErrInvalidPageToken = apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")
)

var _ services.TaskService = (*Service)(nil)

// Service reads the Tasks of a project with their schedules.
type Service struct {
	taskStore store.TaskStore
}

// New creates the task service.
func New(taskStore store.TaskStore) *Service {
	return &Service{taskStore: taskStore}
}

// ListTasks returns one page of the project's Tasks, oldest first.
func (s *Service) ListTasks(ctx context.Context, projectID uuid.UUID, params paging.Params) (paging.List[domain.TaskSummary], error) {
	afterID, err := params.AfterID()
	if err != nil {
		return paging.List[domain.TaskSummary]{}, ErrInvalidPageToken
	}

	tasks, err := s.taskStore.ListTasks(ctx, store.ListTasksParams{
		ProjectID: projectID,
		AfterID:   afterID,
		Limit:     params.Limit(),
	})
	if err != nil {
		return paging.List[domain.TaskSummary]{}, fmt.Errorf("list tasks: %w", err)
	}

	return paging.NewList(params, tasks, func(task domain.TaskSummary) uuid.UUID { return task.ID }), nil
}

// GetTask returns a Task of the project with its settings, schedule and live workers.
func (s *Service) GetTask(ctx context.Context, projectID, taskID uuid.UUID) (domain.TaskDetail, error) {
	task, err := s.taskStore.GetTaskDetail(ctx, projectID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrTaskNotFound) {
			return domain.TaskDetail{}, ErrTaskNotFound
		}

		return domain.TaskDetail{}, fmt.Errorf("get task: %w", err)
	}

	return task, nil
}
