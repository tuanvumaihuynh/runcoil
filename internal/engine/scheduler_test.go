package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

func at(sec int) time.Time {
	return time.Date(2026, 10, 11, 10, 0, sec, 0, time.UTC)
}

// everyTask is a Task with an every schedule.
func everyTask(name string, d time.Duration, skipOverlap bool) domain.Task {
	return domain.Task{
		Ref:      domain.TaskRef{WorkerID: "demo", TaskID: name},
		Schedule: &domain.Schedule{Kind: domain.ScheduleKindEvery, Every: d, SkipOverlap: skipOverlap},
	}
}

var testSchedulerConfig = schedulerConfig{Window: 30 * time.Second, PollInterval: 10 * time.Second, MaxLoaded: 100}

var testReportRef = domain.TaskRef{WorkerID: "demo", TaskID: "report"}

// register stores a worker ID's Tasks and plans its schedules, as a registration does.
func register(t *testing.T, store *fakeStore, workerID string, now time.Time, tasks ...domain.Task) {
	t.Helper()
	ctx := context.Background()
	err := store.UpsertTasks(ctx, tasks)
	require.NoError(t, err, "upsert")

	stored, err := store.List(ctx, workerID)
	require.NoError(t, err, "list")
	err = store.Replace(ctx, workerID, domain.PlanSchedules(stored, tasks, now))
	require.NoError(t, err, "replace")
}

// schedHarness is a scheduler on the fake stores, with a clock the test moves.
type schedHarness struct {
	store *fakeStore
	clk   *fakeClock
	s     *scheduler
}

func newHarness(t *testing.T, store *fakeStore, cfg schedulerConfig, now time.Time) *schedHarness {
	t.Helper()
	clk := newFakeClock(now)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &schedHarness{
		store: store,
		clk:   clk,
		s: newScheduler(
			cfg,
			logger,
			store,
			store,
			store,
			newFakeSignals(),
			newInstances(),
			clk.Now,
		),
	}
}

func newTestScheduler(t *testing.T, store *fakeStore, cfg schedulerConfig, now time.Time) *scheduler {
	t.Helper()
	return newHarness(t, store, cfg, now).s
}

func TestPollLoadsOnlyTheWindow(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1),
		everyTask("near", 2*time.Second, false),
		everyTask("far", 2*time.Hour, false))

	s := newTestScheduler(t, store, testSchedulerConfig, at(1))

	capHit := s.poll(context.Background(), at(1))
	require.False(t, capHit, "the cap was not reached")
	require.Equal(t, 1, s.loaded.Len(), "loaded = %+v, want only the schedule due within the window", s.loaded)
	require.Equal(t, "near", s.loaded[0].Ref.TaskID, "loaded = %+v, want only the schedule due within the window", s.loaded)
}

func TestPollRespectsTheCap(t *testing.T) {
	store := newFakeStore()
	var tasks []domain.Task
	for i := range 10 {
		tasks = append(tasks, everyTask(fmt.Sprintf("t%02d", i), 2*time.Second, false))
	}

	register(t, store, "demo", at(1), tasks...)
	cfg := testSchedulerConfig
	cfg.MaxLoaded = 3
	s := newTestScheduler(t, store, cfg, at(1))

	capHit := s.poll(context.Background(), at(1))
	require.True(t, capHit, "a full load must report the cap")
	require.Equal(t, 3, s.loaded.Len(), "loaded, want the cap of 3")
}

func TestFireCreatesRunAndRearms(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()

	h.s.poll(ctx, at(1))
	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	runs := store.scheduledRuns(testReportRef)
	require.Len(t, runs, 1, "want one schedule Run due at 10:00:02")
	require.True(t, runs[0].DueAt.Equal(at(2)), "due at = %v, want 10:00:02", runs[0].DueAt)
	require.Equal(t, domain.RunSourceSchedule, runs[0].Source, "want one schedule Run due at 10:00:02")
	require.Equal(t, 1, h.s.loaded.Len(), "after the firing the next tick must be loaded")
	require.True(t, h.s.loaded[0].Tick.Equal(at(4)), "after the firing the next tick must be loaded: %+v", h.s.loaded)

	h.clk.Set(at(4))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 2, "runs after the second tick")
}

// A loaded tick whose schedule changed meanwhile creates nothing, and is dropped.
func TestDropsEntryWhoseRowChanged(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	// Another registration changes the schedule. The loaded tick is now stale.
	register(t, store, "demo", at(1), everyTask("report", 5*time.Second, false))
	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	require.Empty(t, store.scheduledRuns(testReportRef), "runs, want none for a changed schedule")
	require.Equal(t, 0, h.s.loaded.Len(), "the stale entry must be dropped, loaded = %+v", h.s.loaded)
}

// A loaded tick whose schedule was removed meanwhile creates nothing, and is dropped.
func TestDropsEntryWhoseRowVanished(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	register(t, store, "demo", at(1), domain.Task{Ref: testReportRef})
	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	require.Empty(t, store.scheduledRuns(testReportRef), "runs, want none for a removed schedule")
	require.Equal(t, 0, h.s.loaded.Len(), "the vanished entry must be dropped, loaded = %+v", h.s.loaded)
}

func TestSkipOverlapSkipsWhileOneIsUnfinished(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, true))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 1, "after tick 1")

	// The first Run is still queued, which counts as unfinished: tick 2 creates nothing.
	h.clk.Set(at(4))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 1, "while the first Run is queued")

	first := store.scheduledRuns(testReportRef)[0]
	store.mu.Lock()
	first.Status = domain.RunStatusSucceeded
	store.runs[first.ID] = first
	store.mu.Unlock()

	h.clk.Set(at(6))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 2, "after the first Run finished")
}

// At start, a tick that passed while the server was down creates no Run: the schedule
// moves to its next tick after now.
func TestStartupAdvanceCreatesNoRun(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(0), everyTask("report", 2*time.Second, false))
	ctx := context.Background()
	e := newFakeService(t, store, testConfig(), at(11))

	err := e.advanceMissed(ctx)
	require.NoError(t, err, "advance")
	require.Empty(t, store.scheduledRuns(testReportRef), "runs, want none for the ticks that passed")

	store.mu.Lock()
	next := store.schedules[testReportRef].next
	store.mu.Unlock()
	require.True(t, next.Equal(at(12)), "next tick = %v, want 10:00:12", next)

	h := newHarness(t, store, testSchedulerConfig, at(11))
	h.s.poll(ctx, at(11))
	h.s.fireDue(ctx)
	require.Empty(t, store.scheduledRuns(testReportRef), "runs after the first poll, want none")
}

// A failing advance fails the start: the first poll must not fire the missed ticks silently.
func TestAdvanceFailureFailsTheStart(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(0), everyTask("report", 2*time.Second, false))
	failing := &failingSchedules{fakeStore: store, failListMissed: true}

	e := newService(
		testConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		store,
		store,
		failing,
		newFakeSignals(),
		func() time.Time { return at(11) },
		func() float64 { return 0.5 },
	)

	err := e.Run(context.Background())
	require.Error(t, err, "run must fail when the missed ticks cannot be advanced")
}

// Registering an unchanged schedule keeps its pending tick, so a due tick is not dropped.
func TestUnchangedScheduleKeepsItsTick(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	nextOf := func() time.Time {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.schedules[testReportRef].next
	}

	pending := nextOf()

	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	kept := nextOf()
	require.True(t, kept.Equal(pending), "an unchanged schedule's tick moved from %v to %v", pending, kept)

	// A changed schedule starts from now.
	register(t, store, "demo", at(1), everyTask("report", 5*time.Second, false))
	changed := nextOf()
	require.True(t, changed.Equal(at(5)), "a changed schedule's tick = %v, want 10:00:05", changed)
}

// A signal makes the scheduler poll at once, even though its own interval has not passed.
func TestSignalMakesTheSchedulerPollNow(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	signals := newFakeSignals()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := newScheduler(
		testSchedulerConfig,
		logger,
		store,
		store,
		store,
		signals,
		newInstances(),
		func() time.Time { return at(1) },
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx)
	}()

	waitFor := func(n int) {
		t.Helper()
		require.Eventually(t, func() bool { return store.loadCount() >= n }, 2*time.Second, 5*time.Millisecond,
			"loads, want %d", n)
	}

	waitFor(1)
	// The poll interval is ten seconds. The next poll must come from the signal.
	signals.Notify(TopicSchedules)
	waitFor(2)
	cancel()
	<-done
}

func TestPollFailureKeepsLoadedTicks(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	s := newTestScheduler(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	s.poll(ctx, at(1))
	require.Equal(t, 1, s.loaded.Len(), "loaded")

	// A failing store keeps what was loaded; the next poll replaces it.
	s.schedules = &failingSchedules{fakeStore: store, failLoadDue: true}
	s.poll(ctx, at(1))
	require.Equal(t, 1, s.loaded.Len(), "a failed poll must keep the loaded ticks")
}

// The compare-and-set loses when the schedule changes between the read and the advance.
// The registration's tick stands; the Run created for the fired tick is the one that was decided.
func TestCompareAndSetLosingKeepsTheRegistrationTick(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	store.advanceHook = func(ref domain.TaskRef) {
		// Between the read and the advance, another registration moves the schedule.
		register(t, store, "demo", at(1), everyTask("report", 5*time.Second, false))
		store.advanceHook = nil
	}

	h.clk.Set(at(2))
	h.s.fireDue(ctx)

	store.mu.Lock()
	next := store.schedules[testReportRef].next
	store.mu.Unlock()
	require.True(t, next.Equal(at(5)), "next tick = %v, want the registration's 10:00:05", next)
	require.Len(t, store.scheduledRuns(testReportRef), 1, "runs, want the one decided before the change")
}

// A crash between the create and the advance is repeated as a no-op: no second Run.
func TestRepeatedFiringAfterAFailedAdvanceCreatesNoSecondRun(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	failing := &failingSchedules{fakeStore: store, failAdvanceOnce: true}
	h := newHarness(t, store, testSchedulerConfig, at(1))
	h.s.schedules = failing
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 1, "after the failed advance: the one created")

	store.mu.Lock()
	stillAt2 := store.schedules[testReportRef].next.Equal(at(2))
	store.mu.Unlock()
	require.True(t, stillAt2, "a failed advance must leave the tick in place, so it is fired again")

	// The backoff delays the next try; after it, the same tick is fired again.
	h.clk.Set(at(3))
	h.s.poll(ctx, at(3))
	h.s.fireDue(ctx)
	require.Len(t, store.scheduledRuns(testReportRef), 1, "the repeated firing must not create a second Run")

	store.mu.Lock()
	next := store.schedules[testReportRef].next
	store.mu.Unlock()
	require.True(t, next.Equal(at(4)), "after the repeated firing the next tick = %v, want 10:00:04", next)
}

// A schedule whose firing keeps failing is left out of the poll until its delay has passed.
func TestFailingScheduleIsBackedOff(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	failing := &failingSchedules{fakeStore: store, failGet: true}
	h := newHarness(t, store, testSchedulerConfig, at(1))
	h.s.schedules = failing
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	h.clk.Set(at(2))
	h.s.fireDue(ctx)
	b, ok := h.s.backoff[testReportRef]
	require.True(t, ok, "backoff = %+v, want one failure recorded", b)
	require.Equal(t, 1, b.failures, "backoff = %+v, want one failure recorded", b)

	// Within the delay the schedule is not loaded, even though its tick is due.
	h.s.poll(ctx, at(2))
	require.Equal(t, 0, h.s.loaded.Len(), "a schedule in backoff must not be loaded")

	// After the delay it is loaded again.
	failing.failGet = false
	h.clk.Set(at(4))
	h.s.poll(ctx, at(4))
	require.Equal(t, 1, h.s.loaded.Len(), "after the delay the schedule is loaded again")
}

// Each firing reads the clock again, so a later firing in the same pass gets its own time.
func TestEachFiringReadsTheClock(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1),
		everyTask("first", 2*time.Second, false),
		everyTask("second", 2*time.Second, false))

	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.poll(ctx, at(1))

	fired := 0
	store.advanceHook = func(domain.TaskRef) {
		fired++
		if fired == 1 {
			h.clk.Set(at(5)) // the first firing takes time; the second starts later
		}
	}

	h.clk.Set(at(4))
	h.s.fireDue(ctx)

	var created []time.Time
	for _, ref := range []domain.TaskRef{{WorkerID: "demo", TaskID: "first"}, {WorkerID: "demo", TaskID: "second"}} {
		runs := store.scheduledRuns(ref)
		require.Len(t, runs, 1, "%s: runs", ref.TaskID)

		created = append(created, runs[0].CreatedAt)
	}

	require.True(t, created[0].Equal(at(4)), "created at %v, want the clock read per firing: 10:00:04 then 10:00:05", created)
	require.True(t, created[1].Equal(at(5)), "created at %v, want the clock read per firing: 10:00:04 then 10:00:05", created)
}

// failingSchedules wraps the fake store and fails the calls a test names, once or always.
type failingSchedules struct {
	*fakeStore
	failListMissed  bool
	failLoadDue     bool
	failGet         bool
	failAdvanceOnce bool
}

var errInjected = errors.New("database unavailable")

func (f *failingSchedules) ListMissed(ctx context.Context, now time.Time, after domain.TaskRef, limit int) ([]domain.ScheduleEntry, error) {
	if f.failListMissed {
		return nil, errInjected
	}

	return f.fakeStore.ListMissed(ctx, now, after, limit)
}

func (f *failingSchedules) LoadDue(ctx context.Context, until time.Time, limit int) ([]domain.ScheduleTimer, error) {
	if f.failLoadDue {
		return nil, errInjected
	}

	return f.fakeStore.LoadDue(ctx, until, limit)
}

func (f *failingSchedules) Get(ctx context.Context, ref domain.TaskRef) (domain.ScheduleEntry, bool, error) {
	if f.failGet {
		return domain.ScheduleEntry{}, false, errInjected
	}

	return f.fakeStore.Get(ctx, ref)
}

func (f *failingSchedules) Advance(ctx context.Context, ref domain.TaskRef, tick, next time.Time) (bool, error) {
	if f.failAdvanceOnce {
		f.failAdvanceOnce = false
		return false, errInjected
	}

	return f.fakeStore.Advance(ctx, ref, tick, next)
}

// A schedule removed from the registration leaves no backoff entry behind.
func TestBackoffEntriesOfRemovedSchedulesArePruned(t *testing.T) {
	store := newFakeStore()
	register(t, store, "demo", at(1), everyTask("report", 2*time.Second, false))
	h := newHarness(t, store, testSchedulerConfig, at(1))
	ctx := context.Background()
	h.s.backoff[testReportRef] = fireBackoff{failures: 3, until: at(30)}
	h.s.poll(ctx, at(1))
	_, ok := h.s.backoff[testReportRef]
	require.True(t, ok, "a failing schedule still in the window keeps its entry")

	register(t, store, "demo", at(1), domain.Task{Ref: testReportRef})
	h.s.poll(ctx, at(1))
	_, ok = h.s.backoff[testReportRef]
	require.False(t, ok, "an entry for a removed schedule must be dropped on the next poll")
}
