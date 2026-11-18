package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/pkg/paging"
)

// TaskService reads the Tasks of a project with their schedules.
type TaskService interface {
	ListTasks(ctx context.Context, params ListTasksParams) (paging.List[domain.TaskSummary], error)
	GetTask(ctx context.Context, params GetTaskParams) (domain.TaskDetail, error)
}

// ListTasksParams asks for one page of a project's Tasks, oldest first.
type ListTasksParams struct {
	ProjectID uuid.UUID
	Paging    paging.Params
}

// GetTaskParams names one Task of a project.
type GetTaskParams struct {
	ProjectID uuid.UUID
	TaskID    uuid.UUID
}
