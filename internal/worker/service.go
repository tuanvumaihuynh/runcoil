// Package worker runs the server's scheduled background jobs.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// RunStore is what the cleanup needs from storage.
type RunStore interface {
	DeleteFinishedBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
	PruneKeys(ctx context.Context, now time.Time, limit int) (int, error)
}

// shutdownTimeout bounds the wait for the running jobs when the service stops.
const shutdownTimeout = 10 * time.Second

// Config is the configuration of the worker service.
type Config struct {
	// Cleanup is the cleanup of finished Runs and of expired idempotency keys.
	Cleanup CleanupConfig `yaml:"cleanup"`
}

// CleanupConfig is how often the cleanup runs, and how long a finished Run is kept.
type CleanupConfig struct {
	// Interval is how often the cleanup jobs run.
	Interval time.Duration `yaml:"interval"`
	// RunRetention is how long a finished Run is kept, counted from when it finished.
	RunRetention time.Duration `yaml:"run_retention"`
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.Cleanup.Interval <= 0 {
		return errors.New("cleanup interval must be greater than 0")
	}

	if c.Cleanup.RunRetention <= 0 {
		return errors.New("run retention must be greater than 0")
	}

	return nil
}

// Service runs the scheduled jobs of the server. NewService composes it without
// starting anything, and Run starts the jobs and blocks until Stop.
type Service struct {
	logger  *slog.Logger
	cleanup *cleanupHandler
	// stopped is closed when the service is asked to stop, so that Run shuts down.
	stopped  chan struct{}
	stopOnce sync.Once
}

// NewService composes the worker service on the given store. It starts nothing.
func NewService(cfg Config, logger *slog.Logger, runs RunStore) *Service {
	clock := func() time.Time { return time.Now().UTC() }
	return &Service{
		logger:  logger,
		cleanup: newCleanupHandler(cfg, logger, runs, clock),
		stopped: make(chan struct{}),
	}
}

// Run starts the scheduler with the cleanup jobs, and each job runs at start and then on
// its interval. It blocks until Stop is called or ctx ends, then shuts the scheduler down.
// The shutdown ends the context of every job, and waits for the running jobs for a bounded
// time. A failure to register a job fails the start.
func (s *Service) Run(ctx context.Context) error {
	scheduler, err := newScheduler(s.cleanup)
	if err != nil {
		return err
	}

	scheduler.Start()

	s.logger.InfoContext(
		ctx,
		"worker service started",
		slog.Duration("cleanup_interval", s.cleanup.cfg.Cleanup.Interval),
		slog.Duration("run_retention", s.cleanup.cfg.Cleanup.RunRetention),
	)

	select {
	case <-s.stopped:
	case <-ctx.Done():
	}

	s.logger.InfoContext(ctx, "worker service is shutting down")

	// The context of each job is ended when the shutdown begins, so a running job is asked
	// to stop first. The wait is bounded, so a job that ignores its context holds the
	// shutdown for at most shutdownTimeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err = scheduler.ShutdownWithContext(shutdownCtx)

	s.logger.InfoContext(ctx, "worker service is stopped")

	if err != nil {
		// The manager does not report an error that comes during the shutdown, so it is logged here.
		s.logger.ErrorContext(ctx, "shut down scheduler", slog.Any("error", err))
		return fmt.Errorf("shut down scheduler: %w", err)
	}

	return nil
}

// Stop starts a graceful shutdown: Run shuts the scheduler down and returns. It is safe
// to call more than once, and before Run.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

// newScheduler creates a scheduler in UTC and registers the cleanup jobs on it. It does not
// start the scheduler. A failure to register a job shuts the scheduler down again.
func newScheduler(cleanup *cleanupHandler) (gocron.Scheduler, error) {
	scheduler, err := gocron.NewScheduler(gocron.WithLocation(time.UTC))
	if err != nil {
		return nil, fmt.Errorf("new scheduler: %w", err)
	}

	if err := cleanup.registerJobs(scheduler); err != nil {
		return nil, errors.Join(fmt.Errorf("register jobs: %w", err), scheduler.Shutdown())
	}

	return scheduler, nil
}
