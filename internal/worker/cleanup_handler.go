package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// cleanupBatch is how many records one cleanup call removes at most.
const cleanupBatch = 500

// The names of the cleanup jobs, as the scheduler reports them.
const (
	runsCleanupJob = "runs.cleanup"
	keysCleanupJob = "idempotency-keys.cleanup"
)

// cleanupHandler removes the finished Runs that are past their retention period, with
// their Steps, and the idempotency keys whose window has passed. Each is a job of its
// own. A job runs in batches, so a large cleanup never holds one long transaction.
type cleanupHandler struct {
	cfg    Config
	logger *slog.Logger
	runs   RunStore
	now    func() time.Time
}

func newCleanupHandler(cfg Config, logger *slog.Logger, runs RunStore, now func() time.Time) *cleanupHandler {
	return &cleanupHandler{cfg: cfg, logger: logger, runs: runs, now: now}
}

// registerJobs adds the cleanup jobs to the scheduler. Each runs at start, then on every
// interval. A run that comes while the previous one is still going is skipped.
func (h *cleanupHandler) registerJobs(scheduler gocron.Scheduler) error {
	jobs := []struct {
		name string
		run  func(ctx context.Context)
	}{
		{runsCleanupJob, h.cleanupRuns},
		{keysCleanupJob, h.cleanupKeys},
	}

	for _, job := range jobs {
		_, err := scheduler.NewJob(
			gocron.DurationJob(h.cfg.Cleanup.Interval),
			gocron.NewTask(job.run),
			gocron.WithName(job.name),
			gocron.WithSingletonMode(gocron.LimitModeReschedule),
			gocron.WithStartAt(gocron.WithStartImmediately()),
		)
		if err != nil {
			return fmt.Errorf("new job %s: %w", job.name, err)
		}
	}

	return nil
}

// cleanupRuns removes the finished Runs that finished before the retention period.
// A failure is logged, and the next run tries again.
func (h *cleanupHandler) cleanupRuns(ctx context.Context) {
	cutoff := h.now().Add(-h.cfg.Cleanup.RunRetention)
	removed, err := drainBatches(cleanupBatch, func(limit int) (int, error) {
		return h.runs.DeleteFinishedBefore(ctx, cutoff, limit)
	})

	if removed > 0 {
		h.logger.InfoContext(ctx, "runs cleaned up", slog.Int("runs_removed", removed))
	}

	if err != nil && ctx.Err() == nil {
		h.logger.ErrorContext(ctx, "runs cleanup failed", slog.Any("error", err))
	}
}

// cleanupKeys removes the idempotency keys whose window has passed. A failure is logged,
// and the next run tries again.
func (h *cleanupHandler) cleanupKeys(ctx context.Context) {
	now := h.now()
	pruned, err := drainBatches(cleanupBatch, func(limit int) (int, error) {
		return h.runs.PruneKeys(ctx, now, limit)
	})

	if pruned > 0 {
		h.logger.InfoContext(ctx, "idempotency keys cleaned up", slog.Int("keys_pruned", pruned))
	}

	if err != nil && ctx.Err() == nil {
		h.logger.ErrorContext(ctx, "idempotency keys cleanup failed", slog.Any("error", err))
	}
}

// drainBatches calls step with a limit until a batch comes back short, and returns the total.
// A failing batch stops the loop, and its error is returned with the total so far.
func drainBatches(batch int, step func(limit int) (int, error)) (int, error) {
	total := 0
	for {
		n, err := step(batch)
		total += n
		if err != nil {
			return total, err
		}

		if n < batch {
			return total, nil
		}
	}
}
