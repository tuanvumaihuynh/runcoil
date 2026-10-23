package engine

import (
	"context"
	rand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// giveBackTimeout bounds the write that gives an attempt back after a failed send.
const giveBackTimeout = 5 * time.Second

// dispatcherConfig holds the durations and rules the dispatcher and its sweeps use.
type dispatcherConfig struct {
	LeaseDuration   time.Duration
	SendTimeout     time.Duration
	EvictAfter      time.Duration
	WorkerWaitLimit time.Duration
	Retry           domain.RetryConfig
	PollInterval    time.Duration
	SweepInterval   time.Duration
}

// dispatcher is the one writer that starts attempts, and that enforces the rules
// about time. It runs two passes. The dispatch pass runs on its poll interval and
// when a Run becomes ready: it expires the Runs that never started, then claims and
// sends attempts. The sweep pass runs on its own interval: it enforces instance
// liveness, expires worker waits, and takes back lost attempts. A Run that expires is
// never claimed, because the dispatch pass expires before it claims, at one now.
type dispatcher struct {
	cfg       dispatcherConfig
	logger    *slog.Logger
	tasks     TaskStore
	runs      RunStore
	queue     RunQueue
	instances *instances
	signals   Signals
	now       func() time.Time
	unit      func() float64
	// grace is the time before which no lease is swept, after the server started.
	grace time.Time
	// ends records why a stream was closed by the dispatcher, so its detach can say so.
	ends *streamEnds
}

// batchSize is how many Runs one expiry or sweep call handles at most. A pass
// that finds more repeats with the next batch, so no single call grows without bound.
const batchSize = 500

// graceUntil sets the time before which no lease is swept.
func (d *dispatcher) graceUntil(t time.Time) {
	d.grace = t
}

// newDispatcher creates the dispatcher from its configuration, its stores and signals, and the instance record.
func newDispatcher(
	cfg dispatcherConfig,
	logger *slog.Logger,
	tasks TaskStore,
	runs RunStore,
	queue RunQueue,
	signals Signals,
	inst *instances,
	now func() time.Time,
	unit func() float64,
) *dispatcher {
	return &dispatcher{
		cfg:       cfg,
		logger:    logger,
		tasks:     tasks,
		runs:      runs,
		queue:     queue,
		instances: inst,
		signals:   signals,
		now:       now,
		unit:      unit,
		ends:      newStreamEnds(),
	}
}

// wakeUp asks the dispatch pass to run now. Calls made before it runs count as one.
func (d *dispatcher) wakeUp() {
	d.signals.Notify(TopicRuns)
}

// run runs the two passes until ctx ends.
func (d *dispatcher) run(ctx context.Context) {
	runs, unsubscribe := d.signals.Subscribe(TopicRuns)
	defer unsubscribe()
	dispatchTicker := time.NewTicker(d.cfg.PollInterval)
	defer dispatchTicker.Stop()
	sweepTicker := time.NewTicker(d.cfg.SweepInterval)
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dispatchTicker.C:
			d.dispatchPass(ctx)
		case <-runs:
			d.dispatchPass(ctx)
		case <-sweepTicker.C:
			d.sweepPass(ctx)
		}
	}
}

// dispatchPass expires the Runs that never started, then dispatches what is ready. A
// failed expiry ends the pass with no claim: a Run past its start deadline must never be
// claimed, and without the expiry the pass cannot tell which queued Runs are past it. The
// next pass tries again. A failed dispatch of one Task is logged, and the other Tasks
// still run.
func (d *dispatcher) dispatchPass(ctx context.Context) {
	now := d.now()
	if err := d.expireNeverStarted(ctx, now); err != nil {
		d.logFailure(ctx, "expire never started runs", err)
		return
	}

	refs, err := d.queue.ListQueuedTasks(ctx)
	if err != nil {
		d.logFailure(ctx, "list queued tasks", err)
		return
	}

	for _, ref := range refs {
		key := taskKey{workerID: ref.WorkerID, taskID: ref.TaskID}
		if err := d.dispatchTask(ctx, key, now); err != nil {
			d.logger.ErrorContext(
				ctx,
				"dispatch task failed",
				slog.String("worker_id", key.workerID),
				slog.String("task_id", key.taskID),
				slog.Any("error", err),
			)
		}
	}
}

// sweepPass enforces instance liveness, expires worker waits, and takes back the
// lost attempts. Each step runs even when an earlier one failed.
func (d *dispatcher) sweepPass(ctx context.Context) {
	now := d.now()
	closed, evicted := d.instances.Sweep(now, d.cfg.LeaseDuration, d.cfg.EvictAfter)
	for _, id := range evicted {
		d.logger.InfoContext(
			ctx,
			"instance evicted",
			slog.String("instance_id", id),
		)
	}

	if closed > 0 {
		d.logger.DebugContext(
			ctx,
			"streams ended for silence",
			slog.Int("streams_closed", closed),
		)
	}

	// Nothing that depends on a worker being ready is decided before the restart grace has
	// passed: attached workers need one lease to reconnect and heartbeat. The start
	// deadline is not part of this pass, so it is not held back.
	if now.Before(d.grace) {
		return
	}

	if err := d.expireWorkerWaits(ctx, now); err != nil {
		d.logFailure(ctx, "expire worker waits", err)
	}

	if err := d.sweepLeases(ctx, now); err != nil {
		d.logFailure(ctx, "sweep leases", err)
	}
}

func (d *dispatcher) logFailure(ctx context.Context, what string, err error) {
	if ctx.Err() != nil {
		return
	}

	d.logger.ErrorContext(
		ctx,
		"dispatcher step failed",
		slog.String("step", what),
		slog.Any("error", err),
	)
}

// expireNeverStarted fails the Runs that never started within their start deadline, in batches.
func (d *dispatcher) expireNeverStarted(ctx context.Context, now time.Time) error {
	return drainRefs(batchSize, func(limit int) ([]domain.RunRef, error) {
		return d.runs.ExpireNeverStarted(ctx, now, limit)
	}, func(refs []domain.RunRef) {
		d.reportServerFailures(ctx, refs, domain.FailureCodeStartDeadline)
	})
}

// logServerFailure records a Run that the server failed for a reason of its own, such as
// a start deadline or a worker wait limit. The Run is decided in the store; the line is here.
func (d *dispatcher) logServerFailure(ctx context.Context, ref domain.RunRef, code domain.FailureCode) {
	d.logger.InfoContext(
		ctx,
		"run failed",
		slog.String("run_id", ref.ID),
		slog.String("worker_id", ref.WorkerID),
		slog.String("task_id", ref.TaskID),
		slog.String("failure_code", string(code)),
	)
}

// reportServerFailures logs and notifies one batch of Runs that the server failed.
func (d *dispatcher) reportServerFailures(ctx context.Context, refs []domain.RunRef, code domain.FailureCode) {
	for _, ref := range refs {
		d.logServerFailure(ctx, ref, code)
		d.signals.Notify(topicRunFinished(ref.ID))
	}
}

// expireWorkerWaits fails the Runs that waited for a worker past the worker wait limit, in batches.
func (d *dispatcher) expireWorkerWaits(ctx context.Context, now time.Time) error {
	if d.cfg.WorkerWaitLimit <= 0 {
		return nil
	}

	return drainRefs(batchSize, func(limit int) ([]domain.RunRef, error) {
		return d.runs.ExpireWorkerWaits(ctx, now, d.cfg.WorkerWaitLimit, limit)
	}, func(refs []domain.RunRef) {
		d.reportServerFailures(ctx, refs, domain.FailureCodeWorkerWaitLimit)
	})
}

// sweepLeases takes back the running attempts whose leases have expired, a batch at a
// time. Each Run is handled in its own transaction. A failure on one Run does not stop
// the others. A batch in which every Run failed ends the pass, so a Run that cannot be
// swept does not hold the pass.
func (d *dispatcher) sweepLeases(ctx context.Context, now time.Time) error {
	var firstErr error
	for {
		ids, err := d.runs.ListExpiredLeases(ctx, now, batchSize)
		if err != nil {
			return err
		}

		failed := 0
		for _, id := range ids {
			if err := d.sweepLease(ctx, id, now); err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("sweep run %s: %w", id, err)
				}
			}
		}

		if len(ids) < batchSize || failed == len(ids) {
			return firstErr
		}
	}
}

// sweepLease decides the fate of one expired attempt. An attempt that was lost is
// told to stop, on its stream if the stream is still open.
func (d *dispatcher) sweepLease(ctx context.Context, id string, now time.Time) error {
	run, err := d.runs.GetRun(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	taskPolicy, err := d.taskPolicy(ctx, taskKey{workerID: run.WorkerID, taskID: run.TaskID})
	if err != nil {
		return err
	}

	// The queued reason is read before the Run is locked: the instance record is not read under the lock.
	reason := queuedReasonOf(d.instances, taskKey{workerID: run.WorkerID, taskID: run.TaskID})

	decision, err := d.decideSweep(ctx, id, now, reason, taskPolicy)
	if err != nil {
		return err
	}

	if decision.outcome == domain.LostNone {
		return nil
	}

	logger := d.logger.With(
		slog.String("run_id", id),
		slog.String("worker_id", run.WorkerID),
		slog.String("task_id", run.TaskID),
	)

	logger.InfoContext(
		ctx,
		"attempt lease swept",
		slog.String("instance_id", decision.holder),
		slog.Int("attempt", decision.attempt),
		slog.String("outcome", lostOutcomeWords(decision.outcome)),
	)

	d.reportLostOutcome(ctx, logger, id, run, decision.outcome, decision.failure, decision.nextAttempt)

	if decision.outcome != domain.LostFinal {
		if err := d.queue.Ready(ctx, run.WorkerID, run.TaskID, now); err != nil {
			d.logFailure(ctx, "announce ready run", err)
		}
	}

	d.wakeUp()
	d.cancelAttempt(ctx, decision.holder, id, decision.token)
	return nil
}

// sweepDecision is what the sweep decided about one expired lease, with the parts of the
// Run that the log lines and the cancel need.
type sweepDecision struct {
	holder      string
	token       string
	attempt     int
	outcome     domain.LostOutcome
	nextAttempt *time.Time
	failure     *domain.Failure
}

// decideSweep runs the sweep decision on the Run in one transaction.
func (d *dispatcher) decideSweep(
	ctx context.Context,
	id string,
	now time.Time,
	reason domain.QueuedReason,
	taskPolicy *domain.RetryPolicy,
) (sweepDecision, error) {
	var out sweepDecision
	err := d.runs.Mutate(ctx, id, func(s domain.Snapshot) (domain.Write, error) {
		out.holder = s.Run.InstanceID
		out.token = s.Run.LeaseToken
		out.attempt = s.Run.Attempts
		write, outcome := domain.SweepLease(s, now, reason, taskPolicy, d.cfg.Retry, d.unit())
		out.outcome = outcome
		out.nextAttempt = write.Run.NextAttemptAt
		out.failure = write.Run.Failure
		return write, nil
	})

	return out, err
}

// reportLostOutcome logs what the sweep did with a lost attempt, and notifies on a final loss.
// logger is the sweep's logger, already scoped to the Run.
func (d *dispatcher) reportLostOutcome(
	ctx context.Context,
	logger *slog.Logger,
	id string,
	run domain.Run,
	outcome domain.LostOutcome,
	failure *domain.Failure,
	nextAttempt *time.Time,
) {
	switch outcome {
	case domain.LostFinal:
		code := domain.FailureCodeAttemptLost
		if failure != nil {
			code = failure.Code
		}

		d.logServerFailure(ctx, domain.RunRef{ID: id, WorkerID: run.WorkerID, TaskID: run.TaskID}, code)
		d.signals.Notify(topicRunFinished(id))
	case domain.LostRetry:
		logger.DebugContext(
			ctx,
			"retry scheduled",
			slog.String("reason", "worker lost"),
			slog.Time("next_attempt_at", *nextAttempt),
		)
	case domain.LostGivenBack:
		logger.DebugContext(ctx, "run given back", slog.String("reason", "dispatch never acknowledged"))
	}
}

// cancelAttempt tells the instance to stop an attempt whose result will not be
// accepted. It is best effort: the stream may be gone, and the send is bounded.
func (d *dispatcher) cancelAttempt(ctx context.Context, instanceID string, id string, token string) {
	sess := d.instances.StreamOf(instanceID)
	if sess == nil {
		return
	}

	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.SendTimeout)
	defer cancel()

	msg := streamMessage{Cancel: &domain.AttemptRef{RunID: id, Token: token}}
	if err := sess.Send(sendCtx, msg, d.cfg.SendTimeout); err != nil {
		d.logger.DebugContext(
			ctx,
			"cancel not sent",
			slog.String("run_id", id),
			slog.Any("error", err),
		)
	}
}

// taskPolicy returns the retry policy the Task registered, or nil when it has none.
func (d *dispatcher) taskPolicy(ctx context.Context, ref taskKey) (*domain.RetryPolicy, error) {
	task, err := d.tasks.GetTask(ctx, ref.workerID, ref.taskID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return task.Retry, nil
}

// dispatchTask starts attempts of the ready queued Runs of one Task while an instance
// that serves it has room. When none has room, it records why the Runs wait. now is
// the pass's time, the same one the expiry used; each claim reads the clock for itself.
func (d *dispatcher) dispatchTask(ctx context.Context, ref taskKey, now time.Time) error {
	for {
		candidates := d.instances.Serving(ref)
		if len(candidates) == 0 {
			return d.queue.SetQueuedReason(ctx, ref.workerID, ref.taskID, domain.QueuedReasonWaitingForWorker, now)
		}

		target, ok, err := d.instanceWithRoom(ctx, candidates)
		if err != nil {
			return err
		}

		if !ok {
			return d.queue.SetQueuedReason(ctx, ref.workerID, ref.taskID, domain.QueuedReasonWaitingForCapacity, now)
		}

		token, err := newLeaseToken()
		if err != nil {
			return err
		}

		// Each claim reads the clock, so a slow pass does not hand out leases that are short
		// or already expired.
		claimAt := d.now()
		run, found, err := d.queue.ClaimNext(ctx, ref.workerID, ref.taskID, domain.Claim{
			InstanceID:     target.InstanceID,
			LeaseToken:     token,
			LeaseExpiresAt: claimAt.Add(d.cfg.LeaseDuration),
			Now:            claimAt,
		})
		if err != nil {
			return err
		}

		if !found {
			return nil
		}

		// The attempt is committed. Now the worker must receive it. If it does not,
		// the attempt is given back, so the Run waits again without using a try.
		steps, err := d.runs.ListSteps(ctx, run.ID)
		if err != nil {
			return d.failedDispatch(ctx, ref, run.ID, token, now, err)
		}

		if err := d.sendAttempt(ctx, ref, target, run, steps, token, now); err != nil {
			return err
		}
	}
}

// sendAttempt sends a committed attempt to its instance, and gives it back when the send fails.
func (d *dispatcher) sendAttempt(
	ctx context.Context,
	ref taskKey,
	target candidate,
	run domain.Run,
	steps []domain.StepRecord,
	token string,
	now time.Time,
) error {
	msg := streamMessage{Run: &run, Steps: steps}
	if err := target.Stream.Send(ctx, msg, d.cfg.SendTimeout); err != nil {
		d.logger.WarnContext(
			ctx,
			"dispatch not sent",
			slog.String("run_id", run.ID),
			slog.String("instance_id", target.InstanceID),
			slog.Any("error", err),
		)

		cause := "send failed"
		if errors.Is(err, errSendTimeout) || errors.Is(err, context.DeadlineExceeded) {
			cause = "send timeout"
		}

		d.ends.note(target.Stream, cause)
		target.Stream.Close()
		return d.failedDispatch(ctx, ref, run.ID, token, now, nil)
	}

	d.logger.DebugContext(
		ctx,
		"run dispatched",
		slog.String("run_id", run.ID),
		slog.String("worker_id", ref.workerID),
		slog.String("task_id", ref.taskID),
		slog.String("instance_id", target.InstanceID),
		slog.Int("attempt", run.Attempts),
	)
	return nil
}

// failedDispatch gives an attempt back after its dispatch could not be sent. It
// returns cause, or the error of the give back when there is no cause.
func (d *dispatcher) failedDispatch(
	ctx context.Context,
	ref taskKey,
	id string,
	token string,
	now time.Time,
	cause error,
) error {
	// The give back must finish even when the dispatch was cancelled, so it does not use ctx.
	giveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), giveBackTimeout)
	defer cancel()

	reason := queuedReasonOf(d.instances, ref)
	if err := d.queue.GiveBack(giveCtx, id, token, reason, now); err != nil {
		return fmt.Errorf("give back attempt: %w", err)
	}

	d.logger.DebugContext(
		ctx,
		"run given back",
		slog.String("run_id", id),
		slog.String("worker_id", ref.workerID),
		slog.String("task_id", ref.taskID),
		slog.String("reason", "dispatch not sent"),
	)

	if err := d.queue.Ready(giveCtx, ref.workerID, ref.taskID, now); err != nil {
		d.logFailure(ctx, "announce ready run", err)
	}

	d.wakeUp()
	return cause
}

// instanceWithRoom returns the candidate with the most free slots, when it has any.
// Free slots are the registered slots less the attempts the instance holds in storage.
func (d *dispatcher) instanceWithRoom(ctx context.Context, candidates []candidate) (candidate, bool, error) {
	var best candidate
	bestFree := 0
	for _, c := range candidates {
		running, err := d.runs.CountRunning(ctx, c.InstanceID)
		if err != nil {
			return candidate{}, false, err
		}

		if free := c.Slots - running; free > bestFree {
			best, bestFree = c, free
		}
	}

	return best, bestFree > 0, nil
}

// newLeaseToken returns a random token that proves a report belongs to one attempt.
func newLeaseToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// drainRefs calls step with a limit until a batch comes back short. It is for the expiries,
// which report the Runs they changed. Each batch goes to handle as it returns, and no
// batch is kept after that, so a pass over many Runs holds one batch at a time. It stops
// at the first failing batch, after handle has seen the Runs that batch reports.
func drainRefs(
	batch int,
	step func(limit int) ([]domain.RunRef, error),
	handle func(refs []domain.RunRef),
) error {
	for {
		refs, err := step(batch)
		handle(refs)

		if err != nil {
			return err
		}

		if len(refs) < batch {
			return nil
		}
	}
}

// lostOutcomeWords says what the sweep did with a lost attempt, in words for the logs.
func lostOutcomeWords(o domain.LostOutcome) string {
	switch o {
	case domain.LostGivenBack:
		return "given back"
	case domain.LostRetry:
		return "retry scheduled"
	case domain.LostFinal:
		return "failed with no tries left"
	default:
		return "none"
	}
}
