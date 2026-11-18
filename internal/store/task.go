package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// TaskStore reads the Tasks of a project with their schedules.
type TaskStore interface {
	ListTasks(ctx context.Context, params ListTasksParams) ([]domain.TaskSummary, error)
	GetTaskDetail(ctx context.Context, projectID, taskID uuid.UUID) (domain.TaskDetail, error)
}

// ListTasksParams asks for at most Limit Tasks of a project, oldest first, after AfterID
// when it is set.
type ListTasksParams struct {
	ProjectID uuid.UUID
	AfterID   *uuid.UUID
	Limit     int32
}
