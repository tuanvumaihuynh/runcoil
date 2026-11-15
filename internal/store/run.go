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
}
