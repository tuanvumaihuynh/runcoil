package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/store"
)

// batchSize is how many Runs one tick of a job handles: 500 a second per server with the
// default intervals. More is left for the next tick.
const batchSize = 500

// workerBatchSize is how many dead worker sessions one tick of the reaper ends. Each may
// hold up to domain.MaxSlots attempts.
const workerBatchSize = 50

// maxLostAttemptsCap bounds run.max-lost-attempts.
const maxLostAttemptsCap = 100

// Config is how often each job of the scheduler runs, and what the reaper allows.
type Config struct {
	RequeueDelayedRunsInterval time.Duration
	StartDeadlineCheckInterval time.Duration
	DeadWorkerCheckInterval    time.Duration
	ScheduleCheckInterval      time.Duration
	// MaxLostAttempts is how often a Run may lose its worker before it fails with
	// attempt_lost.
	MaxLostAttempts int
	WorkerTTL       time.Duration
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&c.RequeueDelayedRunsInterval, "scheduler.requeue-delayed-runs-interval", time.Second,
		"How often Runs whose wait is over, such as the backoff before a Step's retry, go back to the queue.")
	fs.DurationVar(&c.StartDeadlineCheckInterval, "scheduler.start-deadline-check-interval", time.Second,
		"How often queued Runs that did not start before their start deadline are failed.")
	fs.DurationVar(&c.DeadWorkerCheckInterval, "scheduler.dead-worker-check-interval", 5*time.Second,
		"How often worker sessions with no heartbeat for longer than dispatcher.worker-ttl are ended, and their Runs go back to the queue.")
	fs.DurationVar(&c.ScheduleCheckInterval, "scheduler.schedule-check-interval", time.Second,
		"How often schedules whose tick has come make their Runs. A cron with seconds fires no more often than this.")
	fs.IntVar(&c.MaxLostAttempts, "run.max-lost-attempts", 5,
		"How often a Run may lose its worker, which stopped while running it, before the Run fails with attempt_lost.")
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.RequeueDelayedRunsInterval <= 0 {
		return errors.New("requeue delayed runs interval must be greater than 0")
	}

	if c.StartDeadlineCheckInterval <= 0 {
		return errors.New("start deadline check interval must be greater than 0")
	}

	if c.DeadWorkerCheckInterval <= 0 {
		return errors.New("dead worker check interval must be greater than 0")
	}

	if c.ScheduleCheckInterval <= 0 {
		return errors.New("schedule check interval must be greater than 0")
	}

	if c.MaxLostAttempts < 0 || c.MaxLostAttempts > maxLostAttemptsCap {
		return fmt.Errorf("max lost attempts must be between 0 and %d", maxLostAttemptsCap)
	}

	return nil
}

// Service does what is due at a time: it moves Runs whose wait in run_delayed is over to
// the queue, fails queued Runs past their start deadline, ends the sessions of dead
// workers, and makes the Runs of schedules whose tick has come. Each batch locks its rows with SKIP LOCKED, so several servers can run one
// each.
type Service struct {
	cfg         Config
	logger      *slog.Logger
	runStore    store.RunStore
	workerStore store.WorkerStore

	running  atomic.Bool
	stopped  chan struct{}
	stopOnce sync.Once

	// startedAt is when Run started. The reaper waits one worker TTL from it.
	startedAt time.Time
}

// New creates the scheduler. It does nothing until Run.
func New(cfg Config, logger *slog.Logger, runStore store.RunStore, workerStore store.WorkerStore) *Service {
	return &Service{
		cfg:         cfg,
		logger:      logger,
		runStore:    runStore,
		workerStore: workerStore,
		stopped:     make(chan struct{}),
	}
}

// Run does each job on its own ticker until Stop is called or ctx ends. Each job runs one
// batch per tick, so Stop is seen within one batch, and a backlog in one job never holds
// up the others.
func (s *Service) Run(ctx context.Context) error {
	s.startedAt = time.Now()
	s.running.Store(true)
	defer s.running.Store(false)

	requeue := time.NewTicker(s.cfg.RequeueDelayedRunsInterval)
	defer requeue.Stop()

	deadlines := time.NewTicker(s.cfg.StartDeadlineCheckInterval)
	defer deadlines.Stop()

	deadWorkers := time.NewTicker(s.cfg.DeadWorkerCheckInterval)
	defer deadWorkers.Stop()

	schedules := time.NewTicker(s.cfg.ScheduleCheckInterval)
	defer schedules.Stop()

	for {
		select {
		case <-s.stopped:
			return nil
		case <-ctx.Done():
			return nil
		case <-requeue.C:
			s.requeueDelayedRuns(ctx)
		case <-deadlines.C:
			s.failRunsPastStartDeadline(ctx)
		case <-deadWorkers.C:
			s.endDeadWorkers(ctx)
		case <-schedules.C:
			s.fireDueSchedules(ctx)
		}
	}
}

// Stop ends Run. It is safe to call more than once, and before Run.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

// Ready reports whether the scheduler runs.
func (s *Service) Ready(_ context.Context) error {
	if !s.running.Load() {
		return errors.New("not running")
	}

	return nil
}

// requeueDelayedRuns puts one batch of Runs whose wait in run_delayed is over, such as
// the backoff before a retry, back in the queue. A failed batch is tried again at the next
// tick.
func (s *Service) requeueDelayedRuns(ctx context.Context) {
	n, err := s.runStore.PromoteDueDelayed(ctx, batchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "requeue delayed runs failed", slog.Any("error", err))
		}

		return
	}

	if n > 0 {
		s.logger.DebugContext(ctx, "delayed runs requeued", slog.Int("runs", n))
	}
}

// failRunsPastStartDeadline fails one batch of queued Runs that did not start before their
// start deadline. A failed batch is tried again at the next tick.
func (s *Service) failRunsPastStartDeadline(ctx context.Context) {
	n, err := s.runStore.ExpireStartDeadlines(ctx, batchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "fail runs past their start deadline failed", slog.Any("error", err))
		}

		return
	}

	if n > 0 {
		s.logger.InfoContext(ctx, "runs failed past their start deadline", slog.Int("runs", n))
	}
}

// endDeadWorkers ends one batch of worker sessions with no heartbeat for longer than the
// worker TTL, and puts the Runs they held back in the queue, or fails the Runs lost too
// often. A failed batch is tried again at the next tick.
//
// It does nothing for one worker TTL after Run starts. While the server was down, nothing
// wrote the heartbeats of its workers, so a worker that reconnects at once still looks
// dead until the first flush after it; the wait gives every live worker a TTL to
// reconnect and send a heartbeat.
func (s *Service) endDeadWorkers(ctx context.Context) {
	if time.Since(s.startedAt) < s.cfg.WorkerTTL {
		return
	}

	dead, err := s.workerStore.EndDeadWorkers(ctx, s.cfg.WorkerTTL, s.cfg.MaxLostAttempts, workerBatchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "end dead workers failed", slog.Any("error", err))
		}

		return
	}

	if dead.Ended > 0 {
		s.logger.InfoContext(ctx, "dead workers ended",
			slog.Int("workers", dead.Ended),
			slog.Int("runs_requeued", dead.Requeued),
			slog.Int("runs_failed", dead.Failed),
		)
	}
}

// fireDueSchedules fires one batch of schedules whose tick has come. A failed batch is
// tried again at the next tick; the ticks it missed fire once, as after downtime.
func (s *Service) fireDueSchedules(ctx context.Context) {
	ticks, err := s.runStore.FireDueSchedules(ctx, batchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "fire due schedules failed", slog.Any("error", err))
		}

		return
	}

	if ticks.Broken > 0 {
		s.logger.ErrorContext(ctx, "schedules with a cron that cannot be parsed", slog.Int("schedules", ticks.Broken))
	}

	if ticks.Fired > 0 || ticks.Skipped > 0 {
		s.logger.DebugContext(ctx, "schedules fired",
			slog.Int("runs", ticks.Fired),
			slog.Int("skipped", ticks.Skipped),
		)
	}
}
