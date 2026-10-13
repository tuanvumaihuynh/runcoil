package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// newTestDispatcher builds a dispatcher on the fake stores, with a fixed clock.
func newTestDispatcher(store *fakeStore, dir *instances, signals Signals, now time.Time) *dispatcher {
	return newDispatcher(
		dispatcherConfig{LeaseDuration: 30 * time.Second, SendTimeout: 5 * time.Second,
			EvictAfter: 90 * time.Second, WorkerWaitLimit: time.Hour,
			Retry:        domain.RetryConfig{MaxAttempts: 4, Delays: []time.Duration{time.Second}},
			PollInterval: time.Second, SweepInterval: time.Second},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		store,
		store,
		signals,
		dir,
		func() time.Time { return now },
		func() float64 { return 0.5 },
	)
}

func TestStartDeadlineFailsUndispatchedRunAndNeverDispatchesIt(t *testing.T) {
	store := newFakeStore()
	registerTestTaskWithDeadline(t, store, 2*time.Second)
	id := store.seedQueued(t, testRef, testNow)
	d := newTestDispatcher(store, newInstances(), newFakeSignals(), testNow)
	ctx := context.Background()

	// Before the deadline the Run is left alone.
	err := d.expireNeverStarted(ctx, testNow.Add(time.Second))
	require.NoError(t, err, "expire before the deadline")
	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "before the deadline")

	// After the deadline the Run fails, and it is not claimed afterwards.
	now := testNow.Add(3 * time.Second)
	err = d.expireNeverStarted(ctx, now)
	require.NoError(t, err, "expire")

	run, _ = store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusFailed, run.Status, "after the deadline")
	require.NotNil(t, run.Failure, "after the deadline")
	require.Equal(t, domain.FailureCodeStartDeadline, run.Failure.Code, "after the deadline")
	require.Equal(t, 0, run.Failure.Attempts, "after the deadline")

	_, claimed, _ := store.ClaimNext(ctx, testRef, domain.Claim{Now: now})
	require.False(t, claimed, "an expired Run must never be claimed")
}

func TestStartDeadlineIgnoresRunsWithoutDeadline(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	id := store.seedQueued(t, testRef, testNow)
	d := newTestDispatcher(store, newInstances(), newFakeSignals(), testNow)

	err := d.expireNeverStarted(context.Background(), testNow.Add(time.Hour))
	require.NoError(t, err, "expire")
	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "a Task without a deadline must keep a late Run queued")
}

// failingExpiryStore is the fake store with a start-deadline expiry that can fail, as a
// statement that was cancelled does. It records whether a pass listed the queued Tasks.
type failingExpiryStore struct {
	*fakeStore
	expiryErr error
	listed    bool
}

func (f *failingExpiryStore) ExpireNeverStarted(ctx context.Context, now time.Time, limit int) ([]domain.RunRef, error) {
	if f.expiryErr != nil {
		return nil, f.expiryErr
	}

	return f.fakeStore.ExpireNeverStarted(ctx, now, limit)
}

func (f *failingExpiryStore) ListQueuedTasks(ctx context.Context) ([]domain.TaskRef, error) {
	f.listed = true
	return f.fakeStore.ListQueuedTasks(ctx)
}

// A pass whose expiry failed claims nothing and lists no Task, so a Run past its deadline
// cannot be claimed. The next pass runs the expiry again and dispatches the Run.
func TestFailedStartDeadlineExpiryEndsThePassWithoutClaiming(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	id := store.seedQueued(t, testRef, testNow)
	dir := newInstances()
	registerI1(dir, testNow)
	sess, ok := attach(dir, "i-1", testNow)
	require.True(t, ok, "attach the instance")
	d := newTestDispatcher(store, dir, newFakeSignals(), testNow)
	failing := &failingExpiryStore{fakeStore: store, expiryErr: errors.New("canceling statement due to statement timeout")}
	d.runs, d.queue = failing, failing
	ctx := context.Background()

	d.dispatchPass(ctx)
	require.False(t, failing.listed, "a pass whose expiry failed must not list the queued Tasks")
	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "a pass whose expiry failed claims nothing")
	require.Zero(t, run.Attempts, "a pass whose expiry failed claims nothing")

	// The stream takes the next dispatch, so the Run is sent and running.
	failing.expiryErr = nil
	go func() {
		out := <-sess.out
		out.Result <- nil
	}()
	d.dispatchPass(ctx)
	require.True(t, failing.listed, "the next pass lists the queued Tasks")
	run, _ = store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusRunning, run.Status, "the next pass dispatches the Run")
	require.Equal(t, 1, run.Attempts, "the next pass dispatches the Run")
}

// A give back after a failed send returns the attempt even when the dispatch was
// cancelled, and reports the cause.
func TestFailedDispatchGivesAttemptBackEvenWhenCancelled(t *testing.T) {
	store := newFakeStore()
	id := store.seedRunning(t, testRef, "tok-1")
	d := newTestDispatcher(store, newInstances(), newFakeSignals(), testNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cause := errors.New("send failed")
	err := d.failedDispatch(ctx, testRef, id, "tok-1", testNow, cause)
	require.ErrorIs(t, err, cause, "failedDispatch err")

	stored, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, stored.Status, "after give back")
	require.Equal(t, 0, stored.Attempts, "after give back")
	require.Empty(t, stored.LeaseToken, "after give back")
	require.Equal(t, domain.QueuedReasonWaitingForWorker, stored.QueuedReason, "no stream is open")
	require.Len(t, store.readies, 1, "a Run that is given back is ready")
}

// A worker whose stream takes no dispatch in time is logged as a send timeout. The stream is
// the real one, and nothing reads its outbox, so the send ends on its own timer.
func TestDispatchToStalledStreamIsEndedAsSendTimeout(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	id := seedLeased(t, store, testRef, "tok-1", false, testNow.Add(time.Minute))
	run, _ := store.GetRun(context.Background(), id)
	d := newTestDispatcher(store, newInstances(), newFakeSignals(), testNow)
	d.cfg.SendTimeout = 20 * time.Millisecond
	target := candidate{InstanceID: "i-1", Slots: 1, Stream: newStream()}

	err := d.sendAttempt(context.Background(), testRef, target, run, nil, "tok-1", testNow)
	require.NoError(t, err, "sendAttempt")

	cause := d.ends.take(target.Stream)
	require.Equal(t, "send timeout", cause, "cause")
}

func registerTestTaskWithDeadline(t *testing.T, store *fakeStore, deadline time.Duration) {
	t.Helper()
	task := domain.Task{Ref: testRef, StartDeadline: deadline}
	err := store.UpsertTasks(context.Background(), []domain.Task{task})
	require.NoError(t, err, "upsert")
}

// An acknowledgement that arrives after the lease expired, before the sweep, is refused.
func TestLateAckIsStaleAndTheSweepTakesTheAttemptBack(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := seedLeased(t, store, testRef, "tok-1", false, testNow.Add(-time.Second))

	err := e.ackDispatch(context.Background(), id, "tok-1")
	require.Equal(t, apierr.CodeStaleLease, engineCode(err), "late ack: err = %v", err)
	err = e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "a never-acked attempt whose lease ran out is given back")
	require.Equal(t, 0, run.Attempts, "a never-acked attempt whose lease ran out is given back")
}

// A worker lost while running a Step with its own limit of one try is not retried,
// because the Step's policy, sent by the heartbeat, is used before the Task's.
func TestLostStepUsesThePolicyTheHeartbeatNamed(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second}})
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	attachInstance(t, e, "i-1", testNow)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(10*time.Second))

	ctx := context.Background()
	_, _, err := e.heartbeat(ctx, "i-1", []domain.AttemptRef{{
		RunID: id, Token: "tok-1", StepID: "charge", StepAttempt: 1,
		Retry: &domain.RetryPolicy{MaxAttempts: 1},
	}})
	require.NoError(t, err, "heartbeat")

	// The heartbeat renewed the lease to testNow+30s; the sweep is at testNow+31s.
	err = e.dispatcher.sweepLeases(ctx, testNow.Add(31*time.Second))
	require.NoError(t, err, "sweep")

	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusFailed, run.Status, "the Step allows one try")
	require.NotNil(t, run.Failure, "the Step allows one try")
	require.Equal(t, domain.FailureCodeAttemptLost, run.Failure.Code, "the Step allows one try")
	require.Equal(t, "charge", run.Failure.StepID, "the Step allows one try")
}

// Without a Step policy in the heartbeat, the Task's limit decides, so the loss is retried.
func TestLostStepFallsBackToTheTaskPolicy(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second}})
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	attachInstance(t, e, "i-1", testNow)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(10*time.Second))

	ctx := context.Background()
	_, _, err := e.heartbeat(ctx, "i-1", []domain.AttemptRef{{
		RunID: id, Token: "tok-1", StepID: "charge", StepAttempt: 1,
	}})
	require.NoError(t, err, "heartbeat")
	err = e.dispatcher.sweepLeases(ctx, testNow.Add(31*time.Second))
	require.NoError(t, err, "sweep")

	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "queued for a retry under the Task's limit")
	require.Equal(t, domain.QueuedReasonRetryBackoff, run.QueuedReason, "queued for a retry under the Task's limit")
}

// A restart does not sweep leases until one lease has passed since the start, so a
// worker that is reconnecting keeps its attempt.
func TestRestartGraceDefersTheLeaseSweep(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	d := e.dispatcher
	d.graceUntil(testNow.Add(30 * time.Second))
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(-time.Second))
	ctx := context.Background()

	d.sweepPass(ctx) // at testNow, inside the grace
	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusRunning, run.Status, "inside the grace the attempt must not be swept")

	later := newFakeService(t, store, testConfig(), testNow.Add(31*time.Second))
	later.dispatcher.graceUntil(testNow.Add(30 * time.Second))
	later.dispatcher.sweepPass(ctx)
	run, _ = store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "after the grace the expired attempt is swept")
}

// A sweep over more leases than one batch takes back all of them, batch after batch.
func TestSweepWorksThroughEveryBatch(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	const n = batchSize*2 + 37
	for range n {
		runID := uuid.NewString()
		store.mu.Lock()
		store.runs[runID] = domain.Run{
			ID: runID, Task: testRef, Status: domain.RunStatusRunning, Source: domain.RunSourceInvoke,
			Attempts: 1, LeaseToken: uuid.NewString(), LeaseExpiresAt: testNow.Add(-time.Second),
			InstanceID: "gone", DueAt: testNow, CreatedAt: testNow, StartedAt: testNow,
		}

		store.mu.Unlock()
	}

	err := e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	store.mu.Lock()
	defer store.mu.Unlock()
	queued := 0
	for _, run := range store.runs {
		if run.Status == domain.RunStatusQueued {
			queued++
		}
	}

	require.Equal(t, n, queued, "given back: all of them")
}

func TestSweepGivesBackUnackedAttemptWithoutTry(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil)
	expired := testNow.Add(-time.Second)
	id := seedLeased(t, store, testRef, "tok-1", false, expired)

	err := e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "unacked attempt")
	require.Equal(t, 0, run.Attempts, "unacked attempt")
	require.Empty(t, run.LeaseToken, "unacked attempt")
	require.Equal(t, 0, run.HandlerAttemptsUsed, "a give back uses no try")
	require.True(t, run.NextAttemptAt.IsZero(), "a give back waits for no retry: next = %v", run.NextAttemptAt)
}

func TestSweepCountsAckedLossOnNamedStep(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(-time.Second))
	// The last heartbeat named step b, try 1.
	store.mu.Lock()
	run := store.runs[id]
	run.RunningStepID = "b"
	run.RunningStepAttempt = 1
	store.runs[id] = run
	store.mu.Unlock()

	err := e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	stored, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, stored.Status, "run status, want queued for a retry")
	require.Equal(t, domain.QueuedReasonRetryBackoff, stored.QueuedReason, "run reason, want queued for a retry")
	require.Empty(t, stored.RunningStepID, "the running step is cleared")

	steps, _ := store.ListSteps(context.Background(), id)
	require.Len(t, steps, 1, "steps: want b pending at try 1")
	require.Equal(t, "b", steps[0].StepID, "steps: want b pending at try 1")
	require.Equal(t, domain.StepStatusPending, steps[0].Status, "steps: want b pending at try 1")
	require.Equal(t, 1, steps[0].AttemptsUsed, "steps: want b pending at try 1")

	// The lost worker's late report is stale: the lease is no longer current.
	_, err = e.finishRun(context.Background(), id, domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded})
	require.Equal(t, apierr.CodeStaleLease, engineCode(err), "late report: err = %v", err)
}

func TestSweepFinalLossFailsRunWithAttemptLost(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, &domain.RetryPolicy{MaxAttempts: 1})
	e := newServiceWith(t, store, nil)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(-time.Second))

	err := e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusFailed, run.Status, "want FAILED ATTEMPT_LOST")
	require.NotNil(t, run.Failure, "want FAILED ATTEMPT_LOST")
	require.Equal(t, domain.FailureCodeAttemptLost, run.Failure.Code, "want FAILED ATTEMPT_LOST")
}

// A lost attempt whose stream is still open is told to stop.
func TestSweepSendsCancelOnOpenStream(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	sess := attachInstance(t, e, "i-1", testNow)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(-time.Second))

	got := make(chan streamMessage, 1)
	go func() {
		item := <-sess.Outbox()
		got <- item.Message
		item.Result <- nil
	}()

	err := e.dispatcher.sweepLeases(context.Background(), testNow)
	require.NoError(t, err, "sweep")

	select {
	case msg := <-got:
		require.NotNil(t, msg.Cancel, "message = %+v, want cancel for the lost attempt", msg)
		require.Equal(t, id, msg.Cancel.RunID, "cancel run")
		require.Equal(t, "tok-1", msg.Cancel.Token, "cancel token")
	case <-time.After(2 * time.Second):
		require.Fail(t, "no cancel was sent on the open stream")
	}
}

func TestWorkerWaitLimitFailsRunWithoutWorker(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil) // one hour
	id := store.seedWaiting(t, testRef, testNow.Add(-2*time.Hour))

	err := e.dispatcher.expireWorkerWaits(context.Background(), testNow)
	require.NoError(t, err, "expire")

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusFailed, run.Status, "want FAILED WORKER_WAIT_LIMIT")
	require.NotNil(t, run.Failure, "want FAILED WORKER_WAIT_LIMIT")
	require.Equal(t, domain.FailureCodeWorkerWaitLimit, run.Failure.Code, "want FAILED WORKER_WAIT_LIMIT")
}

// Within the restart grace, a worker wait is not expired: no worker has had time to reconnect.
func TestRestartGraceDefersTheWorkerWaitExpiry(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	e.dispatcher.graceUntil(testNow.Add(30 * time.Second))
	id := store.seedWaiting(t, testRef, testNow.Add(-2*time.Hour)) // past the one-hour limit
	ctx := context.Background()

	e.dispatcher.sweepPass(ctx)
	run, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, run.Status, "inside the grace the worker wait is not expired")

	later := newFakeService(t, store, testConfig(), testNow.Add(31*time.Second))
	later.dispatcher.graceUntil(testNow.Add(30 * time.Second))
	later.dispatcher.sweepPass(ctx)
	run, _ = store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusFailed, run.Status, "after the grace the worker wait expires")
	require.NotNil(t, run.Failure, "after the grace the worker wait expires")
	require.Equal(t, domain.FailureCodeWorkerWaitLimit, run.Failure.Code, "after the grace the worker wait expires")
}
