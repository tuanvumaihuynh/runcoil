package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// fakeStore keeps Tasks, schedules, Runs, Steps, and idempotency keys in memory. It
// implements the engine's store interfaces, so the engine tests run without storage. Each
// method does what one store call does: a decision passed to Mutate runs on a copy under
// the lock, and the result is stored.
type fakeStore struct {
	mu        sync.Mutex
	tasks     map[domain.TaskRef]domain.Task
	schedules map[domain.TaskRef]*fakeSchedule
	runs      map[string]domain.Run
	steps     map[string][]domain.StepRecord
	keys      map[string]fakeKey
	loads     int
	// renewCalls counts the calls to RenewLeases, so a test can see that a call never reached the store.
	renewCalls int
	readies    []domain.TaskRef
	// advanceHook runs before each Advance, so a test can change a schedule in between.
	advanceHook func(ref domain.TaskRef)
}

// fakeSchedule is a schedule row: its spec and its next tick. A zero next tick means none.
type fakeSchedule struct {
	sched domain.Schedule
	next  time.Time
}

// fakeKey is an idempotency key row.
type fakeKey struct {
	runID       string
	fingerprint string
	expiresAt   time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tasks:     make(map[domain.TaskRef]domain.Task),
		schedules: make(map[domain.TaskRef]*fakeSchedule),
		runs:      make(map[string]domain.Run),
		steps:     make(map[string][]domain.StepRecord),
		keys:      make(map[string]fakeKey),
	}
}

func (f *fakeStore) UpsertTasks(_ context.Context, tasks []domain.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range tasks {
		stored := task
		stored.Schedule = nil
		f.tasks[task.Ref] = stored
	}

	return nil
}

func (f *fakeStore) GetTask(_ context.Context, ref domain.TaskRef) (domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[ref]
	if !ok {
		return domain.Task{}, domain.ErrNotFound
	}

	return task, nil
}

func (f *fakeStore) ListTasks(_ context.Context, workerID string) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Task
	for ref, task := range f.tasks {
		if ref.WorkerID == workerID {
			out = append(out, task)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Ref.TaskID < out[j].Ref.TaskID })
	return out, nil
}

// LoadDue returns up to limit schedules whose next tick is at or before until, earliest first.
func (f *fakeStore) LoadDue(_ context.Context, until time.Time, limit int) ([]domain.ScheduleTimer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	var out []domain.ScheduleTimer
	for ref, sc := range f.schedules {
		if !sc.next.IsZero() && !sc.next.After(until) {
			out = append(out, domain.ScheduleTimer{Ref: ref, Tick: sc.next})
		}
	}

	// Ties in the tick are broken by Task, so the order does not depend on map iteration.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Tick.Equal(out[j].Tick) {
			return out[i].Tick.Before(out[j].Tick)
		}

		return keyLess(out[i].Ref, out[j].Ref)
	})

	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func (f *fakeStore) Get(_ context.Context, ref domain.TaskRef) (domain.ScheduleEntry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc, ok := f.schedules[ref]
	if !ok {
		return domain.ScheduleEntry{}, false, nil
	}

	return entryOf(ref, sc), true, nil
}

// Advance is the compare-and-set of the store: it writes only while the tick is the stored one.
func (f *fakeStore) Advance(_ context.Context, ref domain.TaskRef, tick, next time.Time) (bool, error) {
	if f.advanceHook != nil {
		f.advanceHook(ref)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	sc, ok := f.schedules[ref]
	if !ok || !sc.next.Equal(tick) {
		return false, nil
	}

	sc.next = next
	return true, nil
}

// ListMissed returns the schedules whose tick is before now, after the given Task, in key order.
func (f *fakeStore) ListMissed(_ context.Context, now time.Time, after domain.TaskRef, limit int) ([]domain.ScheduleEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ScheduleEntry
	for ref, sc := range f.schedules {
		if sc.next.IsZero() || !sc.next.Before(now) {
			continue
		}
		if !keyAfter(ref, after) {
			continue
		}

		out = append(out, entryOf(ref, sc))
	}

	sort.Slice(out, func(i, j int) bool { return keyLess(out[i].Ref, out[j].Ref) })
	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func (f *fakeStore) List(_ context.Context, workerID string) ([]domain.ScheduleEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ScheduleEntry
	for ref, sc := range f.schedules {
		if ref.WorkerID == workerID {
			out = append(out, entryOf(ref, sc))
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Ref.TaskID < out[j].Ref.TaskID })
	return out, nil
}

// Replace mirrors the store: a schedule whose rule is unchanged keeps its next tick, and
// the other schedules of the worker ID are removed.
func (f *fakeStore) Replace(_ context.Context, workerID string, entries []domain.ScheduleEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	named := make(map[string]bool)
	for _, entry := range entries {
		if cur, ok := f.schedules[entry.Ref]; ok && domain.SameSchedule(cur.sched, entry.Schedule) {
			cur.sched = entry.Schedule
			continue
		}

		f.schedules[entry.Ref] = &fakeSchedule{sched: entry.Schedule, next: entry.NextRunAt}
		named[entry.Ref.TaskID] = true
	}

	for _, entry := range entries {
		named[entry.Ref.TaskID] = true
	}

	for ref := range f.schedules {
		if ref.WorkerID == workerID && !named[ref.TaskID] {
			delete(f.schedules, ref)
		}
	}

	return nil
}

func entryOf(ref domain.TaskRef, sc *fakeSchedule) domain.ScheduleEntry {
	return domain.ScheduleEntry{Ref: ref, Schedule: sc.sched, NextRunAt: sc.next}
}

func keyLess(a, b domain.TaskRef) bool {
	if a.WorkerID != b.WorkerID {
		return a.WorkerID < b.WorkerID
	}

	return a.TaskID < b.TaskID
}

func keyAfter(ref, after domain.TaskRef) bool {
	return after == (domain.TaskRef{}) || keyLess(after, ref)
}

// hasUnfinishedLocked reports an unfinished schedule Run of the Task.
func (f *fakeStore) hasUnfinishedLocked(ref domain.TaskRef) bool {
	for _, run := range f.runs {
		if run.Source == domain.RunSourceSchedule && run.Task == ref &&
			(run.Status == domain.RunStatusQueued || run.Status == domain.RunStatusRunning) {
			return true
		}
	}

	return false
}

func (f *fakeStore) HasUnfinishedScheduled(_ context.Context, ref domain.TaskRef) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasUnfinishedLocked(ref), nil
}

// CreateTickRun inserts the Run of a tick, unless a schedule Run of the Task has that due time.
func (f *fakeStore) CreateTickRun(_ context.Context, run domain.Run) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.runs {
		if existing.Source == domain.RunSourceSchedule && existing.Task == run.Task && existing.DueAt.Equal(run.DueAt) {
			return false, nil
		}
	}

	run.ID = uuid.NewString()
	f.runs[run.ID] = run
	return true, nil
}

// scheduledRuns returns the schedule Runs of the Task, by due time.
func (f *fakeStore) scheduledRuns(ref domain.TaskRef) []domain.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Run
	for _, run := range f.runs {
		if run.Source == domain.RunSourceSchedule && run.Task == ref {
			out = append(out, run)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].DueAt.Before(out[j].DueAt) })
	return out
}

func (f *fakeStore) loadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads
}

// CreateRun mirrors the store's transaction: an expired key is removed, an unexpired
// key is returned as the holder, and otherwise the Run and its copied Steps are stored.
func (f *fakeStore) CreateRun(
	_ context.Context,
	run domain.Run,
	key domain.KeyRecord,
	copied []domain.StepRecord,
) (domain.KeyHolder, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k, ok := f.keys[key.Key]; ok && !k.expiresAt.After(key.CreatedAt) {
		delete(f.keys, key.Key)
	}
	if k, ok := f.keys[key.Key]; ok {
		return domain.KeyHolder{RunID: k.runID, Fingerprint: k.fingerprint}, false, nil
	}

	f.keys[key.Key] = fakeKey{runID: key.RunID, fingerprint: key.Fingerprint, expiresAt: key.ExpiresAt}
	run.ID = key.RunID
	f.runs[run.ID] = run
	for i, step := range copied {
		step.Position = i + 1
		f.steps[run.ID] = append(f.steps[run.ID], step)
	}

	return domain.KeyHolder{}, true, nil
}

// RunByKey returns the Run of an unexpired key.
func (f *fakeStore) RunByKey(_ context.Context, key string, now time.Time) (domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.keys[key]
	if !ok || !k.expiresAt.After(now) {
		return domain.Run{}, domain.ErrNotFound
	}

	run, ok := f.runs[k.runID]
	if !ok {
		return domain.Run{}, domain.ErrNotFound
	}

	return run, nil
}

// ListRuns returns the Runs that match the filter, newest first, after the position.
func (f *fakeStore) ListRuns(_ context.Context, filter domain.RunFilter) ([]domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []domain.Run
	for _, run := range f.runs {
		if filter.WorkerID != "" && run.Task.WorkerID != filter.WorkerID {
			continue
		}
		if filter.TaskID != "" && run.Task.TaskID != filter.TaskID {
			continue
		}
		if len(filter.Statuses) > 0 && !containsStatus(filter.Statuses, run.Status) {
			continue
		}
		if !filter.CreatedAfter.IsZero() && run.CreatedAt.Before(filter.CreatedAfter) {
			continue
		}
		if !filter.CreatedBefore.IsZero() && !run.CreatedAt.Before(filter.CreatedBefore) {
			continue
		}

		all = append(all, run)
	}

	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}

		return strings.Compare(all[i].ID, all[j].ID) > 0
	})

	var out []domain.Run
	for _, run := range all {
		if (!filter.AfterCreatedAt.IsZero() || filter.AfterID != "") &&
			!beforeCursor(run, filter.AfterCreatedAt, filter.AfterID) {
			continue
		}

		out = append(out, run)
		if len(out) == filter.Limit {
			break
		}
	}

	return out, nil
}

// beforeCursor reports whether a Run comes after the position in the newest-first order.
func beforeCursor(run domain.Run, createdAt time.Time, id string) bool {
	if !run.CreatedAt.Equal(createdAt) {
		return run.CreatedAt.Before(createdAt)
	}

	return strings.Compare(run.ID, id) < 0
}

func containsStatus(list []domain.RunStatus, s domain.RunStatus) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}

	return false
}

func (f *fakeStore) GetRun(_ context.Context, id string) (domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return domain.Run{}, domain.ErrNotFound
	}

	return run, nil
}

// RunStatus returns the status of a Run, or ErrNotFound.
func (f *fakeStore) RunStatus(_ context.Context, id string) (domain.RunStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return "", domain.ErrNotFound
	}

	return run.Status, nil
}

func (f *fakeStore) ListSteps(_ context.Context, runID string) ([]domain.StepRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.StepRecord(nil), f.steps[runID]...), nil
}

// readyAt is when a queued Run may start: its next attempt, or else its due time.
func readyAt(run domain.Run) time.Time {
	if !run.NextAttemptAt.IsZero() {
		return run.NextAttemptAt
	}

	return run.DueAt
}

func (f *fakeStore) ClaimNext(_ context.Context, ref domain.TaskRef, claim domain.Claim) (domain.Run, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, run := range f.runs {
		if run.Status != domain.RunStatusQueued || run.Task != ref || readyAt(run).After(claim.Now) {
			continue
		}

		run.Status = domain.RunStatusRunning
		run.QueuedReason = domain.QueuedReasonNone
		run.Attempts++
		run.LeaseToken = claim.LeaseToken
		run.LeaseExpiresAt = claim.LeaseExpiresAt
		run.InstanceID = claim.InstanceID
		run.AckedAt = time.Time{}
		run.RunningStepID = ""
		run.RunningStepAttempt = 0
		run.RunningStepPolicy = nil
		f.runs[id] = run
		return run, true, nil
	}

	return domain.Run{}, false, nil
}

// GiveBack mirrors the store: it applies the decision under the lock, on the Run alone.
func (f *fakeStore) GiveBack(_ context.Context, id string, token string, reason domain.QueuedReason, now time.Time) error {
	return f.MutateRun(context.Background(), id, func(s domain.Snapshot) (domain.Write, error) {
		return domain.GiveBack(s, token, reason, now), nil
	})
}

// Ready records the Runs that became ready, so a test can see them.
func (f *fakeStore) Ready(_ context.Context, ref domain.TaskRef, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readies = append(f.readies, ref)
	return nil
}

func (f *fakeStore) Mutate(_ context.Context, id string, fn func(domain.Snapshot) (domain.Write, error)) error {
	return f.mutate(id, true, fn)
}

func (f *fakeStore) MutateRun(_ context.Context, id string, fn func(domain.Snapshot) (domain.Write, error)) error {
	return f.mutate(id, false, fn)
}

func (f *fakeStore) mutate(id string, withSteps bool, fn func(domain.Snapshot) (domain.Write, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return domain.ErrNotFound
	}

	snap := domain.Snapshot{Run: run}
	if withSteps {
		snap.Steps = append([]domain.StepRecord(nil), f.steps[id]...)
	}

	write, err := fn(snap)
	if err != nil {
		return err
	}

	f.runs[id] = write.Run
	if write.Step != nil {
		replaced := false
		for i, step := range f.steps[id] {
			if step.StepID == write.Step.StepID {
				f.steps[id][i] = *write.Step
				replaced = true
			}
		}

		if !replaced {
			f.steps[id] = append(f.steps[id], *write.Step)
		}
	}

	return nil
}

// ListQueuedTasks returns the Tasks that have a queued Run.
func (f *fakeStore) ListQueuedTasks(_ context.Context) ([]domain.TaskRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := make(map[domain.TaskRef]bool)
	var out []domain.TaskRef
	for _, run := range f.runs {
		if run.Status == domain.RunStatusQueued && !seen[run.Task] {
			seen[run.Task] = true
			out = append(out, run.Task)
		}
	}

	return out, nil
}

// SetQueuedReason records nothing: the fake keeps the reason each Run was given.
func (f *fakeStore) SetQueuedReason(_ context.Context, _ domain.TaskRef, _ domain.QueuedReason, _ time.Time) error {
	return nil
}

func (f *fakeStore) CountRunning(_ context.Context, instanceID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, run := range f.runs {
		if run.Status == domain.RunStatusRunning && run.InstanceID == instanceID {
			n++
		}
	}

	return n, nil
}

// renewCount returns how many RenewLeases calls the store has seen.
func (f *fakeStore) renewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewCalls
}

// RenewLeases renews the named attempts whose lease is current. It records the Step each
// one runs, with the Step's policy.
func (f *fakeStore) RenewLeases(
	_ context.Context,
	instanceID string,
	attempts []domain.AttemptRef,
	expiresAt time.Time,
) ([]domain.AttemptRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewCalls++
	var stale []domain.AttemptRef
	for _, attempt := range attempts {
		run, ok := f.runs[attempt.RunID]
		if !ok || run.Status != domain.RunStatusRunning || run.LeaseToken != attempt.Token || run.InstanceID != instanceID {
			stale = append(stale, attempt)
			continue
		}

		run.LeaseExpiresAt = expiresAt
		run.RunningStepID = attempt.StepID
		run.RunningStepAttempt = 0
		run.RunningStepPolicy = nil
		if attempt.StepID != "" {
			run.RunningStepAttempt = attempt.StepAttempt
			run.RunningStepPolicy = attempt.Retry
		}

		f.runs[attempt.RunID] = run
	}

	return stale, nil
}

// ListExpiredLeases returns up to limit running Runs whose lease expired at or before now.
func (f *fakeStore) ListExpiredLeases(_ context.Context, now time.Time, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type item struct {
		id    string
		lease time.Time
	}

	var items []item
	for id, run := range f.runs {
		if run.Status == domain.RunStatusRunning && !run.LeaseExpiresAt.After(now) {
			items = append(items, item{id, run.LeaseExpiresAt})
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].lease.Before(items[j].lease) })
	var out []string
	for _, it := range items {
		if len(out) == limit {
			break
		}

		out = append(out, it.id)
	}

	return out, nil
}

// ExpireNeverStarted mirrors the statement: up to limit queued Runs that never started,
// whose due time plus their Task's start deadline has passed.
func (f *fakeStore) ExpireNeverStarted(_ context.Context, now time.Time, limit int) ([]domain.RunRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var refs []domain.RunRef
	for id, run := range f.runs {
		if len(refs) == limit {
			break
		}

		task, ok := f.tasks[run.Task]
		if !ok || task.StartDeadline <= 0 || run.Status != domain.RunStatusQueued || !run.StartedAt.IsZero() {
			continue
		}
		if now.Before(run.DueAt.Add(task.StartDeadline)) {
			continue
		}

		run.Status = domain.RunStatusFailed
		run.QueuedReason = domain.QueuedReasonNone
		run.NextAttemptAt = time.Time{}
		run.WorkerWaitSince = time.Time{}
		run.FinishedAt = now
		run.Failure = &domain.Failure{Code: domain.FailureCodeStartDeadline, Message: domain.StartDeadlineMessage,
			Attempts: run.Attempts}

		f.runs[id] = run
		refs = append(refs, domain.RunRef{ID: id, Task: run.Task})
	}

	return refs, nil
}

// ExpireWorkerWaits mirrors the statement: up to limit queued Runs that waited for a worker
// since before now minus the wait limit.
func (f *fakeStore) ExpireWorkerWaits(_ context.Context, now time.Time, wait time.Duration, limit int) ([]domain.RunRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var refs []domain.RunRef
	for id, run := range f.runs {
		if len(refs) == limit {
			break
		}
		if run.Status != domain.RunStatusQueued || run.QueuedReason != domain.QueuedReasonWaitingForWorker ||
			run.WorkerWaitSince.IsZero() || !run.WorkerWaitSince.Before(now.Add(-wait)) {
			continue
		}

		run.Status = domain.RunStatusFailed
		run.QueuedReason = domain.QueuedReasonNone
		run.WorkerWaitSince = time.Time{}
		run.FinishedAt = now
		run.Failure = &domain.Failure{Code: domain.FailureCodeWorkerWaitLimit, Message: domain.WorkerWaitLimitMessage,
			Attempts: run.Attempts}

		f.runs[id] = run
		refs = append(refs, domain.RunRef{ID: id, Task: run.Task})
	}

	return refs, nil
}

// seedRunning puts a running attempt into the store, as a dispatch would.
func (f *fakeStore) seedRunning(t *testing.T, ref domain.TaskRef, token string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	runID := uuid.NewString()
	f.runs[runID] = domain.Run{
		ID:             runID,
		Task:           ref,
		Status:         domain.RunStatusRunning,
		Source:         domain.RunSourceInvoke,
		Attempts:       1,
		LeaseToken:     token,
		LeaseExpiresAt: testNow.Add(time.Minute),
		InstanceID:     "i-1",
		DueAt:          testNow,
		CreatedAt:      testNow,
		StartedAt:      testNow,
	}

	return runID
}

// seedQueued puts a queued Run that was never dispatched, due at the given time.
func (f *fakeStore) seedQueued(t *testing.T, ref domain.TaskRef, due time.Time) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	runID := uuid.NewString()
	f.runs[runID] = domain.Run{
		ID:           runID,
		Task:         ref,
		Status:       domain.RunStatusQueued,
		QueuedReason: domain.QueuedReasonWaitingForWorker,
		Source:       domain.RunSourceInvoke,
		DueAt:        due,
		CreatedAt:    due,
	}

	return runID
}

// seedWaiting puts a queued Run waiting for a worker since the given time.
func (f *fakeStore) seedWaiting(t *testing.T, ref domain.TaskRef, since time.Time) string {
	t.Helper()
	id := f.seedQueued(t, ref, since)
	f.mu.Lock()
	defer f.mu.Unlock()
	run := f.runs[id]
	run.WorkerWaitSince = since
	f.runs[id] = run
	return id
}

// seedLeased puts a running attempt whose lease expires at expires into the store.
func seedLeased(t *testing.T, store *fakeStore, ref domain.TaskRef, token string, acked bool, expires time.Time) string {
	t.Helper()
	id := store.seedRunning(t, ref, token)
	store.mu.Lock()
	defer store.mu.Unlock()
	run := store.runs[id]
	run.LeaseExpiresAt = expires
	if acked {
		run.AckedAt = testNow
	}

	store.runs[id] = run
	return id
}

var testNow = time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)

var testRef = domain.TaskRef{WorkerID: "demo", TaskID: "hello"}

// testConfig is the configuration the engine tests use, unless a test changes it.
func testConfig() Config {
	return Config{
		Port:                   7070,
		Token:                  "tok",
		HeartbeatInterval:      10 * time.Second,
		LeaseDuration:          30 * time.Second,
		SendTimeout:            5 * time.Second,
		WorkerWaitLimit:        time.Hour,
		InstanceEvictionLeases: 3,
		Retry: RetryConfig{
			MaxAttempts: 3,
			Delays:      []time.Duration{time.Second, 2 * time.Second},
			Jitter:      0,
		},
		Scheduler:   SchedulerConfig{Window: 30 * time.Second, PollInterval: 10 * time.Second, MaxLoaded: 100},
		Dispatcher:  DispatcherConfig{PollInterval: time.Second},
		Sweep:       SweepConfig{Interval: time.Second},
		Idempotency: IdempotencyConfig{Window: 24 * time.Hour},
		WaitRun:     WaitRunConfig{MaxTimeout: time.Minute, PollInterval: 500 * time.Millisecond, MaxWaiters: 1000},
	}
}

// newTestService builds an engine on the fake store, with a fixed clock and no jitter.
func newTestService(t *testing.T, store *fakeStore) *Service {
	t.Helper()
	return newFakeService(t, store, testConfig(), testNow)
}

// newFakeService builds an engine on the fake stores with a fixed clock.
func newFakeService(t *testing.T, store *fakeStore, cfg Config, now time.Time) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newService(
		cfg,
		logger,
		store,
		store,
		store,
		store,
		newFakeSignals(),
		func() time.Time { return now },
		func() float64 { return 0.5 },
	)
}

// registerTestTask stores a Task with a retry policy, without a schedule. It runs on the
// test goroutine, so a failure stops the test.
func registerTestTask(t *testing.T, store *fakeStore, policy *domain.RetryPolicy) {
	t.Helper()
	task := domain.Task{Ref: testRef, Retry: policy}
	err := store.UpsertTasks(context.Background(), []domain.Task{task})
	require.NoError(t, err, "upsert")
}

// fakeSignals is the signal hub for tests: each subscriber holds at most one wake-up.
type fakeSignals struct {
	mu   sync.Mutex
	next int
	subs map[string]map[int]chan struct{}
}

func newFakeSignals() *fakeSignals {
	return &fakeSignals{subs: make(map[string]map[int]chan struct{})}
}

func (s *fakeSignals) Notify(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs[topic] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *fakeSignals) Subscribe(topic string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[topic] == nil {
		s.subs[topic] = make(map[int]chan struct{})
	}

	id := s.next
	s.next++
	ch := make(chan struct{}, 1)
	s.subs[topic][id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs[topic], id)
	}
}

// fakeClock is a clock that a test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock { return &fakeClock{now: now} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// fakeAfter is a timer for WaitRun that a test fires by hand. Each call creates a
// channel that Fire closes; the stop function is a no-op.
type fakeAfter struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

type fakeTimer struct {
	d  time.Duration
	ch chan time.Time
}

func (a *fakeAfter) After(d time.Duration) (<-chan time.Time, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := &fakeTimer{d: d, ch: make(chan time.Time, 1)}
	a.timers = append(a.timers, t)
	return t.ch, func() {}
}

// FireFirst fires the oldest timer of at least the given duration that has not fired.
// It runs on the test goroutine, so a missing timer stops the test.
func (a *fakeAfter) FireFirst(t *testing.T, minD time.Duration) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, timer := range a.timers {
		if timer.d >= minD {
			timer.ch <- testNow
			a.timers = append(a.timers[:i], a.timers[i+1:]...)
			return
		}
	}

	require.Fail(t, fmt.Sprintf("no pending timer of at least %v", minD))
}

// newServiceWith builds a test engine whose configuration the test may change first.
func newServiceWith(t *testing.T, store *fakeStore, change func(*Config)) *Service {
	t.Helper()
	cfg := testConfig()
	cfg.Limits = LimitsConfig{InputBytes: 8, StepOutputBytes: 8, RunOutputBytes: 8, RunTotalBytes: 16}
	if change != nil {
		change(&cfg)
	}

	return newFakeService(t, store, cfg, testNow)
}

// registerInstance registers an instance, serving testRef.
func registerInstance(t *testing.T, e *Service, id string, now time.Time) {
	t.Helper()
	e.instances.Register(domain.Registration{WorkerID: "demo", InstanceID: id,
		Slots: 1, Tasks: []domain.Task{{Ref: testRef}}}, now)
}

// attachInstance gives the instance a stream, as a Listen call does.
func attachInstance(t *testing.T, e *Service, id string, now time.Time) *stream {
	t.Helper()
	sess, ok := e.instances.Attach(id, now)
	require.True(t, ok, "attach instance: the instance is not registered")

	return sess
}

func engineCode(err error) apierr.Code {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return ""
}
