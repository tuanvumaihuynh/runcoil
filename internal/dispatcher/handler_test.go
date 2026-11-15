package dispatcher

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/runcoil/runcoil/internal/domain"
	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
)

func TestRegistrationFromProto(t *testing.T) {
	reg := registrationFromProto(&runcoilv1.RegisterRequest{
		App:   "shop",
		Slots: 4,
		Tasks: []*runcoilv1.TaskDefinition{
			{Name: "charge"},
			{
				Name:          "ship",
				Retry:         &runcoilv1.RetryPolicy{MaxAttempts: 3, Delays: []*durationpb.Duration{durationpb.New(5 * time.Second)}},
				StartDeadline: durationpb.New(time.Minute),
			},
		},
	})

	deadline := time.Minute
	assert.Equal(t, domain.Registration{
		App:   "shop",
		Slots: 4,
		Tasks: []domain.TaskDefinition{
			{Name: "charge"},
			{
				Name:          "ship",
				Retry:         &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{5 * time.Second}},
				StartDeadline: &deadline,
			},
		},
	}, reg)
}

func TestFailureCodeFromProto(t *testing.T) {
	assert.Equal(t, domain.FailureCodeError, failureCodeFromProto(runcoilv1.ErrorKind_ERROR_KIND_UNSPECIFIED))
	assert.Equal(t, domain.FailureCodeError, failureCodeFromProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR))
	assert.Equal(t, domain.FailureCodePanic, failureCodeFromProto(runcoilv1.ErrorKind_ERROR_KIND_PANIC))
	assert.Equal(t, domain.FailureCodeOutputNotEncodable, failureCodeFromProto(runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_NOT_ENCODABLE))
	assert.Equal(t, domain.FailureCodeOutputTooLarge, failureCodeFromProto(runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE))
}

func TestStepRecordToProto(t *testing.T) {
	ok := stepRecordToProto(domain.StepRecord{StepID: "charge", Kind: domain.StepKindDo, Output: []byte(`1`)})
	assert.Equal(t, []byte(`1`), ok.GetOutput())

	failed := stepRecordToProto(domain.StepRecord{
		StepID:  "charge",
		Kind:    domain.StepKindDo,
		Failure: &domain.Failure{Code: domain.FailureCodePanic, Message: "boom"},
	})
	assert.Equal(t, runcoilv1.Failure_CODE_PANIC, failed.GetFailure().GetCode())
	assert.Equal(t, "boom", failed.GetFailure().GetMessage())
	assert.Equal(t, "charge", failed.GetFailure().GetStepId())

	tooLarge := stepRecordToProto(domain.StepRecord{
		StepID:  "charge",
		Kind:    domain.StepKindDo,
		Failure: &domain.Failure{Code: domain.FailureCodeOutputTooLarge, Message: "too large"},
	})
	assert.Equal(t, runcoilv1.Failure_CODE_OUTPUT_TOO_LARGE, tooLarge.GetFailure().GetCode())
}

func TestStreamMessageToProto(t *testing.T) {
	lease := domain.Lease{RunID: uuid.New(), Epoch: 7}

	assign := streamMessageToProto(streamMessage{Assign: &domain.Assign{Lease: lease, TaskName: "charge"}})
	assert.Equal(t, lease.RunID.String(), assign.GetAssign().GetLease().GetRunId())
	assert.Equal(t, int64(7), assign.GetAssign().GetLease().GetEpoch())
	assert.Equal(t, "charge", assign.GetAssign().GetTaskName())

	lost := streamMessageToProto(streamMessage{LeaseLost: &lease})
	assert.Equal(t, runcoilv1.Cancel_REASON_LEASE_LOST, lost.GetCancel().GetReason())

	assert.NotNil(t, streamMessageToProto(streamMessage{Keepalive: true}).GetKeepalive())
}

func TestCompleteStepParams(t *testing.T) {
	workerID, lease := uuid.New(), domain.Lease{RunID: uuid.New(), Epoch: 3}

	failedTry := completeStepParams(workerID, lease, &runcoilv1.CompleteStepRequest{
		StepId: "charge",
		Outcome: &runcoilv1.CompleteStepRequest_Error{Error: &runcoilv1.StepError{
			Message:      "rate limited",
			Kind:         runcoilv1.ErrorKind_ERROR_KIND_ERROR,
			NonRetriable: true,
			RetryAfter:   durationpb.New(30 * time.Second),
		}},
		Retry: &runcoilv1.RetryPolicy{MaxAttempts: 2},
	})

	after := 30 * time.Second
	assert.Equal(t, domain.CompleteStepParams{
		WorkerID: workerID,
		Lease:    lease,
		StepID:   "charge",
		Outcome:  domain.StepOutcome{Failure: &domain.Failure{Code: domain.FailureCodeError, Message: "rate limited"}},
		Retry:    domain.StepRetry{Policy: &domain.RetryPolicy{MaxAttempts: 2}, NonRetriable: true, After: &after},
	}, failedTry)

	succeeded := completeStepParams(workerID, lease, &runcoilv1.CompleteStepRequest{
		StepId:  "charge",
		Outcome: &runcoilv1.CompleteStepRequest_Output{Output: []byte(`1`)},
	})
	assert.Equal(t, domain.StepOutcome{Output: []byte(`1`)}, succeeded.Outcome)

	nul := completeStepParams(workerID, lease, &runcoilv1.CompleteStepRequest{
		StepId:  "charge",
		Outcome: &runcoilv1.CompleteStepRequest_Error{Error: &runcoilv1.StepError{Message: "bad\x00byte"}},
	})
	assert.Equal(t, "bad\uFFFDbyte", nul.Outcome.Failure.Message, "the message is cleaned")
	assert.Equal(t, domain.StepRetry{}, succeeded.Retry, "no policy, no marks")
}

func TestRetryPolicyFromProto(t *testing.T) {
	assert.Nil(t, retryPolicyFromProto(nil))

	got := retryPolicyFromProto(&runcoilv1.RetryPolicy{
		MaxAttempts: 3,
		Delays:      []*durationpb.Duration{durationpb.New(time.Second), durationpb.New(time.Minute)},
	})
	assert.Equal(t, &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second, time.Minute}}, got)
}

func TestStepResultToProto(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	retry := stepResultToProto(domain.StepResult{Retry: &domain.ScheduledRetry{NextAttemptAt: at, TryCount: 2}})
	assert.Equal(t, timestamppb.New(at).AsTime(), retry.GetRetryScheduled().GetNextAttemptAt().AsTime())
	assert.Equal(t, uint32(2), retry.GetRetryScheduled().GetTryCount())
	assert.Nil(t, retry.GetRecord())

	record := stepResultToProto(domain.StepResult{Record: domain.StepRecord{StepID: "charge", Kind: domain.StepKindDo, Output: []byte(`1`)}})
	assert.Equal(t, "charge", record.GetRecord().GetStepId())
	assert.Nil(t, record.GetRetryScheduled())
}

func TestScheduleFromProto(t *testing.T) {
	assert.Nil(t, scheduleFromProto(nil))
	assert.Equal(t, &domain.Schedule{Cron: "0 * * * *", Overlap: domain.OverlapSkip},
		scheduleFromProto(&runcoilv1.Schedule{Cron: "0 * * * *"}), "unset overlap skips")
	assert.Equal(t, &domain.Schedule{Cron: "0 * * * *", Overlap: domain.OverlapSkip},
		scheduleFromProto(&runcoilv1.Schedule{Cron: "0 * * * *", Overlap: runcoilv1.OverlapPolicy_OVERLAP_POLICY_SKIP}))
	assert.Equal(t, &domain.Schedule{Cron: "0 * * * *", Overlap: domain.OverlapAllow},
		scheduleFromProto(&runcoilv1.Schedule{Cron: "0 * * * *", Overlap: runcoilv1.OverlapPolicy_OVERLAP_POLICY_ALLOW}))
}

func TestCleanMessage(t *testing.T) {
	assert.Equal(t, "smtp down", cleanMessage("smtp down"))
	assert.Equal(t, "bad\uFFFDbyte", cleanMessage("bad\x00byte"))

	long := strings.Repeat("x", maxErrorMessageBytes+10)
	assert.Equal(t, long[:maxErrorMessageBytes], cleanMessage(long))

	// A three-byte character across the limit is dropped whole.
	cut := cleanMessage(strings.Repeat("x", maxErrorMessageBytes-1) + "\u20ac")
	assert.Len(t, cut, maxErrorMessageBytes-1)
	assert.True(t, utf8.ValidString(cut))

	// NUL grows to three bytes before the cut.
	nuls := cleanMessage(strings.Repeat("\x00", maxErrorMessageBytes))
	assert.LessOrEqual(t, len(nuls), maxErrorMessageBytes)
	assert.True(t, utf8.ValidString(nuls))
	assert.NotContains(t, nuls, "\x00")
}
