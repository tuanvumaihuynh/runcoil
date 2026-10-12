package engine

import (
	"container/heap"
	"context"
	"log/slog"
	"time"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// schedulerConfig holds the poller's window, interval, and cap.
type schedulerConfig struct {
	Window       time.Duration
	PollInterval time.Duration
	MaxLoaded    int
}

// fireBackoffMax is the longest a schedule whose firing keeps failing waits before its next try.
const fireBackoffMax = time.Minute

// scheduler creates the Runs of schedule ticks. The schedules live in the store with
// their next ticks. The scheduler holds in memory only the ticks due within a window,
// in a heap ordered by tick, and at most MaxLoaded of them.
//
// Each poll reads the schedules due by now plus the window, earliest first, and
// replaces the heap with them. A poll runs every PollInterval, and at once when a
// registration signals a change, or when the loaded ticks were all handled and the cap
// was reached. A change made by another process is seen within one poll interval; a
// change made by this process is seen at once, through the signal.
//
// A tick is fired in three steps, with no transaction across them. The stored schedule
// is read and the tick is decided: a schedule that no longer has the tick is dropped.
// The Run of the tick is created; the insert does nothing when it exists already. The
// next tick is written with a compare-and-set on the tick that was fired. A crash
// between the create and the advance repeats the create, which does nothing.
//
// A schedule whose firing keeps failing is retried with a growing delay, up to one
// minute, so one bad schedule does not hold the poller. Nothing in the scheduler grows
// with the total number of schedules: the heap holds at most MaxLoaded entries.
type scheduler struct {
	cfg       schedulerConfig
	logger    *slog.Logger
	runs      RunStore
	queue     RunQueue
	schedules ScheduleStore
	instances *instances
	signals   Signals
	clock     func() time.Time
	loaded    timerHeap
	backoff   map[domain.TaskRef]fireBackoff
}

// fireBackoff is how many firings of a schedule failed in a row, and when it may fire again.
type fireBackoff struct {
	failures int
	until    time.Time
}

// newScheduler creates the scheduler from its configuration, its stores and signals, and the instance record.
func newScheduler(
	cfg schedulerConfig,
	logger *slog.Logger,
	runs RunStore,
	queue RunQueue,
	schedules ScheduleStore,
	signals Signals,
	inst *instances,
	clock func() time.Time,
) *scheduler {
	return &scheduler{
		cfg:       cfg,
		logger:    logger,
		runs:      runs,
		queue:     queue,
		schedules: schedules,
		instances: inst,
		signals:   signals,
		clock:     clock,
		backoff:   make(map[domain.TaskRef]fireBackoff),
	}
}

// run polls and fires until ctx ends. The missed ticks were advanced at start.
func (s *scheduler) run(ctx context.Context) {
	changed, unsubscribe := s.signals.Subscribe(TopicSchedules)
	defer unsubscribe()

	pollAt := s.clock()
	capHit := false
	for {
		now := s.clock()
		if !now.Before(pollAt) {
			capHit = s.poll(ctx, now)
			pollAt = now.Add(s.cfg.PollInterval)
		}

		fired := s.fireDue(ctx)
		if capHit && fired > 0 && s.loaded.Len() == 0 {
			// Every loaded tick is handled, and more schedules are waiting: poll again now.
			capHit = false
			pollAt = s.clock()
			continue
		}

		now = s.clock()
		wait := pollAt.Sub(now)
		if s.loaded.Len() > 0 {
			wait = min(wait, s.loaded[0].Tick.Sub(now))
		}

		timer := time.NewTimer(max(wait, 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-changed:
			pollAt = s.clock()
		}

		timer.Stop()
	}
}

// poll reads the schedules due within the window and replaces the loaded ticks with
// them. A schedule in backoff is left out until its delay has passed. It reports
// whether the cap was reached. On a failure the loaded ticks stay.
func (s *scheduler) poll(ctx context.Context, now time.Time) bool {
	timers, err := s.schedules.LoadDue(ctx, now.Add(s.cfg.Window), s.cfg.MaxLoaded)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.ErrorContext(ctx, "poll schedules", slog.Any("error", err))
		}

		return false
	}

	s.pruneBackoff(timers, now)

	loaded := make(timerHeap, 0, len(timers))
	for _, t := range timers {
		if b, ok := s.backoff[t.Ref]; ok && now.Before(b.until) {
			continue
		}

		loaded = append(loaded, t)
	}

	s.loaded = loaded
	heap.Init(&s.loaded)

	capHit := len(timers) >= s.cfg.MaxLoaded
	if capHit {
		s.logger.WarnContext(
			ctx,
			"schedule cap reached; the ticks past the cap wait for the next poll",
			slog.Int("max_loaded", s.cfg.MaxLoaded),
		)
	}

	s.logger.DebugContext(
		ctx,
		"poll schedules loaded",
		slog.Int("loaded", len(s.loaded)),
		slog.Bool("cap_reached", capHit),
	)

	return capHit
}

// pruneBackoff keeps a backoff entry only while its schedule is still in the window and
// its delay has not passed. An entry for a removed schedule, or one whose delay is over,
// is dropped, so the map never holds more than the window's failing schedules.
func (s *scheduler) pruneBackoff(inWindow []domain.ScheduleTimer, now time.Time) {
	present := make(map[domain.TaskRef]bool, len(inWindow))
	for _, t := range inWindow {
		present[t.Ref] = true
	}

	for ref, b := range s.backoff {
		if !present[ref] || !now.Before(b.until) {
			delete(s.backoff, ref)
		}
	}
}

// fireDue fires each loaded tick that is due, and returns how many it handled. The
// clock is read again for each firing, so a late firing gets its own time.
func (s *scheduler) fireDue(ctx context.Context) int {
	fired := 0
	for s.loaded.Len() > 0 {
		now := s.clock()
		if s.loaded[0].Tick.After(now) {
			break
		}

		t := heap.Pop(&s.loaded).(domain.ScheduleTimer)
		fired++

		next, err := s.fireTick(ctx, t, now)
		if err != nil {
			s.failFire(ctx, t, now, err)
			continue
		}

		delete(s.backoff, t.Ref)
		if next.IsZero() {
			continue
		}

		if !next.After(now.Add(s.cfg.Window)) {
			heap.Push(&s.loaded, domain.ScheduleTimer{Ref: t.Ref, Tick: next})
		}
	}

	return fired
}

// fireTick fires one loaded tick at now. It returns the schedule's next tick, or the
// zero time when the tick was dropped or the schedule has no further tick.
func (s *scheduler) fireTick(ctx context.Context, t domain.ScheduleTimer, now time.Time) (time.Time, error) {
	decision, err := s.decideTick(ctx, t, now)
	if err != nil {
		return time.Time{}, err
	}

	logger := s.logger.With(
		slog.String("worker_id", t.Ref.WorkerID),
		slog.String("task_id", t.Ref.TaskID),
	)

	switch decision.Action {
	case domain.TickDrop:
		logger.DebugContext(ctx, "schedule tick dropped")
		return time.Time{}, nil
	case domain.TickSkip:
		logger.DebugContext(ctx, "schedule tick skipped", slog.Time("tick", t.Tick))
	case domain.TickCreate:
		created, err := s.createTickRun(ctx, t, now)
		if err != nil {
			return time.Time{}, err
		}

		if created {
			s.announceReady(ctx, t.Ref, t.Tick)
		}
	}

	advanced, err := s.schedules.Advance(ctx, t.Ref, t.Tick, decision.Next)
	if err != nil {
		return time.Time{}, err
	}

	if !advanced {
		// The schedule changed while its tick fired. The registration's tick stands.
		logger.DebugContext(ctx, "schedule changed while its tick fired")
		return time.Time{}, nil
	}

	return decision.Next, nil
}

// decideTick reads the stored schedule of a loaded tick, and decides what the tick does.
func (s *scheduler) decideTick(ctx context.Context, t domain.ScheduleTimer, now time.Time) (domain.TickDecision, error) {
	entry, found, err := s.schedules.Get(ctx, t.Ref)
	if err != nil {
		return domain.TickDecision{}, err
	}

	var stored *domain.ScheduleEntry
	unfinished := false
	if found {
		stored = &entry
		if entry.Schedule.SkipOverlap {
			unfinished, err = s.runs.HasUnfinishedScheduled(ctx, t.Ref)
			if err != nil {
				return domain.TickDecision{}, err
			}
		}
	}

	return domain.DecideTick(stored, t.Tick, unfinished, now), nil
}

// createTickRun creates the Run of a due tick, and reports whether this call created it.
func (s *scheduler) createTickRun(ctx context.Context, t domain.ScheduleTimer, now time.Time) (bool, error) {
	reason := queuedReasonOf(s.instances, t.Ref)
	created, err := s.runs.CreateTickRun(ctx, domain.Run{
		Task:         t.Ref,
		Status:       domain.RunStatusQueued,
		QueuedReason: reason,
		Source:       domain.RunSourceSchedule,
		DueAt:        t.Tick,
		CreatedAt:    now,
	})
	if err != nil {
		return false, err
	}

	s.logger.DebugContext(
		ctx,
		"schedule tick fired",
		slog.String("worker_id", t.Ref.WorkerID),
		slog.String("task_id", t.Ref.TaskID),
		slog.Time("tick", t.Tick),
		slog.Bool("run_created", created),
	)

	return created, nil
}

// failFire logs a failed firing and delays the schedule's next try. The delay doubles
// with each failure in a row, up to fireBackoffMax.
func (s *scheduler) failFire(ctx context.Context, t domain.ScheduleTimer, now time.Time, err error) {
	if ctx.Err() != nil {
		return
	}

	b := s.backoff[t.Ref]
	b.failures++
	delay := min(time.Second<<min(b.failures-1, 6), fireBackoffMax)
	b.until = now.Add(delay)
	s.backoff[t.Ref] = b

	s.logger.ErrorContext(
		ctx,
		"fire schedule tick",
		slog.String("worker_id", t.Ref.WorkerID),
		slog.String("task_id", t.Ref.TaskID),
		slog.Int("failures", b.failures),
		slog.Any("error", err),
	)
}

// announceReady tells the queue that a tick's Run is ready, and wakes the dispatcher.
// A failure to announce does not undo the Run: polling finds it.
func (s *scheduler) announceReady(ctx context.Context, ref domain.TaskRef, at time.Time) {
	if err := s.queue.Ready(ctx, ref, at); err != nil && ctx.Err() == nil {
		s.logger.ErrorContext(ctx, "announce ready run", slog.Any("error", err))
	}

	s.signals.Notify(TopicRuns)
}
