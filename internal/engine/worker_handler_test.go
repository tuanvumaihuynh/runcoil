package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

func validRegisterRequest() *runcoilv1.RegisterRequest {
	return &runcoilv1.RegisterRequest{
		WorkerId:              "demo",
		InstanceId:            "i-1",
		MaxConcurrentAttempts: 2,
		ProtocolVersion:       1,
		Tasks: []*runcoilv1.TaskDefinition{
			{TaskId: "hello"},
			{
				TaskId:        "nightly",
				Schedule:      &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(time.Minute)}},
				Retry:         &runcoilv1.RetryPolicy{MaxAttempts: 3, Delays: []*durationpb.Duration{durationpb.New(time.Second)}},
				StartDeadline: durationpb.New(5 * time.Minute),
			},
		},
	}
}

func TestRegistrationFromProtoAccepts(t *testing.T) {
	reg, err := registrationFromProto(validRegisterRequest(), testNow)
	require.NoError(t, err, "registrationFromProto")
	assert.Equal(t, "demo", reg.WorkerID, "registration")
	assert.Equal(t, "i-1", reg.InstanceID, "registration")
	assert.Equal(t, 2, reg.Slots, "registration")

	require.Len(t, reg.Tasks, 2, "tasks")
	require.Equal(t, domain.TaskRef{WorkerID: "demo", TaskID: "hello"}, reg.Tasks[0].Ref, "first task ref")
	assert.Nil(t, reg.Tasks[0].Schedule, "unset fields must stay unset")
	assert.Nil(t, reg.Tasks[0].Retry, "unset fields must stay unset")
	assert.Zero(t, reg.Tasks[0].StartDeadline, "unset fields must stay unset")

	nightly := reg.Tasks[1]
	require.NotNil(t, nightly.Schedule, "schedule")
	require.Equal(t, domain.ScheduleKindEvery, nightly.Schedule.Kind, "schedule")
	require.Equal(t, time.Minute, nightly.Schedule.Every, "schedule")
	require.NotNil(t, nightly.Retry, "retry")
	require.Equal(t, 3, nightly.Retry.MaxAttempts, "retry")
	require.Len(t, nightly.Retry.Delays, 1, "retry")
	assert.Equal(t, 5*time.Minute, nightly.StartDeadline, "start deadline")
}

func TestRegistrationAcceptsValidSchedulesAndPolicies(t *testing.T) {
	cases := map[string]*runcoilv1.Schedule{
		"cron":  {Kind: &runcoilv1.Schedule_Cron{Cron: "*/5 * * * *"}},
		"every": {Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(time.Second)}},
		"daily": {Kind: &runcoilv1.Schedule_Daily{Daily: &runcoilv1.DailyTimes{
			At: []*runcoilv1.TimeOfDay{{Hour: 0, Minute: 0}, {Hour: 23, Minute: 59}},
		}}},
	}

	for name, sched := range cases {
		t.Run(name, func(t *testing.T) {
			req := validRegisterRequest()
			req.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "t", Schedule: sched}}
			_, err := registrationFromProto(req, testNow)
			require.NoError(t, err, "valid %s schedule rejected", name)
		})
	}
}

func TestRegistrationRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*runcoilv1.RegisterRequest)
		wantCode apierr.Code
		wantKind apierr.Kind
		wantTask string
	}{
		{
			name:     "protocol version 2",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.ProtocolVersion = 2 },
			wantCode: apierr.CodeIncompatibleProtocol,
			wantKind: apierr.KindFailedPrecondition,
		},
		{
			name:     "protocol version missing",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.ProtocolVersion = 0 },
			wantCode: apierr.CodeIncompatibleProtocol,
			wantKind: apierr.KindFailedPrecondition,
		},
		{
			name:     "empty worker_id",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.WorkerId = "" },
			wantCode: apierr.CodeInvalidRequest,
			wantKind: apierr.KindInvalid,
		},
		{
			name:     "empty instance",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.InstanceId = "" },
			wantCode: apierr.CodeInvalidRequest,
			wantKind: apierr.KindInvalid,
		},
		{
			name:     "zero concurrent attempts",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.MaxConcurrentAttempts = 0 },
			wantCode: apierr.CodeInvalidRequest,
			wantKind: apierr.KindInvalid,
		},
		{
			name:     "no tasks",
			mutate:   func(r *runcoilv1.RegisterRequest) { r.Tasks = nil },
			wantCode: apierr.CodeInvalidRequest,
			wantKind: apierr.KindInvalid,
		},
		{
			name: "empty task id names the task",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = append(r.Tasks, &runcoilv1.TaskDefinition{TaskId: ""})
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "",
		},
		{
			name: "duplicate task id names the task",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = append(r.Tasks, &runcoilv1.TaskDefinition{TaskId: "hello"})
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "hello",
		},
		{
			name: "cron that does not parse",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "bad-cron",
					Schedule: &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Cron{Cron: "every minute please"}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "bad-cron",
		},
		{
			name: "cron with a seconds field is not standard",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "six",
					Schedule: &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Cron{Cron: "0 */5 * * * *"}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "six",
		},
		{
			name: "every of zero",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "zero-every",
					Schedule: &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(0)}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "zero-every",
		},
		{
			name: "schedule with no kind",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "nokind", Schedule: &runcoilv1.Schedule{}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "nokind",
		},
		{
			name: "daily with no time",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "empty-daily",
					Schedule: &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Daily{Daily: &runcoilv1.DailyTimes{}}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "empty-daily",
		},
		{
			name: "daily hour 24",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "h24", Schedule: &runcoilv1.Schedule{
					Kind: &runcoilv1.Schedule_Daily{Daily: &runcoilv1.DailyTimes{
						At: []*runcoilv1.TimeOfDay{{Hour: 24}}}}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "h24",
		},
		{
			name: "daily minute 60",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "m60", Schedule: &runcoilv1.Schedule{
					Kind: &runcoilv1.Schedule_Daily{Daily: &runcoilv1.DailyTimes{
						At: []*runcoilv1.TimeOfDay{{Hour: 1, Minute: 60}}}}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "m60",
		},
		{
			name: "retry delay of zero",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "zero-delay",
					Retry: &runcoilv1.RetryPolicy{MaxAttempts: 2, Delays: []*durationpb.Duration{durationpb.New(0)}}}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "zero-delay",
		},
		{
			name: "start deadline of zero when set",
			mutate: func(r *runcoilv1.RegisterRequest) {
				r.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "zero-deadline",
					StartDeadline: durationpb.New(0)}}
			},
			wantCode: apierr.CodeInvalidRegistration,
			wantKind: apierr.KindInvalid,
			wantTask: "zero-deadline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRegisterRequest()
			tt.mutate(req)

			reg, err := registrationFromProto(req, testNow)
			require.Error(t, err, "registrationFromProto accepted the request: %+v", reg)

			var apiErr *apierr.Error
			require.ErrorAs(t, err, &apiErr, "error is %T, want *apierr.Error", err)
			assert.Equal(t, tt.wantCode, apiErr.Code, "code")
			assert.Equal(t, tt.wantKind, apiErr.Kind, "kind")
			if tt.wantCode == apierr.CodeInvalidRegistration {
				assert.Equal(t, tt.wantTask, apiErr.Details["task_id"], "task_id detail")
			}
		})
	}
}

// A bad Task anywhere in the request rejects the whole request, so nothing is registered.
func TestRegistrationIsAllOrNothing(t *testing.T) {
	req := validRegisterRequest()
	req.Tasks = append(req.Tasks, &runcoilv1.TaskDefinition{
		TaskId:   "broken",
		Schedule: &runcoilv1.Schedule{Kind: &runcoilv1.Schedule_Cron{Cron: "nope"}},
	})

	reg, err := registrationFromProto(req, testNow)
	require.Error(t, err, "request with a broken Task was accepted: %+v", reg)
	require.Nil(t, reg.Tasks, "no Task may be returned on error")
}

func TestRegistrationScheduleRules(t *testing.T) {
	ok := map[string]*runcoilv1.Schedule{
		"every of one second": {Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(time.Second)}},
		"every a minute":      {Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(time.Minute)}},
		"cron every minute":   {Kind: &runcoilv1.Schedule_Cron{Cron: "* * * * *"}},
	}

	for name, sched := range ok {
		req := validRegisterRequest()
		req.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "t", Schedule: sched}}
		_, err := registrationFromProto(req, testNow)
		assert.NoError(t, err, "%s rejected", name)
	}

	bad := map[string]*runcoilv1.Schedule{
		"every below one second": {Kind: &runcoilv1.Schedule_Every{Every: durationpb.New(500 * time.Millisecond)}},
		"cron that never fires":  {Kind: &runcoilv1.Schedule_Cron{Cron: "0 0 30 2 *"}},
		"cron with a descriptor": {Kind: &runcoilv1.Schedule_Cron{Cron: "@daily"}},
	}

	for name, sched := range bad {
		req := validRegisterRequest()
		req.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "bad", Schedule: sched}}
		_, err := registrationFromProto(req, testNow)
		var apiErr *apierr.Error
		require.ErrorAs(t, err, &apiErr, "%s: err = %v, want INVALID_REGISTRATION naming the Task", name, err)
		assert.Equal(t, apierr.CodeInvalidRegistration, apiErr.Code, "%s", name)
		assert.Equal(t, "bad", apiErr.Details["task_id"], "%s", name)
	}
}

// apiCode returns the API code of err, or "" when err is not an API error.
func apiCode(err error) apierr.Code {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return ""
}

func TestFinishSucceededIsIdempotent(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	in := domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded, Output: []byte(`{"ok":true}`)}

	first, err := e.finishRun(context.Background(), id, in)
	require.NoError(t, err, "finish")

	second, err := e.finishRun(context.Background(), id, in)
	require.NoError(t, err, "repeat finish")
	require.Equal(t, domain.RunStatusSucceeded, first.Run.Status, "statuses")
	require.Equal(t, domain.RunStatusSucceeded, second.Run.Status, "statuses")
	require.Equal(t, `{"ok":true}`, string(second.Run.Output), "repeat answer differs")
	require.True(t, second.Run.FinishedAt.Equal(first.Run.FinishedAt), "repeat answer differs: %+v", second.Run)
}

func TestFinishWithStaleTokenIsStale(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")

	_, err := e.finishRun(context.Background(), id, domain.FinishInput{Token: "other", Kind: domain.FinishSucceeded})
	require.Equal(t, apierr.CodeStaleLease, apiCode(err), "err %v", err)
}

func TestFinishUnknownRunIsNotFound(t *testing.T) {
	e := newTestService(t, newFakeStore())
	_, err := e.finishRun(context.Background(), "missing-run", domain.FinishInput{Token: "t", Kind: domain.FinishSucceeded})
	require.Equal(t, apierr.CodeRunNotFound, apiCode(err), "err %v", err)
}

func TestReleasedAnswersRetryAndStaleAfterRequeue(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	release := domain.FinishInput{Token: "tok-1", Kind: domain.FinishReleased}

	answer, err := e.finishRun(context.Background(), id, release)
	require.NoError(t, err, "release")
	require.NotNil(t, answer.Retry, "release must answer retry, not finished")
	require.Empty(t, answer.Run.Status, "release must answer retry, not finished")
	require.True(t, answer.Retry.NotBefore.Equal(testNow), "not_before = %v, want now %v", answer.Retry.NotBefore, testNow)

	stored, err := store.GetRun(context.Background(), id)
	require.NoError(t, err, "get")
	require.Equal(t, domain.RunStatusQueued, stored.Status, "stored after release")
	require.Equal(t, 0, stored.Attempts, "stored after release")
	require.Empty(t, stored.LeaseToken, "stored after release")

	// The same token resent gets the same retry answer.
	again, err := e.finishRun(context.Background(), id, release)
	require.NoError(t, err, "resent release")
	require.NotNil(t, again.Retry, "resent release")
	require.True(t, again.Retry.NotBefore.Equal(answer.Retry.NotBefore), "resent release: %+v", again)

	// A successful finish with the token of the released attempt must not pass.
	_, err = e.finishRun(context.Background(), id, domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded})
	if err != nil {
		require.Equal(t, apierr.CodeStaleLease, apiCode(err), "err %v", err)
	}

	after, _ := store.GetRun(context.Background(), id)
	require.NotEqual(t, domain.RunStatusSucceeded, after.Status, "a released Run must not be finished with its old token")
}

func TestStepRetryQueuesRunAndOldTokenIsStale(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	answer, err := e.recordStep(ctx, id, "tok-1", domain.StepReport{
		StepID: "b", Attempt: 1, Error: domain.AttemptError{Message: "flaky"},
	})
	require.NoError(t, err, "record step")
	require.NotNil(t, answer.Retry, "answer = %+v, want retry after 1s", answer)
	require.True(t, answer.Retry.NotBefore.Equal(testNow.Add(time.Second)), "answer = %+v, want retry after 1s", answer)

	stored, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusQueued, stored.Status, "run status, want queued/retry_backoff")
	require.Equal(t, domain.QueuedReasonRetryBackoff, stored.QueuedReason, "run reason, want queued/retry_backoff")

	// The worker that had the attempt cannot finish it now.
	_, err = e.finishRun(ctx, id, domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded})
	require.Equal(t, apierr.CodeStaleLease, apiCode(err), "finish after requeue: err %v", err)

	err = e.ackDispatch(ctx, id, "tok-1")
	require.Error(t, err, "ack of a queued Run must fail")
}

func TestRecordStepRepeatGetsSameRetry(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()
	rep := domain.StepReport{StepID: "b", Attempt: 1, Error: domain.AttemptError{Message: "flaky"}}

	first, err := e.recordStep(ctx, id, "tok-1", rep)
	require.NoError(t, err, "record")

	// The answer was lost. The worker resends with the same token, which is no longer current.
	second, err := e.recordStep(ctx, id, "tok-1", rep)
	require.NoError(t, err, "resent record must not be stale")
	require.NotNil(t, first.Retry, "first answer = %+v", first)
	require.NotNil(t, second.Retry, "resent answer = %+v, first = %+v", second.Retry, first.Retry)
	require.True(t, second.Retry.NotBefore.Equal(first.Retry.NotBefore), "resent answer = %+v, first = %+v", second.Retry, first.Retry)
	require.Equal(t, 1, second.Retry.AttemptsUsed, "resent answer = %+v, first = %+v", second.Retry, first.Retry)
}

func TestRecordStepFinalAtLimitKeepsRunRunning(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	// Three tries of step b, each time with the lease that the dispatch gave.
	token := "tok-1"
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			// The next attempt is dispatched with a new token.
			token = "tok-" + string(rune('0'+attempt))
			store.mu.Lock()
			run := store.runs[id]
			run.Status = domain.RunStatusRunning
			run.LeaseToken = token
			run.LeaseExpiresAt = testNow.Add(time.Minute)
			run.QueuedReason = domain.QueuedReasonNone
			run.DueAt = testNow
			store.runs[id] = run
			store.mu.Unlock()
		}

		answer, err := e.recordStep(ctx, id, token, domain.StepReport{
			StepID: "b", Attempt: attempt, Error: domain.AttemptError{Message: "boom"},
		})
		require.NoError(t, err, "attempt %d", attempt)
		if attempt < 3 {
			require.NotNil(t, answer.Retry, "attempt %d must retry, got %+v", attempt, answer)
		}
		if attempt == 3 {
			require.NotNil(t, answer.Record, "attempt 3 must be the final record, got %+v", answer)
			require.Equal(t, domain.StepStatusFailed, answer.Record.Status, "attempt 3 must be the final record, got %+v", answer)
		}
	}

	stored, _ := store.GetRun(ctx, id)
	require.Equal(t, domain.RunStatusRunning, stored.Status, "a final Step must leave the Run running")
}

func TestAckWithCurrentTokenIsIdempotent(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	err := e.ackDispatch(ctx, id, "tok-1")
	require.NoError(t, err, "ack")
	err = e.ackDispatch(ctx, id, "tok-1")
	require.NoError(t, err, "repeated ack")
	err = e.ackDispatch(ctx, id, "other")
	require.Equal(t, apierr.CodeStaleLease, apiCode(err), "ack with another token: %v", err)
}

func TestHeartbeatRenewsOnlyCurrentAttempts(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	stale, expires, err := e.heartbeat(ctx, "i-1", []domain.AttemptRef{
		{RunID: id, Token: "tok-1"},
		{RunID: id, Token: "old"},
		{RunID: "missing-run", Token: "tok-1"},
	})
	require.NoError(t, err, "heartbeat")
	require.Len(t, stale, 2, "stale")
	want := testNow.Add(30 * time.Second)
	require.True(t, expires.Equal(want), "leases expire at %v, want %v", expires, want)

	stored, _ := store.GetRun(ctx, id)
	require.True(t, stored.LeaseExpiresAt.Equal(expires), "lease expiry = %v, want %v", stored.LeaseExpiresAt, expires)
}

func TestRecordStepRejectsBadStepIDAndAttempt(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	_, err := e.recordStep(ctx, id, "tok-1", domain.StepReport{StepID: "", Attempt: 1, Succeeded: true})
	require.Equal(t, apierr.CodeInvalidRequest, apiCode(err), "empty step id: %v", err)

	_, err = e.recordStep(ctx, id, "tok-1", domain.StepReport{StepID: "a", Attempt: 0, Succeeded: true})
	require.Equal(t, apierr.CodeInvalidRequest, apiCode(err), "attempt 0: %v", err)

	_, err = e.recordStep(ctx, id, "tok-1", domain.StepReport{StepID: "a", Attempt: 2, Succeeded: true})
	require.Equal(t, apierr.CodeInvalidRequest, apiCode(err), "attempt 2 for a new step: %v", err)
}

func TestFinishFailedWithoutStepRetriesThenFailsAtLimit(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, &domain.RetryPolicy{MaxAttempts: 2})
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()
	failure := domain.FinishInput{Kind: domain.FinishFailed, Error: domain.AttemptError{Message: "broken"}}

	failure.Token = "tok-1"
	first, err := e.finishRun(ctx, id, failure)
	require.NoError(t, err, "first failure: %+v", first)
	require.NotNil(t, first.Retry, "first failure: %+v", first)
	require.Equal(t, 1, first.Retry.AttemptsUsed, "first failure: %+v", first)

	// The next attempt gets a new token and fails for good at the limit of two.
	store.mu.Lock()
	run := store.runs[id]
	run.Status = domain.RunStatusRunning
	run.LeaseToken = "tok-2"
	run.Attempts = 2
	store.runs[id] = run
	store.mu.Unlock()

	failure.Token = "tok-2"
	second, err := e.finishRun(ctx, id, failure)
	require.NoError(t, err, "second failure")
	require.Nil(t, second.Retry, "at the limit the Run must fail: %+v", second)
	require.Equal(t, domain.RunStatusFailed, second.Run.Status, "at the limit the Run must fail: %+v", second)
	require.NotNil(t, second.Run.Failure, "failure = %+v", second.Run.Failure)
	require.Equal(t, domain.FailureCodeError, second.Run.Failure.Code, "failure = %+v", second.Run.Failure)
}

func TestFinishFailedWithStepIDFailsRunWithoutRetry(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	// The step gives its final record first, as the handler would have seen it.
	_, err := e.recordStep(ctx, id, "tok-1", domain.StepReport{
		StepID: "charge", Attempt: 1, Error: domain.AttemptError{Message: "declined", Permanent: true},
	})
	require.NoError(t, err, "record step")

	answer, err := e.finishRun(ctx, id, domain.FinishInput{
		Token: "tok-1", Kind: domain.FinishFailed, StepID: "charge",
		Error: domain.AttemptError{Message: "declined"},
	})
	require.NoError(t, err, "finish")
	require.Nil(t, answer.Retry, "answer = %+v", answer)
	require.Equal(t, domain.RunStatusFailed, answer.Run.Status, "answer = %+v", answer)

	f := answer.Run.Failure
	require.NotNil(t, f, "failure = %+v", f)
	require.Equal(t, domain.FailureCodeStepFailed, f.Code, "failure = %+v", f)
	require.Equal(t, "charge", f.StepID, "failure = %+v", f)
}

// A retry keeps the Run's due time and sets its next attempt. The Run is not
// claimed before that attempt, and is claimed once it is due.
func TestRetryKeepsDueTimeAndWaitsForNextAttempt(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()

	answer, err := e.finishRun(ctx, id, domain.FinishInput{
		Token: "tok-1", Kind: domain.FinishFailed, Error: domain.AttemptError{Message: "boom"},
	})
	require.NoError(t, err, "finish: %+v", answer)
	require.NotNil(t, answer.Retry, "finish: %+v", answer)

	stored, _ := store.GetRun(ctx, id)
	require.True(t, stored.DueAt.Equal(testNow), "due_at = %v, must stay %v", stored.DueAt, testNow)
	want := testNow.Add(time.Second)
	require.True(t, stored.NextAttemptAt.Equal(want), "next_attempt_at = %v, want %v", stored.NextAttemptAt, want)
	require.True(t, answer.Retry.NotBefore.Equal(want), "answer = %v, want %v", answer.Retry.NotBefore, want)

	ref := testRef
	_, claimed, _ := store.ClaimNext(ctx, ref, domain.Claim{Now: testNow.Add(500 * time.Millisecond)})
	require.False(t, claimed, "a retry must not be claimed before its next attempt")
	_, claimed, _ = store.ClaimNext(ctx, ref, domain.Claim{Now: testNow.Add(time.Second)})
	require.True(t, claimed, "a retry must be claimed at its next attempt")
}

// A repeated report after the Run was dispatched again still gets the stored retry time.
func TestRepeatedRetryAfterDispatchAgainKeepsTheTime(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	id := store.seedRunning(t, testRef, "tok-1")
	ctx := context.Background()
	in := domain.FinishInput{Token: "tok-1", Kind: domain.FinishFailed, Error: domain.AttemptError{Message: "boom"}}

	first, err := e.finishRun(ctx, id, in)
	require.NoError(t, err, "finish")

	// Dispatch the retry again, with a new token.
	_, claimed, _ := store.ClaimNext(ctx, testRef, domain.Claim{Now: testNow.Add(time.Minute), LeaseToken: "tok-2",
		InstanceID: "i-1", LeaseExpiresAt: testNow.Add(2 * time.Minute)})
	require.True(t, claimed, "the retry must be dispatched")

	again, err := e.finishRun(ctx, id, in)
	require.NoError(t, err, "resent failure after dispatch again: %+v", again)
	require.NotNil(t, again.Retry, "resent failure after dispatch again: %+v", again)
	require.True(t, again.Retry.NotBefore.Equal(first.Retry.NotBefore),
		"resent failure after dispatch again: %+v (want the first retry time)", again)
}

// logService builds an engine whose log lines go to the returned buffer, at debug level.
func logService(t *testing.T, store *fakeStore) (*Service, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := newService(
		testConfig(),
		logger,
		store,
		store,
		store,
		store,
		newFakeSignals(),
		func() time.Time { return testNow },
		func() float64 { return 0.5 },
	)
	return e, buf
}

// A repeat registration of an instance is logged as a repeat; the schedule counts appear only
// when a registration changes a schedule.
func TestRegistrationLogsFirstThenRepeatAndScheduleChanges(t *testing.T) {
	store := newFakeStore()
	e, buf := logService(t, store)
	ctx := context.Background()
	reg := domain.Registration{WorkerID: "demo", InstanceID: "i-1", Slots: 2,
		Tasks: []domain.Task{everyTask("hello", 2*time.Second, false)}, SDKName: "py", SDKVersion: "1.0"}

	err := e.register(ctx, reg)
	require.NoError(t, err, "register")
	err = e.register(ctx, reg)
	require.NoError(t, err, "register again")

	out := buf.String()
	require.Contains(t, out, `"msg":"worker registered"`, "want a first and a repeat registration line:\n%s", out)
	require.Contains(t, out, `"first_registration":true`, "want a first and a repeat registration line:\n%s", out)
	require.Contains(t, out, `"first_registration":false`, "want a first and a repeat registration line:\n%s", out)
	require.Equal(t, 1, strings.Count(out, `"msg":"schedules changed"`),
		"the first registration adds a schedule; the repeat changes nothing:\n%s", out)
	require.Contains(t, out, `"added":1`, "the added schedule is counted:\n%s", out)
}

func TestScheduleChangesCountsAddedChangedRemoved(t *testing.T) {
	stored := []domain.ScheduleEntry{
		{Ref: domain.TaskRef{TaskID: "same"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second}},
		{Ref: domain.TaskRef{TaskID: "changed"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second}},
		{Ref: domain.TaskRef{TaskID: "removed"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second}},
	}

	plan := []domain.ScheduleEntry{
		{Ref: domain.TaskRef{TaskID: "same"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second}},
		{Ref: domain.TaskRef{TaskID: "changed"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: 5 * time.Second}},
		{Ref: domain.TaskRef{TaskID: "added"}, Schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second}},
	}

	added, changed, removed := scheduleChanges(stored, plan)
	require.Equal(t, 1, added, "counts: added")
	require.Equal(t, 1, changed, "counts: changed")
	require.Equal(t, 1, removed, "counts: removed")
}

// The cause of a stream's end is decided in a fixed order: the server stopping, a
// replacement, a close the dispatcher made, the worker's own disconnect, then silence.
func TestStreamEndCauses(t *testing.T) {
	store := newFakeStore()
	e, _ := logService(t, store)
	registerInstance(t, e, "i-1", testNow)
	sess := attachInstance(t, e, "i-1", testNow)

	got := e.streamEndCause("i-1", sess, false)
	require.Equal(t, "ended for silence", got, "silent stream: cause")
	got = e.streamEndCause("i-1", sess, true)
	require.Equal(t, "closed by the worker", got, "worker gone: cause")

	e.dispatcher.ends.note(sess, "send timeout")
	got = e.streamEndCause("i-1", sess, true)
	require.Equal(t, "send timeout", got, "dispatcher close: cause")

	newer := attachInstance(t, e, "i-1", testNow)
	got = e.streamEndCause("i-1", sess, true)
	require.Equal(t, "replaced by a new stream", got, "replaced: cause (newer stream attached: %v)", newer != nil)

	stopped := make(chan struct{})
	close(stopped)
	e.stopped = stopped
	got = e.streamEndCause("i-1", sess, false)
	require.Equal(t, "server stopping", got, "stopping: cause")
}

func TestRecordStepOutputOverLimitIsStoredFailed(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil) // step output limit is 8 bytes
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(time.Minute))

	answer, err := e.recordStep(context.Background(), id, "tok-1", domain.StepReport{
		StepID: "a", Attempt: 1, Succeeded: true, Output: []byte("0123456789"),
	})
	require.NoError(t, err, "record")
	require.NotNil(t, answer.Record, "answer = %+v, want FAILED OUTPUT_TOO_LARGE", answer.Record)
	require.Equal(t, domain.StepStatusFailed, answer.Record.Status, "answer = %+v, want FAILED OUTPUT_TOO_LARGE", answer.Record)
	require.NotNil(t, answer.Record.Failure, "answer = %+v, want FAILED OUTPUT_TOO_LARGE", answer.Record)
	require.Equal(t, domain.FailureCodeOutputTooLarge, answer.Record.Failure.Code, "answer = %+v, want FAILED OUTPUT_TOO_LARGE", answer.Record)
}

func TestFinishOutputOverRunLimitFailsRun(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil) // run output limit is 8 bytes
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(time.Minute))

	answer, err := e.finishRun(context.Background(), id, domain.FinishInput{
		Token: "tok-1", Kind: domain.FinishSucceeded, Output: []byte("0123456789"),
	})
	require.NoError(t, err, "finish")
	require.Nil(t, answer.Retry, "answer = %+v, want the Run FAILED OUTPUT_TOO_LARGE", answer.Run)
	require.Equal(t, domain.RunStatusFailed, answer.Run.Status, "answer = %+v, want the Run FAILED OUTPUT_TOO_LARGE", answer.Run)
	require.NotNil(t, answer.Run.Failure, "answer = %+v, want the Run FAILED OUTPUT_TOO_LARGE", answer.Run)
	require.Equal(t, domain.FailureCodeOutputTooLarge, answer.Run.Failure.Code, "answer = %+v, want the Run FAILED OUTPUT_TOO_LARGE", answer.Run)
}

func TestHeartbeatRecordsRunningStepAndReportsStale(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil)
	registerInstance(t, e, "i-1", testNow)
	id := seedLeased(t, store, testRef, "tok-1", true, testNow.Add(time.Minute))

	stale, expires, err := e.heartbeat(context.Background(), "i-1", []domain.AttemptRef{
		{RunID: id, Token: "tok-1", StepID: "b", StepAttempt: 2},
		{RunID: id, Token: "old"},
	})
	require.NoError(t, err, "heartbeat")
	require.Len(t, stale, 1, "stale = %+v", stale)
	require.Equal(t, "old", stale[0].Token, "stale = %+v", stale)
	require.True(t, expires.Equal(testNow.Add(30*time.Second)), "expires = %v", expires)

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, "b", run.RunningStepID, "running step = %q/%d, want b/2", run.RunningStepID, run.RunningStepAttempt)
	require.Equal(t, 2, run.RunningStepAttempt, "running step = %q/%d, want b/2", run.RunningStepID, run.RunningStepAttempt)
}

func TestRegistrationRejectsTimeZonePrefix(t *testing.T) {
	req := validRegisterRequest()
	req.Tasks = []*runcoilv1.TaskDefinition{{TaskId: "tz", Schedule: &runcoilv1.Schedule{
		Kind: &runcoilv1.Schedule_Cron{Cron: "CRON_TZ=Asia/Tokyo 0 9 * * *"}}}}

	_, err := registrationFromProto(req, testNow)
	require.Equal(t, apierr.CodeInvalidRegistration, engineCode(err), "err = %v, want INVALID_REGISTRATION", err)
}

func TestConcurrentRegistrationsApplyInOneOrder(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	done := make(chan error, 2)
	for _, every := range []time.Duration{2 * time.Second, 5 * time.Second} {
		go func(d time.Duration) {
			done <- e.register(context.Background(), domain.Registration{
				WorkerID: "demo", InstanceID: "i-1", Slots: 1, Tasks: []domain.Task{everyTask("hello", d, false)},
			})
		}(every)
	}

	for range 2 {
		err := <-done
		require.NoError(t, err, "register")
	}

	// Both registrations applied in one order: the schedule row exists, and the instance is known.
	store.mu.Lock()
	sc := store.schedules[scheduledRef]
	store.mu.Unlock()
	require.NotNil(t, sc, "schedule after two registrations")
	require.Contains(t, []time.Duration{2 * time.Second, 5 * time.Second}, sc.sched.Every,
		"schedule after two registrations = %+v", sc)
	sess := e.instances.StreamOf("i-1")
	require.Nil(t, sess, "no stream was attached in this test")
}

// A heartbeat with an invalid Step policy on one attempt still renews every lease.
func TestHeartbeatIgnoresAnInvalidPolicyAndRenewsAll(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	attachInstance(t, e, "i-1", testNow)
	good := seedLeased(t, store, testRef, "tok-good", true, testNow.Add(5*time.Second))
	bad := seedLeased(t, store, testRef, "tok-bad", true, testNow.Add(5*time.Second))

	stale, expires, err := e.heartbeat(context.Background(), "i-1", []domain.AttemptRef{
		{RunID: good, Token: "tok-good"},
		{RunID: bad, Token: "tok-bad", StepID: "charge", StepAttempt: 1, Retry: nil},
	})
	require.NoError(t, err, "heartbeat")
	require.Empty(t, stale, "heartbeat: stale")

	for _, id := range []string{good, bad} {
		run, _ := store.GetRun(context.Background(), id)
		require.True(t, run.LeaseExpiresAt.Equal(expires), "run %s lease = %v, want the renewed %v", id, run.LeaseExpiresAt, expires)
	}
}

// An over-long Step ID is not stored and does not refuse the heartbeat. The attempt that names
// it holds no running Step, but its lease is renewed; the other attempt's Step is stored as sent.
// The warning names the length and the Run, and does not echo the Step ID.
func TestHeartbeatDropsAnOverlongStepIDAndRenewsAll(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e, logs := logService(t, store)
	registerInstance(t, e, "i-1", testNow)
	longStep := strings.Repeat("s", 300)
	long := seedLeased(t, store, testRef, "tok-long", true, testNow.Add(5*time.Second))
	good := seedLeased(t, store, testRef, "tok-good", true, testNow.Add(5*time.Second))

	resp, err := workerHandler{s: e}.Heartbeat(context.Background(), connect.NewRequest(&runcoilv1.HeartbeatRequest{
		InstanceId: "i-1",
		Attempts: []*runcoilv1.AttemptRef{
			{RunId: long, LeaseToken: "tok-long", StepId: longStep, StepAttempt: 1},
			{RunId: good, LeaseToken: "tok-good", StepId: "charge", StepAttempt: 2},
		},
	}))
	require.NoError(t, err, "an over-long Step ID must not refuse the heartbeat")
	require.Empty(t, resp.Msg.GetStale(), "no attempt is stale")

	ctx := context.Background()
	longRun, err := store.GetRun(ctx, long)
	require.NoError(t, err, "get the attempt with the long Step ID")
	goodRun, err := store.GetRun(ctx, good)
	require.NoError(t, err, "get the other attempt")

	assert.True(t, longRun.LeaseExpiresAt.After(testNow.Add(5*time.Second)), "the lease of the long-Step attempt is renewed")
	assert.True(t, goodRun.LeaseExpiresAt.After(testNow.Add(5*time.Second)), "the lease of the other attempt is renewed")
	assert.Empty(t, longRun.RunningStepID, "the over-long Step ID must not be stored")
	assert.Zero(t, longRun.RunningStepAttempt, "no Step attempt is stored for the cleared Step")
	assert.Equal(t, "charge", goodRun.RunningStepID, "the other attempt's Step is stored")
	assert.Equal(t, 2, goodRun.RunningStepAttempt, "the other attempt's Step attempt is stored")

	assert.Contains(t, logs.String(), "heartbeat step id ignored", "the drop is logged")
	assert.Contains(t, logs.String(), `"step_id_bytes":300`, "the log names the length")
	assert.NotContains(t, logs.String(), longStep, "the log must not echo the Step ID")
}

var scheduledRef = domain.TaskRef{WorkerID: "demo", TaskID: "hello"}
