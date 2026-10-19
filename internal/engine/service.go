// Package engine is the core of the server. It holds the dispatcher that starts
// attempts and the scheduler that creates the Runs of schedule ticks. Its methods carry
// out each operation of the protocol. The ConnectRPC handlers only map messages to these
// methods and back.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	rand "math/rand/v2"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// Service serves the worker and run APIs and dispatches Runs to connected workers.
type Service struct {
	cfg        Config
	retry      domain.RetryConfig
	limits     domain.Limits
	logger     *slog.Logger
	tasks      TaskStore
	runs       RunStore
	queue      RunQueue
	schedules  ScheduleStore
	database   Pinger
	signals    Signals
	instances  *instances
	dispatcher *dispatcher
	scheduler  *scheduler
	// regMu serializes the register path, so that its steps apply in one order.
	regMu sync.Mutex
	// clock returns the current time. unit returns a random number in [0, 1) for jitter.
	clock func() time.Time
	unit  func() float64
	// after returns a channel that fires after d, and a function that stops the wait.
	// WaitRun uses it, so a test can fire its timeout.
	after func(d time.Duration) (<-chan time.Time, func())
	// waitSlots holds one token per WaitRun call that is waiting: a full channel refuses a call.
	waitSlots chan struct{}
	// stopped is closed when the service is asked to stop, so that waiting calls end and new
	// work is refused.
	stopped  chan struct{}
	stopOnce sync.Once
	// ready is set once the loops have started.
	ready atomic.Bool
	// known holds the instances this process has registered, for the first-registration log line.
	// It is written under regMu.
	known map[string]bool
}

// advanceBatch is how many schedules one page of the startup advance reads.
const advanceBatch = 500

// NewService creates the service on the given stores. It does not listen until Run. The
// engine's Signals is the in-process implementation, which all of its parts share.
func NewService(
	cfg Config,
	logger *slog.Logger,
	tasks TaskStore,
	runs RunStore,
	queue RunQueue,
	schedules ScheduleStore,
	database Pinger,
) *Service {
	clock := func() time.Time { return time.Now().UTC() }
	//nolint:gosec // Jitter only spreads retries out; it needs no secure randomness.
	unit := rand.Float64
	return newService(cfg, logger, tasks, runs, queue, schedules, database, newInProcessSignals(), clock, unit)
}

func newService(
	cfg Config,
	logger *slog.Logger,
	tasks TaskStore,
	runs RunStore,
	queue RunQueue,
	schedules ScheduleStore,
	database Pinger,
	signals Signals,
	clock func() time.Time,
	unit func() float64,
) *Service {
	inst := newInstances()
	s := &Service{
		cfg:       cfg,
		retry:     cfg.Retry.domain(),
		limits:    cfg.Limits.domain(),
		logger:    logger,
		tasks:     tasks,
		runs:      runs,
		queue:     queue,
		schedules: schedules,
		database:  database,
		signals:   signals,
		instances: inst,
		clock:     clock,
		unit:      unit,
		after:     realAfter,
		waitSlots: make(chan struct{}, cfg.WaitRun.MaxWaiters),
		stopped:   make(chan struct{}),
		known:     make(map[string]bool),
	}

	s.dispatcher = newDispatcher(
		dispatcherConfig{
			LeaseDuration:   cfg.LeaseDuration,
			SendTimeout:     cfg.SendTimeout,
			EvictAfter:      cfg.LeaseDuration * time.Duration(cfg.InstanceEvictionLeases),
			WorkerWaitLimit: cfg.WorkerWaitLimit,
			Retry:           cfg.Retry.domain(),
			PollInterval:    cfg.Dispatcher.PollInterval,
			SweepInterval:   cfg.Sweep.Interval,
		},
		logger,
		tasks,
		runs,
		queue,
		signals,
		inst,
		clock,
		unit,
	)

	s.scheduler = newScheduler(
		schedulerConfig{
			Window:       cfg.Scheduler.Window,
			PollInterval: cfg.Scheduler.PollInterval,
			MaxLoaded:    cfg.Scheduler.MaxLoaded,
		},
		logger,
		runs,
		queue,
		schedules,
		signals,
		inst,
		clock,
	)

	return s
}

// Run listens on the configured port, then advances the schedule ticks that were missed
// while the server was down, then starts the HTTP server, the dispatcher, and the scheduler.
// It blocks until Stop is called, ctx ends, or the HTTP server fails. Then it ends the
// waiting calls, stops the dispatcher and the scheduler, ends every worker stream, waits
// up to ShutdownTimeout for the attached workers to finish their Runs, and shuts the HTTP
// server down. The listen comes first, so
// a server whose port is taken fails before it changes any schedule. A failure to listen or
// to advance the missed ticks fails the start, closes the listener, and leaves nothing
// running, so no tick is skipped silently.
func (s *Service) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	if err := s.advanceMissed(ctx); err != nil {
		return errors.Join(fmt.Errorf("advance missed schedule ticks: %w", err), listener.Close())
	}

	srv := s.newHTTPServer(addr)

	// A lease is not swept until one lease duration has passed since start. Workers that
	// were attached before a restart need that long to reconnect and heartbeat.
	s.dispatcher.graceUntil(s.clock().Add(s.cfg.LeaseDuration))

	runCtx, cancel := context.WithCancel(ctx)

	served := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.ErrorContext(runCtx, "serve http", slog.Any("error", err))
		}

		served <- err
	}()

	dispatcherDone, schedulerDone := s.startLoops(runCtx)
	s.ready.Store(true)

	s.logger.InfoContext(
		ctx,
		"engine listening",
		slog.String("addr", listener.Addr().String()),
		slog.Duration("heartbeat_interval", s.cfg.HeartbeatInterval),
		slog.Duration("lease_duration", s.cfg.LeaseDuration),
		slog.Duration("scheduler_window", s.cfg.Scheduler.Window),
		slog.Duration("scheduler_poll_interval", s.cfg.Scheduler.PollInterval),
	)

	var serveErr error
	select {
	case <-s.stopped:
	case <-ctx.Done():
	case err := <-served:
		serveErr = fmt.Errorf("serve http: %w", err)
	}

	// One deadline covers the whole drain, the HTTP shutdown included.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer drainCancel()

	s.logger.InfoContext(ctx, "engine is shutting down")

	// Waiting calls end first, so that the HTTP shutdown does not wait for them.
	s.Stop()
	cancel()
	dispatcherStopped := waitDone(drainCtx, dispatcherDone)
	schedulerStopped := waitDone(drainCtx, schedulerDone)
	if !dispatcherStopped || !schedulerStopped {
		s.logger.WarnContext(
			ctx,
			"loops did not stop before the drain deadline",
			slog.Bool("dispatcher_stopped", dispatcherStopped),
			slog.Bool("scheduler_stopped", schedulerStopped),
		)
	}

	// A Listen stream never ends by itself, so end them before the server waits for them.
	attached := s.instances.Attached()
	s.instances.CloseAll()

	// Workers cannot report when HTTP failed.
	if serveErr == nil {
		s.waitForHeldRuns(drainCtx, attached)
	}

	shutdownErr := srv.Shutdown(drainCtx)
	if shutdownErr != nil {
		_ = srv.Close()
	}

	s.logger.InfoContext(ctx, "engine is stopped")

	if shutdownErr != nil {
		// The manager does not report an error that comes during the shutdown, so it is logged here.
		s.logger.ErrorContext(ctx, "shut down http server", slog.Any("error", shutdownErr))
		return errors.Join(serveErr, fmt.Errorf("shutdown http server: %w", shutdownErr))
	}

	return serveErr
}

// waitDone waits until done is closed, or until ctx ends. It reports whether done was closed.
func waitDone(ctx context.Context, done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// drainPollInterval is how often the drain counts the Runs that the attached workers hold.
const drainPollInterval = 250 * time.Millisecond

// waitForHeldRuns waits until the instances hold no running Run, or until ctx ends.
func (s *Service) waitForHeldRuns(ctx context.Context, instanceIDs []string) {
	held := 0
	for {
		n, err := s.countHeldRuns(ctx, instanceIDs)
		switch {
		case err != nil:
			if ctx.Err() == nil {
				s.logger.ErrorContext(ctx, "count held runs", slog.Any("error", err))
			}
		case n == 0:
			s.logger.InfoContext(ctx, "drain finished")
			return
		default:
			held = n
		}

		poll, stopPoll := s.after(drainPollInterval)
		select {
		case <-ctx.Done():
			stopPoll()
			s.logger.WarnContext(
				ctx,
				"drain timed out",
				slog.Int("held_runs", held),
				slog.String("note", "the held runs keep their leases until the server restarts"),
			)

			return
		case <-poll:
		}

		stopPoll()
	}
}

// countHeldRuns returns how many running Runs the instances hold.
func (s *Service) countHeldRuns(ctx context.Context, instanceIDs []string) (int, error) {
	total := 0
	for _, id := range instanceIDs {
		n, err := s.runs.CountRunning(ctx, id)
		if err != nil {
			return 0, err
		}

		total += n
	}

	return total, nil
}

// Stop starts a graceful shutdown: new work is refused, the waiting calls end, and Run
// drains the service and returns. It is safe to call more than once, and before Run.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

// readyPingTimeout bounds the database ping of the readiness endpoint.
const readyPingTimeout = 2 * time.Second

// newHTTPServer returns the HTTP server of the worker and run APIs, listening on addr.
func (s *Service) newHTTPServer(addr string) *http.Server {
	// The later checks run after the token check, so a caller without the token learns nothing from them.
	server := connect.NewServer(
		errorInterceptor{logger: s.logger}.wrap,
		requestLogger{logger: s.logger}.wrap,
		authInterceptor{token: s.cfg.Token}.wrap,
		drainInterceptor{stopped: s.stopped}.wrap,
		nulInterceptor{}.wrap,
	)
	runcoilv1connect.RegisterWorkerServiceHandler(server, workerHandler{s: s})
	runcoilv1connect.RegisterRunServiceHandler(server, runHandler{s: s})

	// The read cap bounds the body before it is decoded, so a request cannot grow the
	// process without limit. The size checks of the decisions apply within it.
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithReadMaxBytes(s.cfg.ReadMaxBytes()))

	// The health routes are not Connect calls, so they skip the token check.
	mux.HandleFunc("GET /livez", s.livez)
	mux.HandleFunc("GET /readyz", s.readyz)

	// HTTP/1.1 for plain requests, and HTTP/2 without TLS for streams.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Addr:      addr,
		Handler:   mux,
		Protocols: protocols,
		// IdleTimeout retires a connection that has had no request for this long. The
		// Listen streams that are open keep running; the stream test pins this.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// livez answers 200 whenever HTTP is served.
func (s *Service) livez(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, "ok")
}

// readyz answers 200 when the server takes new work, and 503 with the reason otherwise.
func (s *Service) readyz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		writeHealth(w, http.StatusServiceUnavailable, "starting")
		return
	}

	if isStopping(s.stopped) {
		writeHealth(w, http.StatusServiceUnavailable, "shutting down")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readyPingTimeout)
	defer cancel()

	if err := s.database.Ping(ctx); err != nil {
		s.logger.WarnContext(ctx, "database ping failed", slog.Any("error", err))
		writeHealth(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	writeHealth(w, http.StatusOK, "ok")
}

// writeHealth writes a plain-text health answer.
func writeHealth(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// startLoops starts the dispatcher and the scheduler until ctx ends.
func (s *Service) startLoops(ctx context.Context) (dispatcherDone, schedulerDone chan struct{}) {
	dispatcherDone = make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		s.dispatcher.run(ctx)
	}()

	schedulerDone = make(chan struct{})
	go func() {
		defer close(schedulerDone)
		s.scheduler.run(ctx)
	}()

	return dispatcherDone, schedulerDone
}

// realAfter is the production timer of WaitRun.
func realAfter(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// advanceMissed moves every schedule whose tick passed while the server was down to
// its next tick after now, creating no Run. It walks the schedules in keyset batches
// of advanceBatch. Each schedule moves only if its tick is still the one it was read
// with, so a registration that changed it in the meantime is left as the registration
// set it.
func (s *Service) advanceMissed(ctx context.Context) error {
	now := s.clock()
	var after domain.TaskRef
	moved := 0
	for {
		entries, err := s.schedules.ListMissed(ctx, now, after, advanceBatch)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			next, ok := domain.NextTick(entry.Schedule, now)
			if !ok {
				next = time.Time{}
			}

			advanced, err := s.schedules.Advance(ctx, entry.Ref, entry.NextRunAt, next)
			if err != nil {
				return fmt.Errorf("schedule of task %s: %w", entry.Ref.TaskID, err)
			}

			if advanced {
				moved++
			}
		}

		if len(entries) < advanceBatch {
			break
		}

		after = entries[len(entries)-1].Ref
	}

	s.logger.InfoContext(ctx, "missed schedule ticks advanced", slog.Int("schedules", moved))
	return nil
}

// announceReady tells the queue that a Run of the Task became ready at the given time.
// The Run is committed before this is called, so a failure is logged and not returned:
// polling finds the Run.
func (s *Service) announceReady(ctx context.Context, ref domain.TaskRef, at time.Time) {
	if err := s.queue.Ready(ctx, ref, at); err != nil && ctx.Err() == nil {
		s.logger.ErrorContext(
			ctx,
			"announce ready run",
			slog.String("worker_id", ref.WorkerID),
			slog.String("task_id", ref.TaskID),
			slog.Any("error", err),
		)
	}
}

// taskPolicy returns the retry policy the Task registered, or nil when it has none.
func (s *Service) taskPolicy(ctx context.Context, ref domain.TaskRef) (*domain.RetryPolicy, error) {
	task, err := s.tasks.GetTask(ctx, ref)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return task.Retry, nil
}
