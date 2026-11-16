package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/pkg/paging"
)

// TaskService reads the Tasks of a project with their schedules.
type TaskService interface {
	ListTasks(ctx context.Context, projectID uuid.UUID, params paging.Params) (paging.List[domain.TaskSummary], error)
	GetTask(ctx context.Context, projectID, taskID uuid.UUID) (domain.TaskDetail, error)
}
