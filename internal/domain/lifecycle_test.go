package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	t0 = time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	// testCfg retries with 1s, then 2s, up to 3 tries in total, with no jitter.
	testCfg = RetryConfig{
		MaxAttempts: 3,
		Delays:      []time.Duration{time.Second, 2 * time.Second},
		Jitter:      0,
	}
)

// runningRun is an attempt that holds its lease, the first one of its Run.
func runningRun(token string) Run {
	return Run{
		ID:             "run-1",
		WorkerID:       "demo",
		TaskID:         "hello",
		Status:         RunStatusRunning,
		Source:         RunSourceInvoke,
		Attempts:       1,
		LeaseToken:     token,
		LeaseExpiresAt: t0.Add(30 * time.Second),
		InstanceID:     "i-1",
		DueAt:          t0,
		CreatedAt:      t0,
		StartedAt:      t0,
	}
}

func TestAckRunKeepsFirstTimeAndNeedsCurrentLease(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}

	w, err := AckRun(s, "tok", t0)
	require.NoError(t, err, "ack")
	require.True(t, t0.Equal(w.Run.AckedAt), "acked at = %v, want %v", w.Run.AckedAt, t0)

	later := t0.Add(10 * time.Second)
	s.Run.AckedAt = t0
	w, err = AckRun(s, "tok", later)
	require.NoError(t, err, "repeated ack within the lease")
	require.True(t, t0.Equal(w.Run.AckedAt), "repeated ack within the lease must keep the first time, got %v", w.Run.AckedAt)

	_, err = AckRun(s, "other", t0)
	require.ErrorIs(t, err, ErrStaleLease, "ack with another token")
	_, err = AckRun(s, "", t0)
	require.ErrorIs(t, err, ErrStaleLease, "ack with empty token")
}

// An acknowledgement that arrives after the lease expired is refused, also before the sweep.
func TestAckRunAfterLeaseExpiryIsStale(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	expired := s.Run.LeaseExpiresAt
	_, err := AckRun(s, "tok", expired)
	require.ErrorIs(t, err, ErrStaleLease, "ack at the expiry")
	_, err = AckRun(s, "tok", expired.Add(time.Second))
	require.ErrorIs(t, err, ErrStaleLease, "ack after the expiry")
	_, err = AckRun(s, "tok", expired.Add(-time.Second))
	require.NoError(t, err, "ack just before the expiry")
}

// A lost Step counts its try under the Step's own policy, not the Task's.
func TestLostStepUsesTheStepsOwnPolicy(t *testing.T) {
	run := runningRun("tok")
	run.LeaseExpiresAt = t0.Add(-time.Second)
	run.AckedAt = t0
	run.RunningStepID = "charge"
	run.RunningStepAttempt = 1
	run.RunningStepPolicy = &RetryPolicy{MaxAttempts: 1}
	taskPolicy := &RetryPolicy{MaxAttempts: 4, Delays: []time.Duration{time.Second}}

	w, outcome := SweepLease(Snapshot{Run: run}, t0, QueuedReasonWaitingForWorker, taskPolicy, testCfg, 0.5)
	require.Equal(t, LostFinal, outcome, "the Step allows one try")
	require.Equal(t, RunStatusFailed, w.Run.Status, "run status")
	require.NotNil(t, w.Run.Failure)
	require.Equal(t, FailureCodeAttemptLost, w.Run.Failure.Code, "failure code")
	require.Equal(t, "charge", w.Run.Failure.StepID, "failure step")

	// Without the Step's own policy the Task's limit applies, so the try is retried.
	run.RunningStepPolicy = nil
	w, outcome = SweepLease(Snapshot{Run: run}, t0, QueuedReasonWaitingForWorker, taskPolicy, testCfg, 0.5)
	require.Equal(t, LostRetry, outcome, "the Task's limit allows a retry")
	require.NotNil(t, w.Step, "a pending record")
	require.Equal(t, StepStatusPending, w.Step.Status, "a pending record")
}

func TestGiveBackTakesAttemptBackAndKeepsDue(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	w := GiveBack(s, "tok", QueuedReasonWaitingForWorker, t0)

	require.Equal(t, RunStatusQueued, w.Run.Status, "status")
	require.Equal(t, QueuedReasonWaitingForWorker, w.Run.QueuedReason, "reason")
	require.Equal(t, 0, w.Run.Attempts, "attempt not given back")
	require.False(t, w.Run.StartedAt.IsZero(), "start lost")
	require.Empty(t, w.Run.LeaseToken, "lease not cleared")
	require.Empty(t, w.Run.InstanceID, "lease not cleared")
	require.True(t, w.Run.LeaseExpiresAt.IsZero(), "lease not cleared")
	require.True(t, t0.Equal(w.Run.DueAt), "due changed: %v", w.Run.DueAt)
	require.Empty(t, w.Run.EndedToken, "ended token changed")

	unchanged := GiveBack(s, "other", QueuedReasonWaitingForWorker, t0)
	require.Equal(t, RunStatusRunning, unchanged.Run.Status, "give back with a stale token must change nothing")
}

func TestRecordStepSucceededStoresRecord(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	w, ans, err := RecordStep(s, "tok", StepReport{StepID: "a", Attempt: 1, Succeeded: true, Output: []byte(`1`)},
		nil, testCfg, Limits{}, t0, 0.5)
	require.NoError(t, err, "record")
	require.NotNil(t, w.Step)
	require.Equal(t, StepStatusSucceeded, w.Step.Status, "step status")
	require.Equal(t, 1, w.Step.Position, "step position")
	require.Equal(t, 1, w.Step.AttemptsUsed, "step attempts")
	require.NotNil(t, ans.Record, "answer record")
	require.Nil(t, ans.Retry, "answer retry")
	require.Equal(t, RunStatusRunning, w.Run.Status, "a recorded Step must leave the Run running")
}

func TestRecordStepPlainErrorRetriesWithBackoff(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	w, ans, err := RecordStep(s, "tok",
		StepReport{StepID: "b", Attempt: 1, Error: AttemptError{Message: "boom"}},
		nil, testCfg, Limits{}, t0, 0.5)
	require.NoError(t, err, "record")
	require.NotNil(t, ans.Retry, "retry answer")
	require.True(t, t0.Add(time.Second).Equal(ans.Retry.NotBefore), "retry not before = %v", ans.Retry.NotBefore)
	require.Equal(t, 1, ans.Retry.AttemptsUsed, "retry attempts")
	require.NotNil(t, w.Step)
	require.Equal(t, StepStatusPending, w.Step.Status, "step status")
	require.Equal(t, 1, w.Step.AttemptsUsed, "step attempts")
	require.Equal(t, RunStatusQueued, w.Run.Status, "run status")
	require.Equal(t, QueuedReasonRetryBackoff, w.Run.QueuedReason, "run reason")
	require.True(t, t0.Add(time.Second).Equal(w.Run.NextAttemptAt), "next attempt = %v", w.Run.NextAttemptAt)
	require.Empty(t, w.Run.LeaseToken, "lease")
	require.True(t, t0.Equal(w.Run.DueAt), "due_at must never change on a retry: %v", w.Run.DueAt)
	require.Empty(t, w.Run.EndedToken, "a step retry must not set the ended token")
}

func TestRecordStepFinalCases(t *testing.T) {
	tests := []struct {
		name     string
		attempt  int
		err      AttemptError
		policy   *RetryPolicy
		wantCode FailureCode
	}{
		{"permanent", 1, AttemptError{Message: "no", Permanent: true}, nil, FailureCodeError},
		{"panic is never retried", 1, AttemptError{Message: "p", Code: FailureCodePanic}, nil, FailureCodePanic},
		{"output not encodable is never retried", 1,
			AttemptError{Message: "o", Code: FailureCodeOutputNotEncodable}, nil, FailureCodeOutputNotEncodable},
		{"limit reached", 3, AttemptError{Message: "last"}, nil, FailureCodeError},
		{"step policy limit of one", 1, AttemptError{Message: "x"}, &RetryPolicy{MaxAttempts: 1}, FailureCodeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Snapshot{Run: runningRun("tok"), Steps: stepsWithPending(tt.attempt - 1)}
			rep := StepReport{StepID: "b", Attempt: tt.attempt, Error: tt.err}
			if tt.policy != nil {
				rep.StepPolicy = tt.policy
			}

			w, ans, err := RecordStep(s, "tok", rep, nil, testCfg, Limits{}, t0, 0.5)
			require.NoError(t, err, "record")
			require.Nil(t, ans.Retry, "expected a final record")
			require.NotNil(t, ans.Record, "expected a final record")
			require.Equal(t, StepStatusFailed, ans.Record.Status, "record status")
			require.NotNil(t, ans.Record.Failure, "record failure")
			require.Equal(t, tt.wantCode, ans.Record.Failure.Code, "failure code")
			require.Equal(t, tt.attempt, ans.Record.Failure.Attempts, "failure attempts")
			require.Equal(t, "b", ans.Record.Failure.StepID, "failure step")
			require.Equal(t, RunStatusRunning, w.Run.Status, "a final Step must leave the Run running")
		})
	}
}

func TestRecordStepUnknownCodeIsErrorAndRetryAfterIsExact(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	_, ans, err := RecordStep(s, "tok", StepReport{
		StepID:  "b",
		Attempt: 1,
		Error:   AttemptError{Message: "rate limited", Code: FailureCodeStepFailed, RetryAfter: 42 * time.Second},
	}, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "record")
	require.NotNil(t, ans.Retry, "retry answer")
	require.True(t, t0.Add(42*time.Second).Equal(ans.Retry.NotBefore), "retry answer = %+v, want exactly 42s after now", ans.Retry)
}

func TestRecordStepRepeatGetsStoredAnswer(t *testing.T) {
	// The first try of b was a retry. The Run has since been queued, so its lease is gone.
	run := runningRun("tok")
	run.Status = RunStatusQueued
	run.LeaseToken = ""
	run.QueuedReason = QueuedReasonRetryBackoff
	run.DueAt = t0.Add(time.Second)
	pending := StepRecord{
		StepID: "b", Position: 1, Status: StepStatusPending, AttemptsUsed: 1,
		RetryNotBefore: t0.Add(time.Second),
	}

	s := Snapshot{Run: run, Steps: []StepRecord{pending}}

	rep := StepReport{StepID: "b", Attempt: 1, Error: AttemptError{Message: "boom"}}
	w, ans, err := RecordStep(s, "tok", rep, nil, testCfg, Limits{}, t0.Add(time.Minute), 0.5)
	require.NoError(t, err, "repeat must not fail")
	require.NotNil(t, ans.Retry, "repeat answer")
	require.True(t, t0.Add(time.Second).Equal(ans.Retry.NotBefore), "repeat not before = %v", ans.Retry.NotBefore)
	require.Equal(t, 1, ans.Retry.AttemptsUsed, "repeat attempts")
	require.Nil(t, w.Step, "a repeat must write nothing")
	require.Equal(t, RunStatusQueued, w.Run.Status, "a repeat must write nothing")

	// A repeat of a final record answers the record.
	final := StepRecord{StepID: "a", Position: 1, Status: StepStatusSucceeded, AttemptsUsed: 1, Output: []byte(`7`)}
	s2 := Snapshot{Run: runningRun("tok"), Steps: []StepRecord{final}}
	_, ans2, err := RecordStep(s2, "tok", StepReport{StepID: "a", Attempt: 1, Succeeded: true}, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "repeat of a final record")
	require.NotNil(t, ans2.Record, "repeat of a final record")
	require.Equal(t, "7", string(ans2.Record.Output), "repeat of a final record")
}

func TestRecordStepRejectsStaleAndOutOfOrder(t *testing.T) {
	run := runningRun("tok")
	pending := StepRecord{StepID: "b", Position: 1, Status: StepStatusPending, AttemptsUsed: 2,
		RetryNotBefore: t0}

	s := Snapshot{Run: run, Steps: []StepRecord{pending}}

	// An older try than the stored one is stale.
	_, _, err := RecordStep(s, "tok", StepReport{StepID: "b", Attempt: 1}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "older try")

	// Skipping a try is out of order.
	_, _, err = RecordStep(s, "tok", StepReport{StepID: "b", Attempt: 4}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStepOrder, "skipped try")

	// The next try needs the current lease.
	_, _, err = RecordStep(s, "old", StepReport{StepID: "b", Attempt: 3}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "next try with old token")
	_, _, err = RecordStep(s, "tok", StepReport{StepID: "b", Attempt: 3}, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "next try with current token")

	// A Step that is final takes no more tries.
	done := StepRecord{StepID: "a", Position: 1, Status: StepStatusFailed, AttemptsUsed: 1,
		Failure: &Failure{Code: FailureCodeError, Attempts: 1, StepID: "a"}}

	s2 := Snapshot{Run: run, Steps: []StepRecord{done}}
	_, _, err = RecordStep(s2, "tok", StepReport{StepID: "a", Attempt: 2, Succeeded: true}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStepOrder, "try after final")
}

func TestRecordStepAfterRequeueIsStale(t *testing.T) {
	// A Run queued by a dispatch give back has no lease. A new try must not get through.
	run := runningRun("tok")
	run.Status = RunStatusQueued
	run.LeaseToken = ""
	s := Snapshot{Run: run}
	_, _, err := RecordStep(s, "tok", StepReport{StepID: "a", Attempt: 1, Succeeded: true}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease)
}

func TestFinishSucceededAndRepeat(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	w, ans, err := FinishRun(s, FinishInput{Token: "tok", Kind: FinishSucceeded, Output: []byte(`{"ok":1}`)},
		nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.Equal(t, RunStatusSucceeded, ans.Run.Status, "answer status")
	require.Equal(t, `{"ok":1}`, string(ans.Run.Output), "answer output")
	require.Nil(t, ans.Retry, "answer retry")
	require.True(t, t0.Equal(w.Run.FinishedAt), "finished at = %v", w.Run.FinishedAt)

	// The same token again answers the same finished Run.
	again := Snapshot{Run: w.Run}
	_, ans2, err := FinishRun(again, FinishInput{Token: "tok", Kind: FinishSucceeded, Output: []byte(`{"ok":1}`)},
		nil, testCfg, Limits{}, t0.Add(time.Minute), 0)
	require.NoError(t, err, "repeat finish")
	require.Equal(t, RunStatusSucceeded, ans2.Run.Status, "repeat finish status")
	require.Equal(t, `{"ok":1}`, string(ans2.Run.Output), "repeat finish output")
}

func TestFinishWithStaleTokenIsStale(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	_, _, err := FinishRun(s, FinishInput{Token: "other", Kind: FinishSucceeded}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease)

	_, _, err = FinishRun(s, FinishInput{Token: "", Kind: FinishSucceeded}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "empty token")
}

func TestFinishReleasedGivesRetryAndRepeatsIt(t *testing.T) {
	run := runningRun("tok")
	run.Attempts = 2
	s := Snapshot{Run: run}
	in := FinishInput{Token: "tok", Kind: FinishReleased, ReleaseReason: QueuedReasonWaitingForWorker}

	w, ans, err := FinishRun(s, in, nil, testCfg, Limits{}, t0.Add(time.Second), 0)
	require.NoError(t, err, "release")
	require.NotNil(t, ans.Retry, "released must answer retry")
	require.Empty(t, ans.Run.ID, "released must answer retry")
	require.True(t, t0.Add(time.Second).Equal(ans.Retry.NotBefore), "not_before = %v, want now", ans.Retry.NotBefore)
	require.Equal(t, RunStatusQueued, w.Run.Status, "run status after release")
	require.Equal(t, 1, w.Run.Attempts, "run attempts after release")
	require.Equal(t, QueuedReasonWaitingForWorker, w.Run.QueuedReason, "run reason after release")
	require.Equal(t, "tok", w.Run.EndedToken, "ended token")

	// A lost answer is resent with the same token: the same retry comes back.
	_, ans2, err := FinishRun(Snapshot{Run: w.Run}, in, nil, testCfg, Limits{}, t0.Add(time.Minute), 0)
	require.NoError(t, err, "repeat release")
	require.NotNil(t, ans2.Retry, "repeat release")
	require.True(t, ans.Retry.NotBefore.Equal(ans2.Retry.NotBefore), "repeat release not before = %v", ans2.Retry.NotBefore)
}

func TestFinishFailedWithoutStepRetriesUnderTaskLimit(t *testing.T) {
	run := runningRun("tok")
	s := Snapshot{Run: run}
	failure := FinishInput{Token: "tok", Kind: FinishFailed, Error: AttemptError{Message: "handler broke"}}

	w, ans, err := FinishRun(s, failure, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.NotNil(t, ans.Retry, "retry")
	require.Equal(t, 1, ans.Retry.AttemptsUsed, "retry attempts")
	require.True(t, t0.Add(time.Second).Equal(ans.Retry.NotBefore), "retry not before = %v", ans.Retry.NotBefore)
	require.Equal(t, RunStatusQueued, w.Run.Status, "run status")
	require.Equal(t, 1, w.Run.HandlerAttemptsUsed, "handler attempts")
	require.Equal(t, "tok", w.Run.EndedToken, "ended token")

	// The resent report gets the same answer.
	_, ans2, err := FinishRun(Snapshot{Run: w.Run}, failure, nil, testCfg, Limits{}, t0.Add(time.Minute), 0)
	require.NoError(t, err, "repeat")
	require.NotNil(t, ans2.Retry, "repeat")
	require.Equal(t, 1, ans2.Retry.AttemptsUsed, "repeat attempts")

	// After the retry the Run runs again under a new token; the old one is stale.
	next := runningRun("tok2")
	next.HandlerAttemptsUsed = 1
	failure2 := failure
	failure2.Token = "tok2"
	_, _, err = FinishRun(Snapshot{Run: next}, failure2, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "second attempt with its own token")

	queued := w.Run
	_, _, err = FinishRun(Snapshot{Run: queued}, FinishInput{Token: "old", Kind: FinishSucceeded}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "old token after requeue")
}

func TestFinishFailedWithoutStepAtLimitFailsRun(t *testing.T) {
	run := runningRun("tok")
	run.HandlerAttemptsUsed = 2
	run.Attempts = 3
	s := Snapshot{Run: run}
	cfg := RetryConfig{MaxAttempts: 3, Delays: []time.Duration{time.Second}}

	w, ans, err := FinishRun(s, FinishInput{Token: "tok", Kind: FinishFailed,
		Error: AttemptError{Message: "still broken"}}, nil, cfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.Nil(t, ans.Retry, "answer retry")
	require.Equal(t, RunStatusFailed, ans.Run.Status, "answer status")
	require.NotNil(t, w.Run.Failure, "failure")
	require.Equal(t, FailureCodeError, w.Run.Failure.Code, "failure code")
	require.Equal(t, 3, w.Run.Failure.Attempts, "failure attempts")
}

func TestFinishFailedPermanentAndPanicAreFinal(t *testing.T) {
	for _, in := range []FinishInput{
		{Token: "tok", Kind: FinishFailed, Error: AttemptError{Message: "no", Permanent: true}},
		{Token: "tok", Kind: FinishFailed, Error: AttemptError{Message: "p", Code: FailureCodePanic}},
	} {
		_, ans, err := FinishRun(Snapshot{Run: runningRun("tok")}, in, nil, testCfg, Limits{}, t0, 0)
		require.NoError(t, err, "input %+v", in.Error)
		require.Nil(t, ans.Retry, "input %+v", in.Error)
		require.Equal(t, RunStatusFailed, ans.Run.Status, "input %+v", in.Error)
	}
}

func TestFinishFailedWithStepIDIsStepFailed(t *testing.T) {
	failedStep := StepRecord{StepID: "charge", Position: 1, Status: StepStatusFailed, AttemptsUsed: 3,
		Failure: &Failure{Code: FailureCodeError, Message: "declined", Attempts: 3, StepID: "charge"}}

	s := Snapshot{Run: runningRun("tok"), Steps: []StepRecord{failedStep}}

	w, ans, err := FinishRun(s, FinishInput{Token: "tok", Kind: FinishFailed, StepID: "charge",
		Error: AttemptError{Message: "declined"}}, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.Nil(t, ans.Retry, "a Step's final error must fail the Run without retry")
	require.Equal(t, RunStatusFailed, w.Run.Status, "a Step's final error must fail the Run without retry")

	f := w.Run.Failure
	// The Failure counts the Run's attempts. The Step keeps its own count on its record.
	require.NotNil(t, f, "failure")
	require.Equal(t, FailureCodeStepFailed, f.Code, "failure code")
	require.Equal(t, "charge", f.StepID, "failure step")
	require.Equal(t, 1, f.Attempts, "failure attempts")
	require.Equal(t, "declined", f.Message, "failure message")
}

func TestFinishFailedWithStepIDThatIsNotFinalIsOutsideStep(t *testing.T) {
	pending := StepRecord{StepID: "charge", Position: 1, Status: StepStatusPending, AttemptsUsed: 1,
		RetryNotBefore: t0}

	s := Snapshot{Run: runningRun("tok"), Steps: []StepRecord{pending}}

	_, ans, err := FinishRun(s, FinishInput{Token: "tok", Kind: FinishFailed, StepID: "charge",
		Error: AttemptError{Message: "x"}}, nil, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.NotNil(t, ans.Retry, "a step without a final record is a failure outside any Step, which retries")
}

func TestFinishTaskPolicyLimitApplies(t *testing.T) {
	// The Task's own limit of one try makes the first handler failure final.
	taskPolicy := &RetryPolicy{MaxAttempts: 1}
	_, ans, err := FinishRun(Snapshot{Run: runningRun("tok")},
		FinishInput{Token: "tok", Kind: FinishFailed, Error: AttemptError{Message: "x"}},
		taskPolicy, testCfg, Limits{}, t0, 0)
	require.NoError(t, err, "finish")
	require.Nil(t, ans.Retry, "the Task's limit of one try makes the failure final")
	require.Equal(t, RunStatusFailed, ans.Run.Status, "answer status")
}

func stepsWithPending(attempts int) []StepRecord {
	if attempts <= 0 {
		return nil
	}

	return []StepRecord{{StepID: "b", Position: 1, Status: StepStatusPending, AttemptsUsed: attempts,
		RetryNotBefore: t0}}
}

func TestReleasedRepeatSurvivesDispatchAgainButOtherReportsAreStale(t *testing.T) {
	// The attempt tok-1 was released at t0. The Run has been dispatched again as tok-2.
	run := runningRun("tok-2")
	run.Attempts = 1
	run.EndedToken = "tok-1"
	run.EndedKind = EndKindReleased
	run.EndedAt = t0
	s := Snapshot{Run: run}

	_, ans, err := FinishRun(s, FinishInput{Token: "tok-1", Kind: FinishReleased}, nil, testCfg, Limits{}, t0.Add(time.Minute), 0)
	require.NoError(t, err, "resent release after dispatch again")
	require.NotNil(t, ans.Retry, "resent release after dispatch again")
	require.True(t, t0.Equal(ans.Retry.NotBefore), "resent release must answer the stored retry time, got %v", ans.Retry.NotBefore)

	_, _, err = FinishRun(s, FinishInput{Token: "tok-1", Kind: FinishSucceeded}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "succeeded with the released token")
	_, _, err = FinishRun(s, FinishInput{Token: "tok-1", Kind: FinishFailed, Error: AttemptError{Message: "x"}},
		nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "failed with the released token")
}

func TestFailedRetryRepeatIsNotAnsweredForReleaseReports(t *testing.T) {
	run := runningRun("tok-2")
	run.EndedToken = "tok-1"
	run.EndedKind = EndKindFailed
	s := Snapshot{Run: run}
	_, _, err := FinishRun(s, FinishInput{Token: "tok-1", Kind: FinishReleased}, nil, testCfg, Limits{}, t0, 0)
	require.ErrorIs(t, err, ErrStaleLease, "released with a failed attempt's token")
}

func TestGiveBackClearsNextAttemptButKeepsDueAt(t *testing.T) {
	run := runningRun("tok")
	run.NextAttemptAt = t0.Add(time.Minute)
	w := GiveBack(Snapshot{Run: run}, "tok", QueuedReasonWaitingForWorker, t0)
	require.True(t, w.Run.NextAttemptAt.IsZero(), "next attempt = %v, want cleared", w.Run.NextAttemptAt)
	require.True(t, t0.Equal(w.Run.DueAt), "due_at = %v, must not change", w.Run.DueAt)
}

// A release keeps the Run's start: it was dispatched, so it can never expire by its start deadline.
func TestReleaseKeepsStartedAt(t *testing.T) {
	run := runningRun("tok")
	w, _, err := FinishRun(Snapshot{Run: run}, FinishInput{Token: "tok", Kind: FinishReleased,
		ReleaseReason: QueuedReasonWaitingForWorker}, nil, testCfg, Limits{}, t0.Add(time.Second), 0)
	require.NoError(t, err, "release")
	require.False(t, w.Run.StartedAt.IsZero(), "a released Run keeps its start")

	// A released Run has no next attempt, so it is ready at its due time and keeps its place.
	require.True(t, w.Run.NextAttemptAt.IsZero(), "next attempt = %v, want none", w.Run.NextAttemptAt)
	require.True(t, t0.Equal(w.Run.DueAt), "due = %v, want %v", w.Run.DueAt, t0)
	require.True(t, t0.Add(time.Second).Equal(w.Run.EndedAt), "ended at = %v", w.Run.EndedAt)
}

// lostRun is a running attempt whose lease has expired at t0+10s, acknowledged unless ackless.
func lostRun(ackless bool) Run {
	run := runningRun("tok")
	run.LeaseExpiresAt = t0.Add(5 * time.Second)
	run.Attempts = 2
	if ackless {
		run.AckedAt = time.Time{}
	} else {
		run.AckedAt = t0
	}

	return run
}

var lostNow = t0.Add(10 * time.Second)

func TestSweepLeaseTable(t *testing.T) {
	// Policy of three tries; delays of 1s, then 2s; no jitter.
	cfg := RetryConfig{MaxAttempts: 3, Delays: []time.Duration{time.Second, 2 * time.Second}}

	tests := []struct {
		name       string
		run        Run
		steps      []StepRecord
		task       *RetryPolicy
		wantOut    LostOutcome
		wantStatus RunStatus
		check      func(t *testing.T, w Write)
	}{
		{
			name:       "never acked: given back, no try used, ready at due",
			run:        func() Run { r := lostRun(true); r.RunningStepID = "b"; r.RunningStepAttempt = 2; return r }(),
			wantOut:    LostGivenBack,
			wantStatus: RunStatusQueued,
			check: func(t *testing.T, w Write) {
				assert.Equal(t, 1, w.Run.Attempts, "give back: attempts")
				assert.Equal(t, 0, w.Run.HandlerAttemptsUsed, "give back: handler attempts")
				assert.True(t, w.Run.NextAttemptAt.IsZero(), "give back: next attempt = %v", w.Run.NextAttemptAt)
				assert.True(t, t0.Equal(w.Run.DueAt), "give back: due = %v", w.Run.DueAt)
				assert.Empty(t, w.Run.RunningStepID, "running step is cleared")
				assert.Empty(t, w.Run.LeaseToken, "lease is cleared")
				assert.Nil(t, w.Step, "a give back records no Step")
			},
		},
		{
			name: "step named and open: counts the heartbeat's try and waits",
			run: func() Run {
				r := lostRun(false)
				r.RunningStepID = "b"
				r.RunningStepAttempt = 1
				return r
			}(),
			wantOut:    LostRetry,
			wantStatus: RunStatusQueued,
			check: func(t *testing.T, w Write) {
				require.NotNil(t, w.Step)
				require.Equal(t, "b", w.Step.StepID, "step")
				require.Equal(t, StepStatusPending, w.Step.Status, "step status")
				require.Equal(t, 1, w.Step.AttemptsUsed, "step attempts")
				assert.True(t, lostNow.Add(time.Second).Equal(w.Step.RetryNotBefore), "step retry at %v", w.Step.RetryNotBefore)
				assert.True(t, lostNow.Add(time.Second).Equal(w.Run.NextAttemptAt), "run next = %v", w.Run.NextAttemptAt)
				assert.Equal(t, QueuedReasonRetryBackoff, w.Run.QueuedReason, "run reason")
				assert.True(t, t0.Equal(w.Run.DueAt), "due = %v", w.Run.DueAt)
				assert.Equal(t, 0, w.Run.HandlerAttemptsUsed, "handler attempts")
			},
		},
		{
			name: "step named, stored count larger than the heartbeat's: one more than stored",
			run: func() Run {
				r := lostRun(false)
				r.RunningStepID = "b"
				r.RunningStepAttempt = 1
				return r
			}(),
			steps: []StepRecord{{StepID: "b", Position: 1, Status: StepStatusPending, AttemptsUsed: 1,
				RetryNotBefore: t0}},
			wantOut:    LostRetry,
			wantStatus: RunStatusQueued,
			check: func(t *testing.T, w Write) {
				require.NotNil(t, w.Step)
				require.Equal(t, 2, w.Step.AttemptsUsed, "attempts")
				require.Equal(t, 1, w.Step.Position, "position")
			},
		},
		{
			name: "step named, last try: the Step fails with ATTEMPT_LOST and so does the Run",
			run: func() Run {
				r := lostRun(false)
				r.RunningStepID = "b"
				r.RunningStepAttempt = 3
				return r
			}(),
			steps: []StepRecord{{StepID: "b", Position: 2, Status: StepStatusPending, AttemptsUsed: 2,
				RetryNotBefore: t0}},
			wantOut:    LostFinal,
			wantStatus: RunStatusFailed,
			check: func(t *testing.T, w Write) {
				require.NotNil(t, w.Step)
				require.Equal(t, StepStatusFailed, w.Step.Status, "step status")
				require.NotNil(t, w.Step.Failure, "step failure")
				require.Equal(t, FailureCodeAttemptLost, w.Step.Failure.Code, "step failure code")
				require.Equal(t, 3, w.Step.Failure.Attempts, "step failure attempts")

				f := w.Run.Failure
				require.NotNil(t, f, "run failure")
				assert.Equal(t, FailureCodeAttemptLost, f.Code, "run failure code")
				assert.Equal(t, "b", f.StepID, "run failure step")
				assert.Equal(t, 2, f.Attempts, "run failure attempts: the run's attempts")
				assert.Empty(t, w.Run.RunningStepID, "lease not cleared on final")
				assert.Empty(t, w.Run.LeaseToken, "lease not cleared on final")
			},
		},
		{
			name: "step named but already final: the handler's tries count instead",
			run: func() Run {
				r := lostRun(false)
				r.RunningStepID = "b"
				r.RunningStepAttempt = 2
				return r
			}(),
			steps: []StepRecord{{StepID: "b", Position: 1, Status: StepStatusSucceeded, AttemptsUsed: 1,
				Output: []byte(`1`)}},
			wantOut:    LostRetry,
			wantStatus: RunStatusQueued,
			check: func(t *testing.T, w Write) {
				assert.Nil(t, w.Step, "a final Step must not change")
				assert.Equal(t, 1, w.Run.HandlerAttemptsUsed, "handler count")
				assert.True(t, lostNow.Add(time.Second).Equal(w.Run.NextAttemptAt), "next = %v", w.Run.NextAttemptAt)
			},
		},
		{
			name:       "no step named: handler tries count, and the wait",
			run:        func() Run { r := lostRun(false); r.HandlerAttemptsUsed = 1; return r }(),
			wantOut:    LostRetry,
			wantStatus: RunStatusQueued,
			check: func(t *testing.T, w Write) {
				assert.Equal(t, 2, w.Run.HandlerAttemptsUsed, "handler count")
				assert.True(t, lostNow.Add(2*time.Second).Equal(w.Run.NextAttemptAt), "next = %v, want 2s", w.Run.NextAttemptAt)
				assert.Nil(t, w.Step, "no Step was named, so no Step changes")
			},
		},
		{
			name:       "last try with no step: ATTEMPT_LOST on the Run",
			run:        func() Run { r := lostRun(false); r.HandlerAttemptsUsed = 2; return r }(),
			wantOut:    LostFinal,
			wantStatus: RunStatusFailed,
			check: func(t *testing.T, w Write) {
				f := w.Run.Failure
				require.NotNil(t, f, "failure")
				assert.Equal(t, FailureCodeAttemptLost, f.Code, "failure code")
				assert.Empty(t, f.StepID, "failure step")
				assert.Equal(t, 2, f.Attempts, "failure attempts")
			},
		},
		{
			name:       "the Task's limit of one try makes the first loss final",
			run:        lostRun(false),
			task:       &RetryPolicy{MaxAttempts: 1},
			wantOut:    LostFinal,
			wantStatus: RunStatusFailed,
		},
		{
			name:       "lease not expired: nothing changes",
			run:        func() Run { r := lostRun(false); r.LeaseExpiresAt = lostNow.Add(time.Second); return r }(),
			wantOut:    LostNone,
			wantStatus: RunStatusRunning,
		},
		{
			name:       "not running: nothing changes",
			run:        func() Run { r := lostRun(false); r.Status = RunStatusSucceeded; return r }(),
			wantOut:    LostNone,
			wantStatus: RunStatusSucceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Snapshot{Run: tt.run, Steps: tt.steps}
			w, out := SweepLease(s, lostNow, QueuedReasonWaitingForWorker, tt.task, cfg, 0.5)
			require.Equal(t, tt.wantOut, out, "outcome")
			require.Equal(t, tt.wantStatus, w.Run.Status, "status")
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

func TestRecordStepOverLimitStoresFailedStep(t *testing.T) {
	s := Snapshot{Run: runningRun("tok")}
	w, ans, err := RecordStep(s, "tok", StepReport{StepID: "a", Attempt: 1, Succeeded: true,
		Output: []byte("0123456789")}, nil, testCfg, Limits{StepOutput: 4}, t0, 0)
	require.NoError(t, err, "record")
	require.NotNil(t, ans.Record, "answer record")
	require.Equal(t, StepStatusFailed, ans.Record.Status, "answer record status")
	require.NotNil(t, ans.Record.Failure, "answer record failure")
	require.Equal(t, FailureCodeOutputTooLarge, ans.Record.Failure.Code, "answer record failure code")
	require.Equal(t, RunStatusRunning, w.Run.Status, "an over-limit output leaves the Run running")
}

func TestFinishSucceededOverRunLimitFailsRun(t *testing.T) {
	w, ans, err := FinishRun(Snapshot{Run: runningRun("tok")},
		FinishInput{Token: "tok", Kind: FinishSucceeded, Output: []byte("0123456789")},
		nil, testCfg, Limits{RunOutput: 4}, t0, 0)
	require.NoError(t, err, "finish")
	require.Equal(t, RunStatusFailed, ans.Run.Status, "answer status")
	require.NotNil(t, ans.Run.Failure, "answer failure")
	require.Equal(t, FailureCodeOutputTooLarge, ans.Run.Failure.Code, "answer failure code")
	require.Nil(t, ans.Run.Output, "an over-limit run output is not stored")
	require.Equal(t, RunStatusFailed, w.Run.Status, "an over-limit run output is not stored")
}
