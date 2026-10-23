package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

func TestRunToProto(t *testing.T) {
	runID := "0196f0a0-0000-7000-8000-000000000001"
	rerunOf := "0196f0a0-0000-7000-8000-000000000002"
	created := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)

	run := domain.Run{
		ID:           runID,
		WorkerID:     "demo",
		TaskID:       "hello",
		Status:       domain.RunStatusRunning,
		QueuedReason: domain.QueuedReasonNone,
		Source:       domain.RunSourceRerun,
		Attempts:     2,
		Input:        []byte(`{"name":"vux"}`),
		Output:       []byte(`{"ok":true}`),
		Failure: &domain.Failure{
			Code:     domain.FailureCodePanic,
			Message:  "boom",
			Attempts: 2,
			StepID:   "charge",
		},
		InstanceID: "i-1",
		DueAt:      created,
		CreatedAt:  created,
		StartedAt:  timePointer(started),
		RerunOf:    &rerunOf,
	}

	got := runToProto(run)

	assert.Equal(t, runID, got.GetRunId())
	assert.Equal(t, "demo", got.GetWorkerId(), "worker_id")
	assert.Equal(t, "hello", got.GetTaskId(), "task_id")
	assert.Equal(t, runcoilv1.RunStatus_RUN_STATUS_RUNNING, got.GetStatus())
	assert.False(t, got.GetTerminal(), "running run must not be terminal")
	assert.Equal(t, runcoilv1.QueuedReason_QUEUED_REASON_UNSPECIFIED, got.GetQueuedReason(), "queued_reason")
	assert.Equal(t, runcoilv1.RunSource_RUN_SOURCE_RERUN, got.GetSource())
	assert.Equal(t, uint32(2), got.GetAttempts())
	assert.Equal(t, `{"name":"vux"}`, string(got.GetInput()), "input")
	assert.Equal(t, `{"ok":true}`, string(got.GetOutput()), "output")

	f := got.GetFailure()
	assert.Equal(t, runcoilv1.FailureCode_FAILURE_CODE_PANIC, f.GetCode(), "failure")
	assert.Equal(t, "boom", f.GetMessage(), "failure")
	assert.Equal(t, uint32(2), f.GetAttempts(), "failure")
	assert.Equal(t, "charge", f.GetStepId(), "failure")

	assert.Equal(t, "i-1", got.GetInstanceId())
	assert.Equal(t, rerunOf, got.GetRerunOf())
	assert.True(t, got.GetCreatedAt().AsTime().Equal(created), "created_at = %v", got.GetCreatedAt().AsTime())
	assert.Nil(t, got.GetFinishedAt(), "finished_at must be unset for an unfinished run")
	assert.Nil(t, got.GetSteps(), "steps must not be filled in by runToProto")
}

func TestRunToProtoQueuedAndUnsetFields(t *testing.T) {
	run := domain.Run{
		ID:           uuid.NewString(),
		WorkerID:     "demo",
		TaskID:       "hello",
		Status:       domain.RunStatusQueued,
		QueuedReason: domain.QueuedReasonWaitingForWorker,
		Source:       domain.RunSourceInvoke,
		DueAt:        time.Now(),
		CreatedAt:    time.Now(),
	}

	got := runToProto(run)

	assert.Equal(t, runcoilv1.RunStatus_RUN_STATUS_QUEUED, got.GetStatus())
	assert.Equal(t, runcoilv1.QueuedReason_QUEUED_REASON_WAITING_FOR_WORKER, got.GetQueuedReason())
	assert.Empty(t, got.GetRerunOf(), "rerun_of")
	assert.Nil(t, got.GetFailure(), "unset optional fields must be nil")
	assert.Nil(t, got.GetStartedAt(), "unset optional fields must be nil")
	assert.Nil(t, got.GetFinishedAt(), "unset optional fields must be nil")
}

func TestRunToProtoRetryBackoffReason(t *testing.T) {
	got := runToProto(domain.Run{ID: uuid.NewString(), Status: domain.RunStatusQueued,
		QueuedReason: domain.QueuedReasonRetryBackoff})

	assert.Equal(t, runcoilv1.QueuedReason_QUEUED_REASON_RETRY_BACKOFF, got.GetQueuedReason(), "queued_reason")
}

func TestRunToProtoTerminal(t *testing.T) {
	for _, status := range []domain.RunStatus{domain.RunStatusSucceeded, domain.RunStatusFailed} {
		got := runToProto(domain.Run{ID: uuid.NewString(), Status: status})
		assert.True(t, got.GetTerminal(), "status %s must be terminal", status)
	}
}

func TestRunStatusAndSourceMappingsCoverEveryDomainValue(t *testing.T) {
	for _, status := range []domain.RunStatus{
		domain.RunStatusQueued, domain.RunStatusRunning, domain.RunStatusSucceeded, domain.RunStatusFailed,
	} {
		assert.NotEqual(t, runcoilv1.RunStatus_RUN_STATUS_UNSPECIFIED, runStatusToProto[status],
			"status %s has no proto value", status)
	}

	for _, source := range []domain.RunSource{domain.RunSourceInvoke, domain.RunSourceSchedule, domain.RunSourceRerun} {
		assert.NotEqual(t, runcoilv1.RunSource_RUN_SOURCE_UNSPECIFIED, runSourceToProto[source],
			"source %s has no proto value", source)
	}
}

func TestFailureCodeRoundTrip(t *testing.T) {
	for protoCode, domainCode := range map[runcoilv1.FailureCode]domain.FailureCode{
		runcoilv1.FailureCode_FAILURE_CODE_ERROR:                domain.FailureCodeError,
		runcoilv1.FailureCode_FAILURE_CODE_PANIC:                domain.FailureCodePanic,
		runcoilv1.FailureCode_FAILURE_CODE_OUTPUT_NOT_ENCODABLE: domain.FailureCodeOutputNotEncodable,
		runcoilv1.FailureCode_FAILURE_CODE_START_DEADLINE:       domain.FailureCodeStartDeadline,
	} {
		assert.Equal(t, domainCode, failureCodeFromProto(protoCode), "failureCodeFromProto(%v)", protoCode)
		assert.Equal(t, protoCode, failureToProto(domain.Failure{Code: domainCode}).GetCode(),
			"failureToProto(%q) code", domainCode)
	}
}

func TestFailureCodeFromProtoDefaultsToError(t *testing.T) {
	for _, code := range []runcoilv1.FailureCode{
		runcoilv1.FailureCode_FAILURE_CODE_UNSPECIFIED,
		runcoilv1.FailureCode(999),
	} {
		assert.Equal(t, domain.FailureCodeError, failureCodeFromProto(code), "failureCodeFromProto(%v)", code)
	}
}

func TestDispatchMessageCarriesStepsAndProgress(t *testing.T) {
	runID := uuid.NewString()
	due := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	expires := due.Add(30 * time.Second)

	run := domain.Run{
		ID:             runID,
		WorkerID:       "demo",
		TaskID:         "hello",
		Attempts:       2,
		LeaseToken:     "token-1",
		LeaseExpiresAt: timePointer(expires),
		Input:          []byte(`{"name":"vux"}`),
		Source:         domain.RunSourceInvoke,
		DueAt:          due,
	}

	steps := []domain.StepRecord{
		{StepID: "a", Position: 1, Status: domain.StepStatusSucceeded, AttemptsUsed: 1, Output: []byte(`1`)},
		{StepID: "b", Position: 2, Status: domain.StepStatusPending, AttemptsUsed: 1, RetryNotBefore: timePointer(due)},
		{StepID: "c", Position: 3, Status: domain.StepStatusFailed, AttemptsUsed: 2,
			Failure: &domain.Failure{Code: domain.FailureCodeError, Message: "no", Attempts: 2, StepID: "c"}},
	}

	msg := dispatchMessage(run, steps)

	d := msg.GetDispatch()
	require.NotNil(t, d, "message is not a dispatch: %v", msg)
	assert.Equal(t, runID, d.GetRunId())
	assert.Equal(t, uint32(2), d.GetAttempt())
	assert.Equal(t, "token-1", d.GetLeaseToken())
	assert.Equal(t, "hello", d.GetTaskId())
	assert.Equal(t, `{"name":"vux"}`, string(d.GetInput()), "input")
	assert.Equal(t, runcoilv1.RunSource_RUN_SOURCE_INVOKE, d.GetSource())
	assert.True(t, d.GetLeaseExpiresAt().AsTime().Equal(expires), "lease_expires_at = %v", d.GetLeaseExpiresAt())
	assert.True(t, d.GetDueAt().AsTime().Equal(due), "due_at = %v", d.GetDueAt())

	require.Len(t, d.GetSteps(), 2, "steps must be the finished ones")
	assert.Equal(t, "a", d.GetSteps()[0].GetStepId(), "steps must be the finished ones in order")
	assert.Equal(t, "c", d.GetSteps()[1].GetStepId(), "steps must be the finished ones in order")
	assert.Equal(t, uint32(1), d.GetSteps()[0].GetIndex(), "step records")
	assert.Equal(t, runcoilv1.FailureCode_FAILURE_CODE_ERROR, d.GetSteps()[1].GetFailure().GetCode(), "step records")

	require.Len(t, d.GetProgress(), 1, "progress")
	assert.Equal(t, "b", d.GetProgress()[0].GetStepId(), "progress")
	assert.Equal(t, uint32(1), d.GetProgress()[0].GetAttemptsUsed(), "progress")
}

func TestStepRecordToProto(t *testing.T) {
	got := stepRecordToProto(domain.StepRecord{
		StepID:       "charge",
		Position:     4,
		Status:       domain.StepStatusFailed,
		AttemptsUsed: 3,
		Failure:      &domain.Failure{Code: domain.FailureCodePanic, Message: "p", Attempts: 3, StepID: "charge"},
	})

	assert.Equal(t, "charge", got.GetStepId())
	assert.Equal(t, uint32(4), got.GetIndex())
	assert.Equal(t, runcoilv1.StepStatus_STEP_STATUS_FAILED, got.GetStatus())
	assert.Equal(t, runcoilv1.FailureCode_FAILURE_CODE_PANIC, got.GetFailure().GetCode(), "failure")
	assert.Equal(t, uint32(3), got.GetFailure().GetAttempts(), "failure")

	ok := stepRecordToProto(domain.StepRecord{StepID: "a", Position: 1, Status: domain.StepStatusSucceeded,
		Output: []byte(`9`)})

	assert.Equal(t, runcoilv1.StepStatus_STEP_STATUS_SUCCEEDED, ok.GetStatus(), "succeeded step")
	assert.Equal(t, "9", string(ok.GetOutput()), "succeeded step")
	assert.Nil(t, ok.GetFailure(), "succeeded step")
}

func TestRetryScheduledToProto(t *testing.T) {
	at := time.Date(2026, 10, 11, 6, 0, 5, 0, time.UTC)
	got := retryScheduledToProto(domain.RetryAnswer{NotBefore: at, AttemptsUsed: 2})
	assert.True(t, got.GetNotBefore().AsTime().Equal(at), "not_before = %v", got.GetNotBefore().AsTime())
	assert.Equal(t, uint32(2), got.GetAttemptsUsed())
}

func TestKeepaliveMessage(t *testing.T) {
	require.NotNil(t, keepaliveMessage().GetKeepalive(), "keepalive message has no keepalive")
}

func TestTimestampOrNilZeroIsNil(t *testing.T) {
	assert.Nil(t, timestampOrNil(nil), "absent time must map to nil")
	at := time.Now()
	assert.NotNil(t, timestampOrNil(&at), "set time must map to a timestamp")
}

func TestClampUint32(t *testing.T) {
	tests := []struct {
		in   int
		want uint32
	}{
		{-1, 0},
		{0, 0},
		{7, 7},
		{math.MaxUint32, math.MaxUint32},
		{math.MaxUint32 + 1, math.MaxUint32},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, clampUint32(tt.in), "clampUint32(%d)", tt.in)
	}
}

func TestScheduleFromProto(t *testing.T) {
	got, err := scheduleFromProto(nil)
	assert.Nil(t, got, "nil schedule must map to nil")
	assert.NoError(t, err, "nil schedule must map to nil")

	cron, err := scheduleFromProto(&runcoilv1.Schedule{
		Kind:        &runcoilv1.Schedule_Cron{Cron: "*/5 * * * *"},
		SkipOverlap: true,
	})
	require.NoError(t, err, "cron")
	require.NotNil(t, cron, "cron")
	assert.Equal(t, domain.ScheduleKindCron, cron.Kind, "cron")
	assert.Equal(t, "*/5 * * * *", cron.Cron, "cron")
	assert.True(t, cron.SkipOverlap, "cron")

	daily, err := scheduleFromProto(&runcoilv1.Schedule{
		Kind: &runcoilv1.Schedule_Daily{Daily: &runcoilv1.DailyTimes{
			At: []*runcoilv1.TimeOfDay{{Hour: 6, Minute: 30}, {Hour: 18}},
		}},
	})
	require.NoError(t, err, "daily")
	require.NotNil(t, daily, "daily")
	require.Equal(t, domain.ScheduleKindDaily, daily.Kind, "daily")
	require.Len(t, daily.Daily, 2, "daily")
	assert.Equal(t, domain.TimeOfDay{Hour: 6, Minute: 30}, daily.Daily[0], "daily times")
	assert.Equal(t, domain.TimeOfDay{Hour: 18}, daily.Daily[1], "daily times")
}

func TestRetryFromProto(t *testing.T) {
	got, err := retryFromProto(nil)
	assert.Nil(t, got, "nil retry must map to nil")
	assert.NoError(t, err, "nil retry must map to nil")

	got, err = retryFromProto(&runcoilv1.RetryPolicy{
		MaxAttempts: 3,
		Delays:      []*durationpb.Duration{durationpb.New(time.Second), durationpb.New(2 * time.Second)},
	})
	require.NoError(t, err, "retry")
	require.NotNil(t, got, "retry")
	require.Equal(t, 3, got.MaxAttempts, "retry")
	require.Len(t, got.Delays, 2, "delays")
	assert.Equal(t, time.Second, got.Delays[0], "delays")
	assert.Equal(t, 2*time.Second, got.Delays[1], "delays")
}

func TestStepReportFromProto(t *testing.T) {
	rep, err := stepReportFromProto(&runcoilv1.RecordStepRequest{
		StepId:      "b",
		StepAttempt: 2,
		Outcome: &runcoilv1.RecordStepRequest_Failed{Failed: &runcoilv1.AttemptError{
			Message:    "timeout",
			Permanent:  true,
			RetryAfter: durationpb.New(3 * time.Second),
			Code:       runcoilv1.FailureCode_FAILURE_CODE_PANIC,
		}},
	})
	require.NoError(t, err, "stepReportFromProto")
	assert.Equal(t, "b", rep.StepID, "report")
	assert.Equal(t, 2, rep.Attempt, "report")
	assert.False(t, rep.Succeeded, "report")
	assert.Equal(t, "timeout", rep.Error.Message, "error")
	assert.True(t, rep.Error.Permanent, "error")
	require.NotNil(t, rep.Error.RetryAfter, "error")
	assert.Equal(t, 3*time.Second, *rep.Error.RetryAfter, "error")
	assert.Equal(t, domain.FailureCodePanic, rep.Error.Code, "error")

	_, err = stepReportFromProto(&runcoilv1.RecordStepRequest{StepId: "x", StepAttempt: 1})
	assert.Error(t, err, "a request without an outcome must fail")
}

func TestAttemptErrorFromProtoRetryAfterPresenceAndValidation(t *testing.T) {
	got, err := attemptErrorFromProto(&runcoilv1.AttemptError{})
	require.NoError(t, err, "unset retry_after")
	require.Nil(t, got.RetryAfter, "unset retry_after uses policy delay")

	zero, err := attemptErrorFromProto(&runcoilv1.AttemptError{RetryAfter: durationpb.New(0)})
	require.NoError(t, err, "zero retry_after")
	require.NotNil(t, zero.RetryAfter, "set zero retry_after")
	assert.Zero(t, *zero.RetryAfter, "set zero retry_after")

	_, err = attemptErrorFromProto(&runcoilv1.AttemptError{RetryAfter: durationpb.New(-time.Second)})
	require.ErrorContains(t, err, "retry_after must not be negative", "negative retry_after")
}

func TestFinishInputFromProto(t *testing.T) {
	in, err := finishInputFromProto(&runcoilv1.FinishRunRequest{
		RunId:      uuid.NewString(),
		LeaseToken: "tok",
		Result:     &runcoilv1.FinishRunRequest_Released{Released: &runcoilv1.Released{}},
	})
	assert.NoError(t, err, "released input")
	assert.Equal(t, domain.FinishReleased, in.Kind, "released input")
	assert.Equal(t, "tok", in.Token, "released input")

	failed, err := finishInputFromProto(&runcoilv1.FinishRunRequest{
		LeaseToken: "tok",
		Result: &runcoilv1.FinishRunRequest_Failed{Failed: &runcoilv1.HandlerFailed{
			Error:  &runcoilv1.AttemptError{Message: "x", Code: runcoilv1.FailureCode_FAILURE_CODE_STEP_FAILED},
			StepId: "charge",
		}},
	})
	assert.NoError(t, err, "failed input")
	assert.Equal(t, domain.FinishFailed, failed.Kind, "failed input")
	assert.Equal(t, "charge", failed.StepID, "failed input")
	assert.Equal(t, "x", failed.Error.Message, "failed input")

	_, err = finishInputFromProto(&runcoilv1.FinishRunRequest{LeaseToken: "tok"})
	assert.Error(t, err, "a request without a result must fail")
}

func TestNextAttemptAtShownOnlyWhileQueued(t *testing.T) {
	next := time.Date(2026, 10, 11, 6, 0, 5, 0, time.UTC)
	queued := runToProto(domain.Run{ID: uuid.NewString(), Status: domain.RunStatusQueued,
		QueuedReason: domain.QueuedReasonRetryBackoff, NextAttemptAt: timePointer(next)})

	require.True(t, queued.GetNextAttemptAt().AsTime().Equal(next),
		"queued run next_attempt_at = %v, want %v", queued.GetNextAttemptAt(), next)

	running := runToProto(domain.Run{ID: uuid.NewString(), Status: domain.RunStatusRunning, NextAttemptAt: timePointer(next)})
	require.Nil(t, running.GetNextAttemptAt(), "a running Run must not show next_attempt_at")
}

func TestTaskInfoToProto(t *testing.T) {
	registered := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	next := time.Date(2026, 10, 11, 6, 0, 2, 0, time.UTC)
	info := taskInfoToProto(taskListing{
		task: domain.Task{
			WorkerID:      "demo",
			TaskID:        "report",
			Schedule:      &domain.Schedule{Kind: domain.ScheduleKindEvery, Every: 2 * time.Second, SkipOverlap: true},
			Retry:         &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second}},
			StartDeadline: timeDurationPointer(2 * time.Second),
			RegisteredAt:  registered,
			NextRunAt:     timePointer(next),
		},
		readyWorkers: 2,
	})

	assert.Equal(t, "demo", info.GetWorkerId(), "worker_id")
	assert.Equal(t, "report", info.GetTaskId(), "task_id")
	assert.Equal(t, 2*time.Second, info.GetSchedule().GetEvery().AsDuration(), "schedule")
	assert.True(t, info.GetSchedule().GetSkipOverlap(), "schedule")
	assert.Equal(t, uint32(3), info.GetRetry().GetMaxAttempts(), "retry")
	assert.Len(t, info.GetRetry().GetDelays(), 1, "retry")
	assert.Equal(t, 2*time.Second, info.GetStartDeadline().AsDuration(), "start deadline")
	assert.True(t, info.GetRegisteredAt().AsTime().Equal(registered), "registered_at")
	assert.True(t, info.GetNextRunAt().AsTime().Equal(next), "next_run_at")
	assert.Equal(t, uint32(2), info.GetReadyWorkers(), "ready workers")
}

func TestTaskInfoWithoutScheduleOmitsIt(t *testing.T) {
	info := taskInfoToProto(taskListing{task: domain.Task{WorkerID: "demo", TaskID: "x"}})
	assert.Nil(t, info.GetSchedule(), "unscheduled Task must have no schedule")
	assert.Nil(t, info.GetNextRunAt(), "unscheduled Task must have no next run")
	assert.Nil(t, info.GetStartDeadline(), "unscheduled Task must have no deadline")
}

func TestScheduleToProtoDailyAndCron(t *testing.T) {
	daily := scheduleToProto(&domain.Schedule{Kind: domain.ScheduleKindDaily,
		Daily: []domain.TimeOfDay{{Hour: 9, Minute: 30}, {Hour: 17}}})

	times := daily.GetDaily().GetAt()
	require.Len(t, times, 2, "daily")
	assert.Equal(t, uint32(9), times[0].GetHour(), "daily")
	assert.Equal(t, uint32(30), times[0].GetMinute(), "daily")
	assert.Equal(t, uint32(17), times[1].GetHour(), "daily")

	cron := scheduleToProto(&domain.Schedule{Kind: domain.ScheduleKindCron, Cron: "* * * * *"})
	assert.Equal(t, "* * * * *", cron.GetCron(), "cron")
}

func TestConnectCodeOf(t *testing.T) {
	tests := []struct {
		name string
		kind apierr.Kind
		want connect.Code
	}{
		{"invalid", apierr.KindInvalid, connect.CodeInvalidArgument},
		{"unauthenticated", apierr.KindUnauthenticated, connect.CodeUnauthenticated},
		{"not found", apierr.KindNotFound, connect.CodeNotFound},
		{"conflict", apierr.KindConflict, connect.CodeAlreadyExists},
		{"failed precondition", apierr.KindFailedPrecondition, connect.CodeFailedPrecondition},
		{"unavailable", apierr.KindUnavailable, connect.CodeUnavailable},
		{"canceled", apierr.KindCanceled, connect.CodeCanceled},
		{"deadline exceeded", apierr.KindDeadlineExceeded, connect.CodeDeadlineExceeded},
		{"internal", apierr.KindInternal, connect.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, connectCodeOf(tt.kind), "connectCodeOf(%v)", tt.kind)
		})
	}
}

// Every API code must reach the client as its schema ErrorCode, never as UNSPECIFIED.
// A typo in the mapping would otherwise pass silently.
func TestEveryAPICodeHasSchemaCode(t *testing.T) {
	for _, code := range apierr.Codes {
		got := errorCodeOf(code)
		if !assert.NotEqual(t, runcoilv1.ErrorCode_ERROR_CODE_UNSPECIFIED, got, "API code %q maps to ERROR_CODE_UNSPECIFIED", code) {
			continue
		}

		want := "ERROR_CODE_" + string(code)
		assert.Equal(t, want, runcoilv1.ErrorCode_name[int32(got)], "API code %q", code)
	}
}

func TestToConnectError(t *testing.T) {
	apiErr := apierr.NotFound(apierr.CodeTaskNotFound, "task is not registered").WithDetail("task_id", "hello")

	got, convErr := toConnectError(apiErr)
	require.NoError(t, convErr, "toConnectError")
	require.Equal(t, connect.CodeNotFound, got.Code())
	require.Equal(t, "task is not registered", got.Message())

	details := got.Details()
	require.Len(t, details, 1, "details")

	msg, err := connectproto.UnmarshalErrorDetail(details[0])
	require.NoError(t, err, "detail value")

	detail, ok := msg.(*runcoilv1.ErrorDetail)
	require.True(t, ok, "detail type = %T, want *runcoilv1.ErrorDetail", msg)
	require.Equal(t, runcoilv1.ErrorCode_ERROR_CODE_TASK_NOT_FOUND, detail.GetCode(), "detail code")
	require.Equal(t, "hello", detail.GetTaskId(), "detail task_id")
	require.Empty(t, detail.GetRunId(), "detail run_id")
}

func TestToConnectErrorWithoutSchemaCodeHasNoDetail(t *testing.T) {
	got, err := toConnectError(apierr.Canceled("the call was cancelled while waiting"))
	require.NoError(t, err, "toConnectError")
	require.Equal(t, connect.CodeCanceled, got.Code())
	require.Empty(t, got.Details(), "details")
}

func asConnectError(t *testing.T, err error) *connect.Error {
	t.Helper()
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "error is %T, want *connect.Error", err)

	return connectErr
}

// An identifier is checked in bytes, so 255 bytes pass whatever their characters, and one
// byte more is refused. An empty identifier passes here: its caller checks it when it is required.
func TestRequireShortIDCountsBytes(t *testing.T) {
	require.NoError(t, requireShortID("task_id", strings.Repeat("a", 255)), "255 bytes must be allowed")
	require.NoError(t, requireShortID("task_id", ""), "an empty identifier is checked by its caller")
	require.NoError(t, requireShortID("task_id", strings.Repeat("é", 127)), "254 bytes must be allowed")

	// Each é is two bytes, so 128 of them are 256 bytes, though they are only 128 characters.
	err := requireShortID("task_id", strings.Repeat("é", 128))
	var apiErr *apierr.Error
	require.ErrorAs(t, err, &apiErr, "error is %T, want *apierr.Error", err)
	assert.Equal(t, apierr.KindInvalid, apiErr.Kind, "kind")
	assert.Equal(t, apierr.CodeInvalidRequest, apiErr.Code, "code")
	assert.Equal(t, "task_id is longer than 255 bytes", apiErr.Message, "message")
}

func timeDurationPointer(d time.Duration) *time.Duration { return &d }
