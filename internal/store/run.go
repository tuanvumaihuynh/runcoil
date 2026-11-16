package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// RunStore keeps the Runs, their leases and their journal.
type RunStore interface {
	CompleteStep(ctx context.Context, params domain.CompleteStepParams, decide domain.RetryDecider) (domain.StepResult, error)
	FinishRun(ctx context.Context, workerID uuid.UUID, lease domain.Lease, end domain.RunEnd) (bool, error)
	PromoteDueDelayed(ctx context.Context, batchSize int) (int, error)
	ExpireStartDeadlines(ctx context.Context, batchSize int) (int, error)
	FireDueSchedules(ctx context.Context, batchSize int) (domain.ScheduleTicks, error)
	ListRuns(ctx context.Context, params ListRunsParams) ([]domain.RunSummary, error)
	GetRunDetail(ctx context.Context, projectID, runID uuid.UUID) (domain.RunDetail, error)
}

// ListRunsParams asks for at most Limit Runs of a project that match Filter, newest
// first, after AfterID when it is set.
type ListRunsParams struct {
	ProjectID uuid.UUID
	Filter    domain.RunFilter
	AfterID   *uuid.UUID
	Limit     int32
}
