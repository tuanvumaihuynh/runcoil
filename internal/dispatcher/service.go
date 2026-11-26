package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

// shutdownTimeout bounds how long a stop waits for the open calls to return and the last
// heartbeats to be written.
const shutdownTimeout = 10 * time.Second

// Config is the port of the worker protocol, and the timing of the dispatcher and of the
// worker sessions it holds.
type Config struct {
	Port              int
	ClaimInterval     time.Duration
	HeartbeatInterval time.Duration
	WorkerTTL         time.Duration
	// MaxLostAttempts is the scheduler's run.max-lost-attempts, which main copies here: a
	// Run whose worker dropped it without a result more often fails. It has no key of its
	// own.
	MaxLostAttempts int
	// Retry is the policy of a failed Step whose Run and Step set none.
	Retry RetryConfig
}

// RetryConfig is the server's default retry policy. Jitter spreads each delay by up to
// that fraction, either way, so Steps that failed together do not retry together.
type RetryConfig struct {
	MaxAttempts int
	Delays      []time.Duration
	Jitter      float64
}

// RegisterFlags declares the retry.* keys with their defaults and help text.
func (c *RetryConfig) RegisterFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.MaxAttempts, "retry.max-attempts", 4, "Attempts of a failed Step, the first one included, when neither the Step nor its Task sets them.")
	fs.DurationSliceVar(&c.Delays, "retry.delays",
		[]time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 3 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour},
		"Delays before each retry when neither the Step nor its Task sets them. The last one repeats.")
	fs.Float64Var(&c.Jitter, "retry.jitter", 0.2, "Fraction by which each retry delay is spread either way, from 0 to 1.")
}

// Validate checks the default policy against the limits of every policy, and the jitter.
func (c *RetryConfig) Validate() error {
	if c.MaxAttempts < 1 {
		return errors.New("max attempts must be at least 1")
	}

	if len(c.Delays) == 0 {
		return errors.New("delays must list at least one duration")
	}

	if err := c.Policy().Validate(); err != nil {
		return fmt.Errorf("policy: %w", err)
	}

	if c.Jitter < 0 || c.Jitter > 1 {
		return errors.New("jitter must be between 0 and 1")
	}

	return nil
}

// Policy returns the default policy.
func (c RetryConfig) Policy() domain.RetryPolicy {
	return domain.RetryPolicy{MaxAttempts: c.MaxAttempts, Delays: c.Delays}
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "dispatcher.port", 7070, "Port of the worker protocol.")
	fs.DurationVar(&c.ClaimInterval, "dispatcher.claim-interval", time.Second, "How often the dispatcher claims queued Runs for the workers with free slots.")
	fs.DurationVar(&c.HeartbeatInterval, "dispatcher.heartbeat-interval", 10*time.Second, "How often a worker sends a heartbeat, and the server a keepalive.")
	fs.DurationVar(&c.WorkerTTL, "dispatcher.worker-ttl", 30*time.Second, "With no heartbeat for longer than this, a worker session ends.")
	c.Retry.RegisterFlags(fs)
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	if c.ClaimInterval <= 0 {
		return errors.New("claim interval must be greater than 0")
	}

	if c.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be greater than 0")
	}

	if c.WorkerTTL <= c.HeartbeatInterval {
		return errors.New("worker ttl must be longer than the heartbeat interval")
	}

	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("retry: %w", err)
	}

	return nil
}

// Service serves the worker protocol on its own port. It holds the streams of the
// connected workers, claims queued Runs for the ones with free slots, and flushes their
// heartbeats.
type Service struct {
	cfg         Config
	logger      *slog.Logger
	workerStore store.WorkerStore
	runStore    store.RunStore
	apiKeyStore store.APIKeyStore

	// id is this dispatcher's. Each stream it holds carries it, and a claim checks it,
	// so only the dispatcher holding a worker's stream claims Runs for that worker.
	id uuid.UUID

	mu      sync.Mutex
	streams map[uuid.UUID]*stream

	beatsMu sync.Mutex
	beats   map[uuid.UUID]time.Time

	keyUsesMu sync.Mutex
	keyUses   map[uuid.UUID]time.Time

	serving  atomic.Bool
	stopped  chan struct{}
	stopOnce sync.Once
}

// New creates the dispatcher. It serves and claims nothing until Run.
func New(cfg Config, logger *slog.Logger, workerStore store.WorkerStore, runStore store.RunStore, apiKeyStore store.APIKeyStore) *Service {
	return &Service{
		cfg:         cfg,
		logger:      logger,
		workerStore: workerStore,
		runStore:    runStore,
		apiKeyStore: apiKeyStore,
		id:          uuid.NewV7(),
		streams:     make(map[uuid.UUID]*stream),
		beats:       make(map[uuid.UUID]time.Time),
		keyUses:     make(map[uuid.UUID]time.Time),
		stopped:     make(chan struct{}),
	}
}

// Run serves the worker protocol, claims, flushes heartbeats and records that this
// dispatcher is alive until Stop is called or ctx ends.
func (s *Service) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.serve(ctx, listener)
}

func (s *Service) serve(ctx context.Context, listener net.Listener) error {
	if err := s.workerStore.BeatDispatcher(ctx, s.id); err != nil {
		return fmt.Errorf("beat dispatcher: %w", errors.Join(err, listener.Close()))
	}

	srv := s.newHTTPServer()

	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(listener)
	}()

	s.serving.Store(true)
	defer s.serving.Store(false)

	s.logger.InfoContext(ctx, "dispatcher listening",
		slog.String("addr", listener.Addr().String()),
		slog.String("dispatcher_id", s.id.String()),
	)

	err := s.loop(ctx, served)

	s.mu.Lock()
	open := len(s.streams)
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "dispatcher stopping", slog.Int("streams", open))

	// The streams end when stopped is closed, so the shutdown does not wait for them. One
	// drain context bounds the shutdown and the last flush, which runs after the shutdown
	// so it writes the heartbeats that arrived during it.
	s.Stop()

	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(drainCtx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("shutdown http: %w", errors.Join(shutdownErr, srv.Close())))
	}

	s.flushHeartbeats(drainCtx)
	s.flushKeyUses(drainCtx)

	return err
}

// loop claims, flushes and beats until Stop is called, ctx ends or the server fails.
func (s *Service) loop(ctx context.Context, served <-chan error) error {
	claim := time.NewTicker(s.cfg.ClaimInterval)
	defer claim.Stop()

	flush := time.NewTicker(s.cfg.WorkerTTL / 3)
	defer flush.Stop()

	beat := time.NewTicker(s.cfg.HeartbeatInterval)
	defer beat.Stop()

	for {
		select {
		case <-s.stopped:
			return nil
		case <-ctx.Done():
			return nil
		case err := <-served:
			return fmt.Errorf("serve http: %w", err)
		case <-claim.C:
			s.claimAll(ctx)
		case <-flush.C:
			s.flushHeartbeats(ctx)
			s.flushKeyUses(ctx)
		case <-beat.C:
			if err := s.workerStore.BeatDispatcher(ctx, s.id); err != nil {
				s.logger.ErrorContext(ctx, "beat dispatcher failed", slog.Any("error", err))
			}
		}
	}
}

// Ready reports whether the dispatcher takes workers: it serves and is not stopping.
func (s *Service) Ready(_ context.Context) error {
	if s.isStopping() {
		return errors.New("shutting down")
	}

	if !s.serving.Load() {
		return errors.New("not serving")
	}

	return nil
}

// Stop ends the streams and the loops. It is safe to call more than once, and before Run.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

func (s *Service) isStopping() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// claimAll claims queued Runs for every stream with free slots and pushes them.
func (s *Service) claimAll(ctx context.Context) {
	for _, c := range s.claimable() {
		assigns, err := s.workerStore.ClaimRuns(ctx, domain.ClaimRunsParams{
			WorkerID:     c.workerID,
			DispatcherID: s.id,
			StreamID:     c.streamID,
			TaskIDs:      c.taskIDs,
			Slots:        c.free,
		})
		if err != nil {
			if errors.Is(err, domain.ErrStreamFenced) {
				s.logger.InfoContext(ctx, "stream fenced",
					slog.String("worker_id", c.workerID.String()),
					slog.String("stream_id", c.streamID.String()),
				)
				s.end(c.workerID, c.streamID)

				continue
			}

			// The claim may have committed before the error, so the worker reconnects and
			// the reconcile puts what it was never sent back in the queue, counted as lost.
			s.logger.ErrorContext(ctx, "claim runs failed, ending the stream",
				slog.String("worker_id", c.workerID.String()),
				slog.Any("error", err),
			)
			s.end(c.workerID, c.streamID)

			continue
		}

		s.push(ctx, c.workerID, c.streamID, assigns)
	}
}

// flushHeartbeats writes the heartbeats received since the last flush.
func (s *Service) flushHeartbeats(ctx context.Context) {
	s.beatsMu.Lock()
	beats := s.beats
	s.beats = make(map[uuid.UUID]time.Time, len(beats))
	s.beatsMu.Unlock()

	if len(beats) == 0 {
		return
	}

	ids := make([]uuid.UUID, 0, len(beats))
	times := make([]time.Time, 0, len(beats))
	for id, at := range beats {
		ids = append(ids, id)
		times = append(times, at)
	}

	if err := s.workerStore.FlushHeartbeats(ctx, ids, times); err != nil {
		s.logger.ErrorContext(ctx, "flush heartbeats failed", slog.Any("error", err))
		s.keepHeartbeats(beats)

		return
	}

	s.logger.DebugContext(ctx, "heartbeats flushed", slog.Int("count", len(ids)))
}

// keepHeartbeats puts back the heartbeats a failed flush did not write, so the next flush
// tries again. A heartbeat received since then is newer and stays.
func (s *Service) keepHeartbeats(beats map[uuid.UUID]time.Time) {
	s.beatsMu.Lock()
	defer s.beatsMu.Unlock()

	for id, at := range beats {
		if newer, ok := s.beats[id]; ok && newer.After(at) {
			continue
		}

		s.beats[id] = at
	}
}

// recordKeyUse notes that a worker registered with the API key. The next flush writes it.
func (s *Service) recordKeyUse(apiKeyID uuid.UUID) {
	s.keyUsesMu.Lock()
	defer s.keyUsesMu.Unlock()

	s.keyUses[apiKeyID] = time.Now()
}

// flushKeyUses writes when each API key was last used, for the keys used since the last
// flush.
func (s *Service) flushKeyUses(ctx context.Context) {
	s.keyUsesMu.Lock()
	uses := s.keyUses
	s.keyUses = make(map[uuid.UUID]time.Time, len(uses))
	s.keyUsesMu.Unlock()

	if len(uses) == 0 {
		return
	}

	ids := make([]uuid.UUID, 0, len(uses))
	times := make([]time.Time, 0, len(uses))
	for id, at := range uses {
		ids = append(ids, id)
		times = append(times, at)
	}

	if err := s.apiKeyStore.TouchAPIKeys(ctx, ids, times); err != nil {
		s.logger.ErrorContext(ctx, "flush api key uses failed", slog.Any("error", err))
		s.keepKeyUses(uses)
	}
}

// keepKeyUses puts back the uses a failed flush did not write, so the next flush tries
// again. A use recorded since then is newer and stays.
func (s *Service) keepKeyUses(uses map[uuid.UUID]time.Time) {
	s.keyUsesMu.Lock()
	defer s.keyUsesMu.Unlock()

	for id, at := range uses {
		if newer, ok := s.keyUses[id]; ok && newer.After(at) {
			continue
		}

		s.keyUses[id] = at
	}
}
