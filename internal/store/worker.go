package store

import (
	"context"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// WorkerStore keeps the worker sessions, the Tasks they declare, the dispatchers, and the
// claim of queued Runs.
type WorkerStore interface {
	Register(ctx context.Context, keyHash []byte, session domain.WorkerSession) (domain.Worker, error)
	GetWorkerBySessionToken(ctx context.Context, tokenHash []byte) (domain.Worker, error)
	AttachStream(ctx context.Context, params domain.AttachStreamParams) (domain.AttachStreamResult, error)
	FlushHeartbeats(ctx context.Context, ids []uuid.UUID, beats []time.Time) error
	BeatDispatcher(ctx context.Context, id uuid.UUID) error
	ClaimRuns(ctx context.Context, params domain.ClaimRunsParams) ([]domain.Assign, error)
	Deregister(ctx context.Context, workerID uuid.UUID) (int, error)
	EndDeadWorkers(ctx context.Context, ttl time.Duration, maxLost, batchSize int) (domain.DeadWorkers, error)
}
