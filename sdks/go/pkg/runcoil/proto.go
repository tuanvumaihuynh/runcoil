package runcoil

import (
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/sdks/go/internal/runstate"
)

// runResultToProto turns a handler's output or error into the result CompleteRun sends.
func runResultToProto(out []byte, err error) *runcoilv1.CompleteRunRequest {
	if err == nil {
		return &runcoilv1.CompleteRunRequest{Result: &runcoilv1.CompleteRunRequest_Output{Output: out}}
	}

	var se *runstate.StepError
	if errors.As(err, &se) {
		// The handler returned a Step's final error.
		return runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR, err.Error(), se.StepID)
	}

	var pe *panicError
	if errors.As(err, &pe) {
		return runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_PANIC, pe.Error(), "")
	}

	var ee *encodeError
	if errors.As(err, &ee) {
		return runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_NOT_ENCODABLE, ee.Error(), "")
	}

	return runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR, err.Error(), "")
}

// runFailureToProto builds a result that fails the Run. stepID is set when the handler
// returned the final error of that Step.
func runFailureToProto(kind runcoilv1.ErrorKind, message, stepID string) *runcoilv1.CompleteRunRequest {
	return &runcoilv1.CompleteRunRequest{Result: &runcoilv1.CompleteRunRequest_Error{
		Error: &runcoilv1.RunError{Kind: kind, Message: cutMessage(message), StepId: strings.ToValidUTF8(stepID, "\uFFFD")},
	}}
}

// maxMessageBytes is the longest error message the server stores. A longer one is cut
// before it is sent, so a huge error text does not make the report too large.
const maxMessageBytes = 64 << 10 // 64 KiB

// cutMessage makes message fit a report: invalid UTF-8, which a proto string cannot
// carry, becomes U+FFFD, and the text is cut to maxMessageBytes at a character boundary.
func cutMessage(message string) string {
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) <= maxMessageBytes {
		return message
	}

	cut := maxMessageBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}

	return message[:cut]
}

func stepReportToProto(lease *runcoilv1.Lease, r runstate.StepReport) *runcoilv1.CompleteStepRequest {
	req := &runcoilv1.CompleteStepRequest{
		Lease:     lease,
		StepId:    r.StepID,
		Retry:     retryPolicyToProto(r.Retry),
		StartedAt: timestamppb.New(r.StartedAt),
		EndedAt:   timestamppb.New(r.EndedAt),
	}

	if r.Failure != nil {
		stepErr := &runcoilv1.StepError{
			Kind:         errorKindToProto(r.Failure.Kind),
			Message:      cutMessage(r.Failure.Message),
			NonRetriable: r.Failure.NonRetriable,
		}
		if r.Failure.RetryAfter != nil {
			stepErr.RetryAfter = durationpb.New(*r.Failure.RetryAfter)
		}

		req.Outcome = &runcoilv1.CompleteStepRequest_Error{Error: stepErr}

		return req
	}

	req.Outcome = &runcoilv1.CompleteStepRequest_Output{Output: r.Output}

	return req
}

// retryPolicyToProto converts a retry policy. Nil stays nil: the Task's policy applies.
func retryPolicyToProto(p *runstate.RetryPolicy) *runcoilv1.RetryPolicy {
	if p == nil {
		return nil
	}

	policy := &runcoilv1.RetryPolicy{MaxAttempts: uint32(p.MaxAttempts)} //nolint:gosec // Validate keeps it at most 30.
	for _, d := range p.Delays {
		policy.Delays = append(policy.Delays, durationpb.New(d))
	}

	return policy
}

// taskDefinitionToProto converts what NewTask declared for a Task.
func taskDefinitionToProto(t *task) *runcoilv1.TaskDefinition {
	def := &runcoilv1.TaskDefinition{Name: t.name}
	if r := t.cfg.retry; r != nil {
		def.Retry = retryPolicyToProto(&runstate.RetryPolicy{MaxAttempts: r.MaxAttempts, Delays: r.Delays})
	}

	if d := t.cfg.startDeadline; d != nil {
		def.StartDeadline = durationpb.New(*d)
	}

	if s := t.cfg.schedule; s != nil {
		def.Schedule = &runcoilv1.Schedule{Cron: s.cron, Overlap: runcoilv1.OverlapPolicy_OVERLAP_POLICY_SKIP}
		if s.allowOverlap {
			def.Schedule.Overlap = runcoilv1.OverlapPolicy_OVERLAP_POLICY_ALLOW
		}
	}

	return def
}

// stepRecordsFromProto reads the journal an Assign carries, by Step ID.
func stepRecordsFromProto(steps []*runcoilv1.StepRecord) map[string]runstate.Record {
	recorded := make(map[string]runstate.Record, len(steps))
	for _, rec := range steps {
		recorded[rec.GetStepId()] = stepRecordFromProto(rec)
	}

	return recorded
}

func stepRecordFromProto(rec *runcoilv1.StepRecord) runstate.Record {
	failure := rec.GetFailure()
	if failure == nil {
		return runstate.Record{Succeeded: true, Output: rec.GetOutput()}
	}

	return runstate.Record{Kind: failureKindFromProto(failure.GetCode()), Message: failure.GetMessage()}
}

func errorKindToProto(kind string) runcoilv1.ErrorKind {
	switch kind {
	case runstate.KindPanic:
		return runcoilv1.ErrorKind_ERROR_KIND_PANIC
	case runstate.KindOutputNotEncodable:
		return runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_NOT_ENCODABLE
	case runstate.KindOutputTooLarge:
		return runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE
	default:
		return runcoilv1.ErrorKind_ERROR_KIND_ERROR
	}
}

// errorKindFromProto names the kind of a failed Run for the logs.
func errorKindFromProto(kind runcoilv1.ErrorKind) string {
	switch kind {
	case runcoilv1.ErrorKind_ERROR_KIND_PANIC:
		return runstate.KindPanic
	case runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_NOT_ENCODABLE:
		return runstate.KindOutputNotEncodable
	case runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE:
		return runstate.KindOutputTooLarge
	default:
		return runstate.KindError
	}
}

// failureKindFromProto maps a stored failure code to the kind a Step error carries. A
// code this SDK does not know is an error.
func failureKindFromProto(code runcoilv1.Failure_Code) string {
	switch code {
	case runcoilv1.Failure_CODE_PANIC:
		return runstate.KindPanic
	case runcoilv1.Failure_CODE_OUTPUT_NOT_ENCODABLE:
		return runstate.KindOutputNotEncodable
	case runcoilv1.Failure_CODE_OUTPUT_TOO_LARGE:
		return runstate.KindOutputTooLarge
	default:
		return runstate.KindError
	}
}
