package domain

import (
	"time"
)

// AttemptRef names one attempt by its Run and its lease token. In a heartbeat it
// also names the Step that the attempt is running now, and its try number.
type AttemptRef struct {
	RunID       string
	Token       string
	StepID      string
	StepAttempt int
	// Retry is the running Step's own policy, or nil when the Step has none.
	Retry *RetryPolicy
}

// Snapshot is a Run and its Step records, as they are under the Run's lock.
type Snapshot struct {
	Run   Run
	Steps []StepRecord
}

// Write is what a lifecycle decision saves: the Run in full, and optionally one Step record.
type Write struct {
	Run  Run
	Step *StepRecord
}

// RetryAnswer is the answer that a try will be attempted again.
type RetryAnswer struct {
	NotBefore    time.Time
	AttemptsUsed int
}

// FinishKind is how a worker ends its attempt.
type FinishKind int

const (
	// FinishSucceeded: the handler returned its output.
	FinishSucceeded FinishKind = iota + 1
	// FinishFailed: the handler returned an error.
	FinishFailed
	// FinishReleased: the worker gives the Run back, for example while shutting down.
	FinishReleased
)

// FinishInput is one FinishRun report.
type FinishInput struct {
	Token  string
	Kind   FinishKind
	Output []byte
	Error  AttemptError
	// StepID names the Step whose final error the handler returned, if any.
	StepID string
	// ReleaseReason is the queued reason to set when the attempt is released.
	ReleaseReason QueuedReason
}

// FinishAnswer is the answer to FinishRun: the finished Run, or a retry.
type FinishAnswer struct {
	Run   Run
	Retry *RetryAnswer
}

// StepAnswer is the answer to RecordStep: the stored record, or a retry.
type StepAnswer struct {
	Record *StepRecord
	Retry  *RetryAnswer
}

// LostOutcome is what the sweeper did with an attempt whose lease expired.
type LostOutcome int

const (
	// LostNone: the lease has not expired, or the Run is not running. Nothing changed.
	LostNone LostOutcome = iota
	// LostGivenBack: the dispatch was never acknowledged, so the attempt was given back and no try was used.
	LostGivenBack
	// LostRetry: the attempt was counted as a failed try, and the Run waits for its next attempt.
	LostRetry
	// LostFinal: the attempt was counted as a failed try with no tries left. The Run failed with ATTEMPT_LOST.
	LostFinal
)

// lostMessage is the text of an ATTEMPT_LOST failure.
const lostMessage = "the worker was lost and no tries are left"

// FindStep returns the record of the Step, or nil when it has none.
func FindStep(steps []StepRecord, stepID string) *StepRecord {
	for i := range steps {
		if steps[i].StepID == stepID {
			return &steps[i]
		}
	}

	return nil
}

// AckRun records the acknowledgement of the attempt. The first acknowledgement
// of a Run also starts it, so StartedAt is kept from the first one. An acknowledgement
// that arrives after the lease expired is refused, even before the sweep takes the
// attempt back: the lease has run out, and the attempt belongs to the sweep now.
func AckRun(s Snapshot, token string, now time.Time) (Write, error) {
	if !leaseIsCurrent(s.Run, token) || !s.Run.LeaseExpiresAt.After(now) {
		return Write{}, ErrStaleLease
	}

	run := s.Run
	if run.AckedAt.IsZero() {
		run.AckedAt = now
	}

	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}

	return Write{Run: run}, nil
}

// GiveBack puts a running Run back in the queue because its dispatch was never
// received. The attempt is given back and no try is used. The Run was never
// acknowledged, so it is not started, and it is ready at its due time. A Run whose
// lease is not current is left as it is.
func GiveBack(s Snapshot, token string, reason QueuedReason, now time.Time) Write {
	if !leaseIsCurrent(s.Run, token) {
		return Write{Run: s.Run}
	}

	run := giveBack(s.Run, reason, now)
	return Write{Run: run}
}

// FinishRun decides the answer to a worker's end of an attempt, and the Run to save.
//
// A report that the stored state already shows as applied gets the same answer
// again, even when the lease is no longer current. A finished Run with the same
// token answers finished. An attempt that ended by a retry or a release answers
// retry again for the same kind of report, even after the Run was dispatched
// again. Any other report needs the lease of the running attempt, and otherwise
// gets ErrStaleLease.
func FinishRun(
	s Snapshot,
	in FinishInput,
	taskPolicy *RetryPolicy,
	cfg RetryConfig,
	limits Limits,
	now time.Time,
	unit float64,
) (Write, FinishAnswer, error) {
	run := s.Run

	if in.Token != "" && in.Kind != FinishSucceeded && run.EndedToken == in.Token && run.EndedKind == endKindOf(in.Kind) {
		return Write{Run: run}, FinishAnswer{Retry: &RetryAnswer{
			NotBefore:    run.EndedAt,
			AttemptsUsed: run.HandlerAttemptsUsed,
		}}, nil
	}

	if in.Token != "" && run.Status.Terminal() && run.LeaseToken == in.Token {
		return Write{Run: run}, FinishAnswer{Run: run}, nil
	}

	if !leaseIsCurrent(run, in.Token) {
		return Write{}, FinishAnswer{}, ErrStaleLease
	}

	switch in.Kind {
	case FinishSucceeded:
		if RunOutputTooLarge(in.Output, limits) {
			run.Status = RunStatusFailed
			run.Failure = &Failure{
				Code:     FailureCodeOutputTooLarge,
				Message:  "run output is over its size limit",
				Attempts: run.Attempts,
			}

			run.FinishedAt = now
			return Write{Run: run}, FinishAnswer{Run: run}, nil
		}

		run.Status = RunStatusSucceeded
		run.Output = in.Output
		run.FinishedAt = now
		return Write{Run: run}, FinishAnswer{Run: run}, nil
	case FinishReleased:
		// The attempt was released, not run to an end, so the start of the Run stays.
		// The Run keeps its due time, so it keeps its place in the order.
		run = giveBack(run, in.ReleaseReason, now)
		run.EndedToken = in.Token
		run.EndedKind = EndKindReleased
		run.EndedAt = now
		return Write{Run: run}, FinishAnswer{Retry: &RetryAnswer{
			NotBefore:    now,
			AttemptsUsed: run.HandlerAttemptsUsed,
		}}, nil
	default: // FinishFailed
		return finishFailedRun(s, run, in, taskPolicy, cfg, now, unit)
	}
}

// finishFailedRun decides the answer to a failed attempt: a failed Run, or a retry.
func finishFailedRun(
	s Snapshot,
	run Run,
	in FinishInput,
	taskPolicy *RetryPolicy,
	cfg RetryConfig,
	now time.Time,
	unit float64,
) (Write, FinishAnswer, error) {
	if in.StepID != "" {
		if step := FindStep(s.Steps, in.StepID); step != nil && step.Status == StepStatusFailed {
			// The Failure counts the Run's attempts. The Step keeps its own count on its record.
			run.Status = RunStatusFailed
			run.Failure = &Failure{
				Code:     FailureCodeStepFailed,
				Message:  in.Error.Message,
				Attempts: run.Attempts,
				StepID:   in.StepID,
			}

			run.FinishedAt = now
			return Write{Run: run}, FinishAnswer{Run: run}, nil
		}
	}

	code := NormalizeCode(in.Error.Code)
	policy := ResolveRetry(nil, taskPolicy, cfg)
	attempt := run.HandlerAttemptsUsed + 1
	if in.Error.Permanent || NeverRetried(code) || policy.Exhausted(attempt) {
		run.Status = RunStatusFailed
		run.Failure = &Failure{Code: code, Message: in.Error.Message, Attempts: run.Attempts}
		run.FinishedAt = now
		return Write{Run: run}, FinishAnswer{Run: run}, nil
	}

	due := now.Add(DelayAfter(policy, attempt, in.Error.RetryAfter, cfg.Jitter, unit))
	run.HandlerAttemptsUsed = attempt
	requeueForRetry(&run, due, in.Token, EndKindFailed)
	return Write{Run: run}, FinishAnswer{Retry: &RetryAnswer{NotBefore: due, AttemptsUsed: attempt}}, nil
}

// RecordStep decides the answer to one try of one Step, and the records to save.
//
// A report whose try is already stored gets the stored answer, whatever the lease
// is now. Any other report needs the lease of the running attempt, and must be the
// next try of a Step that is not final. A succeeded output that is over a size
// limit is stored as a failed Step instead.
func RecordStep(
	s Snapshot,
	token string,
	rep StepReport,
	taskPolicy *RetryPolicy,
	cfg RetryConfig,
	limits Limits,
	now time.Time,
	unit float64,
) (Write, StepAnswer, error) {
	existing := FindStep(s.Steps, rep.StepID)
	if existing != nil {
		switch {
		case rep.Attempt == existing.AttemptsUsed:
			if existing.Status == StepStatusPending {
				return Write{Run: s.Run}, StepAnswer{Retry: &RetryAnswer{
					NotBefore:    existing.RetryNotBefore,
					AttemptsUsed: existing.AttemptsUsed,
				}}, nil
			}

			return Write{Run: s.Run}, StepAnswer{Record: existing}, nil
		case rep.Attempt < existing.AttemptsUsed:
			return Write{}, StepAnswer{}, ErrStaleLease
		case existing.Finished():
			return Write{}, StepAnswer{}, ErrStepOrder
		}
	}

	if !leaseIsCurrent(s.Run, token) {
		return Write{}, StepAnswer{}, ErrStaleLease
	}

	next := 1
	position := len(s.Steps) + 1
	if existing != nil {
		next = existing.AttemptsUsed + 1
		position = existing.Position
	}

	if rep.Attempt != next {
		return Write{}, StepAnswer{}, ErrStepOrder
	}

	rec := StepRecord{StepID: rep.StepID, Position: position, AttemptsUsed: rep.Attempt}

	if rep.Succeeded {
		return recordSucceededTry(s, rec, rep, limits)
	}

	return recordFailedTry(s, rec, rep, taskPolicy, cfg, now, unit)
}

// recordSucceededTry stores a succeeded try, or a failed Step when its output is too large.
func recordSucceededTry(s Snapshot, rec StepRecord, rep StepReport, limits Limits) (Write, StepAnswer, error) {
	if over := CheckStepOutput(s.Run, s.Steps, rep.StepID, rep.Attempt, rep.Output, limits); over != nil {
		rec.Status = StepStatusFailed
		rec.Failure = over
		return Write{Run: s.Run, Step: &rec}, StepAnswer{Record: &rec}, nil
	}

	rec.Status = StepStatusSucceeded
	rec.Output = rep.Output
	return Write{Run: s.Run, Step: &rec}, StepAnswer{Record: &rec}, nil
}

// recordFailedTry stores a failed try, and either fails the Step or queues its next try.
func recordFailedTry(
	s Snapshot,
	rec StepRecord,
	rep StepReport,
	taskPolicy *RetryPolicy,
	cfg RetryConfig,
	now time.Time,
	unit float64,
) (Write, StepAnswer, error) {
	code := NormalizeCode(rep.Error.Code)
	policy := ResolveRetry(rep.StepPolicy, taskPolicy, cfg)
	if rep.Error.Permanent || NeverRetried(code) || policy.Exhausted(rep.Attempt) {
		rec.Status = StepStatusFailed
		rec.Failure = &Failure{
			Code:     code,
			Message:  rep.Error.Message,
			Attempts: rep.Attempt,
			StepID:   rep.StepID,
		}

		return Write{Run: s.Run, Step: &rec}, StepAnswer{Record: &rec}, nil
	}

	due := now.Add(DelayAfter(policy, rep.Attempt, rep.Error.RetryAfter, cfg.Jitter, unit))
	rec.Status = StepStatusPending
	rec.RetryNotBefore = due
	run := s.Run
	// The try is recorded on the Step. The Run keeps no ended token, so a repeated
	// report of the same lease gets a stale answer, not a retry.
	requeueForRetry(&run, due, "", EndKindNone)
	return Write{Run: run, Step: &rec}, StepAnswer{Retry: &RetryAnswer{NotBefore: due, AttemptsUsed: rep.Attempt}}, nil
}

// SweepLease decides what happens to a running Run whose lease has expired, and the
// Run and Step to save.
//
// A dispatch that was never acknowledged never reached its worker: the attempt is
// given back, and the Run is ready at its due time. An acknowledged attempt is lost,
// and counts as a failed try. The try counts on the Step the last heartbeat named,
// when that Step has no final record yet. Otherwise it counts on the handler's own
// tries. The Step's own policy decides first, then the Task's, then the server default,
// field by field, as the heartbeat named it. When tries remain, the Run waits for its
// next attempt. When none remain, the Run fails with ATTEMPT_LOST, naming the Step when
// there is one.
func SweepLease(
	s Snapshot,
	now time.Time,
	reason QueuedReason,
	taskPolicy *RetryPolicy,
	cfg RetryConfig,
	unit float64,
) (Write, LostOutcome) {
	run := s.Run
	if run.Status != RunStatusRunning || run.LeaseExpiresAt.After(now) {
		return Write{Run: run}, LostNone
	}

	if run.AckedAt.IsZero() {
		run = giveBack(run, reason, now)
		return Write{Run: run}, LostGivenBack
	}

	if run.RunningStepID != "" {
		step := FindStep(s.Steps, run.RunningStepID)
		if step == nil || !step.Finished() {
			policy := ResolveRetry(run.RunningStepPolicy, taskPolicy, cfg)
			return lostStep(s, run, step, now, policy, cfg, unit)
		}
	}

	policy := ResolveRetry(nil, taskPolicy, cfg)
	attempt := run.HandlerAttemptsUsed + 1
	if policy.Exhausted(attempt) {
		run.Status = RunStatusFailed
		run.Failure = &Failure{Code: FailureCodeAttemptLost, Message: lostMessage, Attempts: run.Attempts}
		run.FinishedAt = now
		clearLease(&run)
		return Write{Run: run}, LostFinal
	}

	run.HandlerAttemptsUsed = attempt
	due := now.Add(DelayAfter(policy, attempt, 0, cfg.Jitter, unit))
	requeueForRetry(&run, due, "", EndKindNone)
	return Write{Run: run}, LostRetry
}

// leaseIsCurrent reports whether token is the lease of the running attempt. A lease past
// its expiry is still current: it stays valid until the sweep takes the attempt back, and
// the restart grace relies on a heartbeat renewing a lease that expired while the server
// was down. A caller that needs an unexpired lease checks the expiry itself, as AckRun does.
func leaseIsCurrent(run Run, token string) bool {
	return token != "" && run.Status == RunStatusRunning && run.LeaseToken == token
}

// giveBack queues a running Run again, takes its attempt back, and clears its lease
// and the Step it was running. Its start is kept. Its next attempt is cleared, so it
// is ready at its due time. A Run waiting for a worker starts a new spell of waiting now.
func giveBack(run Run, reason QueuedReason, now time.Time) Run {
	clearLease(&run)
	run.Status = RunStatusQueued
	run.QueuedReason = reason
	run.NextAttemptAt = time.Time{}
	if run.Attempts > 0 {
		run.Attempts--
	}

	if reason == QueuedReasonWaitingForWorker {
		run.WorkerWaitSince = now
	}

	return run
}

// clearLease removes the lease of the attempt from the Run, and the Step it was running.
func clearLease(run *Run) {
	run.LeaseToken = ""
	run.LeaseExpiresAt = time.Time{}
	run.InstanceID = ""
	run.AckedAt = time.Time{}
	run.RunningStepID = ""
	run.RunningStepAttempt = 0
	run.RunningStepPolicy = nil
}

// endKindOf is the kind of ended attempt that a report of this kind produces.
// A succeeded report never ends an attempt with a retry, so it has no kind here.
func endKindOf(kind FinishKind) EndKind {
	switch kind {
	case FinishReleased:
		return EndKindReleased
	case FinishFailed:
		return EndKindFailed
	default:
		return EndKindNone
	}
}

// requeueForRetry queues the Run for a retry that starts no earlier than next.
// The lease and the running Step are cleared. DueAt stays: it is when the Run was
// due, not when it retries. When endedToken is not empty, the attempt that ended
// with it is recorded with its kind and its retry time.
func requeueForRetry(run *Run, next time.Time, endedToken string, kind EndKind) {
	clearLease(run)
	run.Status = RunStatusQueued
	run.QueuedReason = QueuedReasonRetryBackoff
	run.NextAttemptAt = next
	run.WorkerWaitSince = time.Time{}
	run.EndedToken = endedToken
	run.EndedKind = kind
	run.EndedAt = time.Time{}
	if endedToken != "" {
		run.EndedAt = next
	}
}

// lostStep counts a lost attempt as a failed try of the Step the heartbeat named.
// The try number is the heartbeat's, or one more than the stored count when that is
// larger. A Step with no record gets its pending record here.
func lostStep(
	s Snapshot,
	run Run,
	existing *StepRecord,
	now time.Time,
	policy RetryPolicy,
	cfg RetryConfig,
	unit float64,
) (Write, LostOutcome) {
	stepID := run.RunningStepID
	stored := 0
	position := len(s.Steps) + 1
	if existing != nil {
		stored = existing.AttemptsUsed
		position = existing.Position
	}

	attempt := max(run.RunningStepAttempt, stored+1)
	rec := StepRecord{StepID: stepID, Position: position, AttemptsUsed: attempt}

	if policy.Exhausted(attempt) {
		rec.Status = StepStatusFailed
		rec.Failure = &Failure{Code: FailureCodeAttemptLost, Message: lostMessage, Attempts: attempt, StepID: stepID}
		run.Status = RunStatusFailed
		run.Failure = &Failure{Code: FailureCodeAttemptLost, Message: lostMessage, Attempts: run.Attempts, StepID: stepID}
		run.FinishedAt = now
		clearLease(&run)
		return Write{Run: run, Step: &rec}, LostFinal
	}

	due := now.Add(DelayAfter(policy, attempt, 0, cfg.Jitter, unit))
	rec.Status = StepStatusPending
	rec.RetryNotBefore = due
	requeueForRetry(&run, due, "", EndKindNone)
	return Write{Run: run, Step: &rec}, LostRetry
}
