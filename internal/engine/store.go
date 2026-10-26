package engine

import (
	"context"
	"time"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// TaskStore is what the engine needs from storage for Tasks. A Task is stored as it
// was registered; its schedule is kept by the ScheduleStore.
type TaskStore interface {
	UpsertTasks(ctx context.Context, tasks []domain.Task) error
	GetTask(ctx context.Context, workerID, taskID string) (domain.Task, error)
	ListTasks(ctx context.Context, workerID string) ([]domain.Task, error)
}

// RunStore is what the engine needs from storage for Runs. The mutating methods run
// their decision under the Run's lock, in one transaction. Mutate reads the Steps
// too, and MutateRun does not, so a decision that needs only the Run does not read
// the Steps' outputs.
//
// CreateRun stores a new Run under its idempotency key, and the Steps it copies, in
// one transaction. Each Run is written in full by the decisions that change it. A Run
// is identified by the ID the engine gives it.
type RunStore interface {
	CreateRun(ctx context.Context, run domain.Run, key domain.KeyRecord, copied []domain.StepRecord) (domain.KeyHolder, bool, error)
	CreateTickRun(ctx context.Context, run domain.Run) (bool, error)
	GetRun(ctx context.Context, id string) (domain.Run, error)
	RunStatus(ctx context.Context, id string) (domain.RunStatus, error)
	RunByKey(ctx context.Context, key string, now time.Time) (domain.Run, error)
	ListRuns(ctx context.Context, filter domain.RunFilter) ([]domain.Run, error)
	ListSteps(ctx context.Context, runID string) ([]domain.StepRecord, error)
	Mutate(ctx context.Context, id string, fn func(domain.Snapshot) (domain.Write, error)) error
	MutateRun(ctx context.Context, id string, fn func(domain.Snapshot) (domain.Write, error)) error
	CountRunning(ctx context.Context, instanceID string) (int, error)
	HasUnfinishedScheduled(ctx context.Context, workerID, taskID string) (bool, error)
	ListExpiredLeases(ctx context.Context, now time.Time, limit int) ([]string, error)
	ExpireNeverStarted(ctx context.Context, now time.Time, limit int) ([]domain.RunRef, error)
	ExpireWorkerWaits(ctx context.Context, now time.Time, wait time.Duration, limit int) ([]domain.RunRef, error)
	RenewLeases(ctx context.Context, instanceID string, attempts []domain.AttemptRef, expiresAt time.Time) ([]domain.AttemptRef, error)
}

// RunQueue is the ready-Run queue the dispatcher consumes. A Run is ready when its
// next attempt, or its due time when it has none, has come. The Run's state and its
// lease are the system of record: every change to a Run goes through the RunStore,
// in one transaction, and every claim is confirmed there.
//
// A queue may hold stale entries: a Run that was failed, finished, or claimed since
// it was announced. A claim names only a Run that the store still shows as queued and
// due, and a stale entry is dropped. Every consumer also polls the store, so a lost
// announcement costs latency only.
type RunQueue interface {
	// ClaimNext takes the oldest ready Run of the Task and starts an attempt on it for
	// the claim's instance. It reports false when no ready Run was free to take.
	ClaimNext(ctx context.Context, workerID, taskID string, claim domain.Claim) (domain.Run, bool, error)
	// GiveBack puts a running Run back in the queue, ready at its due time, and takes
	// its attempt back. It applies only while the lease token is the Run's current one.
	GiveBack(ctx context.Context, id string, token string, reason domain.QueuedReason, now time.Time) error
	// ListQueuedTasks returns the Tasks that have at least one queued Run.
	ListQueuedTasks(ctx context.Context) ([]domain.Task, error)
	// SetQueuedReason records why the ready queued Runs of the Task wait.
	SetQueuedReason(ctx context.Context, workerID, taskID string, reason domain.QueuedReason, now time.Time) error
	// Ready is called whenever a Run of the Task becomes ready at the given time: when
	// it is created, retried, or released. A queue that is only polled may do nothing
	// here. A failure is logged by the caller and does not undo the committed Run.
	Ready(ctx context.Context, workerID, taskID string, at time.Time) error
}

// ScheduleStore is the schedule store that the scheduler and the registration work
// against. Each schedule has a rule and a next tick. The scheduler reads the ticks due
// soon, and each tick is fired by a compare-and-set, so no transaction spans this
// interface and the run queue.
type ScheduleStore interface {
	// LoadDue returns up to limit schedules whose next tick is at or before until, earliest first.
	LoadDue(ctx context.Context, until time.Time, limit int) ([]domain.ScheduleTimer, error)
	// Get returns the stored schedule of the Task. The bool is false when it has none.
	Get(ctx context.Context, workerID, taskID string) (domain.ScheduleEntry, bool, error)
	// Advance sets the schedule's next tick to next, only while its stored tick is tick.
	// A nil next means no further tick. It reports whether the write took effect; false
	// means the schedule changed since it was read, and nothing was written.
	Advance(ctx context.Context, workerID, taskID string, tick time.Time, next *time.Time) (bool, error)
	// ListMissed returns up to limit schedules whose next tick is before now, in the
	// order of their Task, after the given Task. It is the page of a keyset walk.
	ListMissed(ctx context.Context, now time.Time, afterWorkerID, afterTaskID string, limit int) ([]domain.ScheduleEntry, error)
	// List returns the schedules of a worker ID, ordered by Task ID.
	List(ctx context.Context, workerID string) ([]domain.ScheduleEntry, error)
	// Replace stores the entries of a worker ID and removes that worker ID's other schedules.
	// An entry whose rule matches the stored one keeps the stored next tick.
	Replace(ctx context.Context, workerID string, entries []domain.ScheduleEntry) error
}
