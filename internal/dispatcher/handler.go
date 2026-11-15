package dispatcher

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
	"uuid"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/proto/gen/go/runcoil/v1/runcoilv1connect"
)

var _ runcoilv1connect.WorkerServiceHandler = workerHandler{}

// workerHandler serves the WorkerService: it maps messages to the Service's calls and back.
type workerHandler struct {
	runcoilv1connect.UnimplementedWorkerServiceHandler

	s *Service
}

// Register takes the API key as its bearer token.
func (h workerHandler) Register(ctx context.Context, req *runcoilv1.RegisterRequest) (*runcoilv1.RegisterResponse, error) {
	apiKey, err := bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("read api key: %w", err)
	}

	result, err := h.s.Register(ctx, apiKey, registrationFromProto(req))
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	return &runcoilv1.RegisterResponse{
		WorkerId:          result.Worker.ID.String(),
		SessionToken:      result.SessionToken,
		HeartbeatInterval: durationpb.New(h.s.HeartbeatInterval()),
	}, nil
}

func (h workerHandler) Listen(
	ctx context.Context,
	req *runcoilv1.ListenRequest,
	out runcoilv1connect.WorkerServiceListenServerStream,
) error {
	worker, err := h.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	running := make([]domain.Lease, 0, len(req.GetRunning()))
	for _, l := range req.GetRunning() {
		lease, err := leaseFromProto(l)
		if err != nil {
			return fmt.Errorf("read running lease: %w", err)
		}

		running = append(running, lease)
	}

	err = h.s.Listen(ctx, worker, running, func(msg streamMessage) error {
		return out.Send(streamMessageToProto(msg))
	})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return nil
}

func (h workerHandler) Heartbeat(ctx context.Context, _ *runcoilv1.HeartbeatRequest) (*runcoilv1.HeartbeatResponse, error) {
	worker, err := h.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	h.s.Heartbeat(worker)

	return &runcoilv1.HeartbeatResponse{}, nil
}

func (h workerHandler) CompleteStep(
	ctx context.Context,
	req *runcoilv1.CompleteStepRequest,
) (*runcoilv1.CompleteStepResponse, error) {
	worker, err := h.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	lease, err := leaseFromProto(req.GetLease())
	if err != nil {
		return nil, fmt.Errorf("read lease: %w", err)
	}

	result, err := h.s.CompleteStep(ctx, completeStepParams(worker.ID, lease, req))
	if err != nil {
		return nil, fmt.Errorf("complete step: %w", err)
	}

	return stepResultToProto(result), nil
}

func (h workerHandler) CompleteRun(
	ctx context.Context,
	req *runcoilv1.CompleteRunRequest,
) (*runcoilv1.CompleteRunResponse, error) {
	worker, err := h.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	lease, err := leaseFromProto(req.GetLease())
	if err != nil {
		return nil, fmt.Errorf("read lease: %w", err)
	}

	result := domain.RunResult{Output: req.GetOutput()}
	if runErr := req.GetError(); runErr != nil {
		result = domain.RunResult{Failure: &domain.Failure{
			Code:    failureCodeFromProto(runErr.GetKind()),
			Message: cleanMessage(runErr.GetMessage()),
			StepID:  runErr.GetStepId(),
		}}
	}

	if err := h.s.CompleteRun(ctx, worker, lease, result); err != nil {
		return nil, fmt.Errorf("complete run: %w", err)
	}

	return &runcoilv1.CompleteRunResponse{}, nil
}

func (h workerHandler) Deregister(ctx context.Context, req *runcoilv1.DeregisterRequest) (*runcoilv1.DeregisterResponse, error) {
	worker, err := h.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	if err := h.s.Deregister(ctx, worker, req.GetReason()); err != nil {
		return nil, fmt.Errorf("deregister: %w", err)
	}

	return &runcoilv1.DeregisterResponse{}, nil
}

// authenticate returns the session of the request. Every call but Register takes the
// session token as its bearer token.
func (h workerHandler) authenticate(ctx context.Context) (domain.Worker, error) {
	token, err := bearerToken(ctx)
	if err != nil {
		return domain.Worker{}, fmt.Errorf("read session token: %w", err)
	}

	worker, err := h.s.Authenticate(ctx, token)
	if err != nil {
		return domain.Worker{}, fmt.Errorf("look up session: %w", err)
	}

	return worker, nil
}

// maxErrorMessageBytes is the longest error message stored.
const maxErrorMessageBytes = 64 << 10 // 64 KiB

// cleanMessage returns a worker's error message as it can be stored: each NUL byte
// becomes U+FFFD, and the text is cut to maxErrorMessageBytes at a character boundary.
func cleanMessage(message string) string {
	message = strings.ReplaceAll(message, "\x00", "\uFFFD")
	if len(message) <= maxErrorMessageBytes {
		return message
	}

	cut := maxErrorMessageBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}

	return message[:cut]
}

// scheduleFromProto returns nil for no schedule. An unset overlap policy skips overlaps.
func scheduleFromProto(s *runcoilv1.Schedule) *domain.Schedule {
	if s == nil {
		return nil
	}

	overlap := domain.OverlapSkip
	if s.GetOverlap() == runcoilv1.OverlapPolicy_OVERLAP_POLICY_ALLOW {
		overlap = domain.OverlapAllow
	}

	return &domain.Schedule{Cron: s.GetCron(), Overlap: overlap}
}

func registrationFromProto(req *runcoilv1.RegisterRequest) domain.Registration {
	reg := domain.Registration{
		App:   req.GetApp(),
		Slots: int(req.GetSlots()),
		Tasks: make([]domain.TaskDefinition, 0, len(req.GetTasks())),
	}

	for _, t := range req.GetTasks() {
		def := domain.TaskDefinition{Name: t.GetName()}
		if t.GetStartDeadline() != nil {
			d := t.GetStartDeadline().AsDuration()
			def.StartDeadline = &d
		}

		def.Retry = retryPolicyFromProto(t.GetRetry())
		def.Schedule = scheduleFromProto(t.GetSchedule())

		reg.Tasks = append(reg.Tasks, def)
	}

	return reg
}

// completeStepParams reads one try of a Step: its outcome, and what the worker says about
// retrying it.
func completeStepParams(workerID uuid.UUID, lease domain.Lease, req *runcoilv1.CompleteStepRequest) domain.CompleteStepParams {
	params := domain.CompleteStepParams{
		WorkerID: workerID,
		Lease:    lease,
		StepID:   req.GetStepId(),
		Outcome:  domain.StepOutcome{Output: req.GetOutput()},
		Retry:    domain.StepRetry{Policy: retryPolicyFromProto(req.GetRetry())},
	}

	if stepErr := req.GetError(); stepErr != nil {
		params.Outcome = domain.StepOutcome{Failure: &domain.Failure{
			Code:    failureCodeFromProto(stepErr.GetKind()),
			Message: cleanMessage(stepErr.GetMessage()),
		}}
		params.Retry.NonRetriable = stepErr.GetNonRetriable()

		if after := stepErr.GetRetryAfter(); after != nil {
			d := after.AsDuration()
			params.Retry.After = &d
		}
	}

	return params
}

// retryPolicyFromProto reads a retry policy. Nil stays nil: no policy.
func retryPolicyFromProto(p *runcoilv1.RetryPolicy) *domain.RetryPolicy {
	if p == nil {
		return nil
	}

	policy := &domain.RetryPolicy{MaxAttempts: int(p.GetMaxAttempts())}
	for _, d := range p.GetDelays() {
		policy.Delays = append(policy.Delays, d.AsDuration())
	}

	return policy
}

func stepResultToProto(r domain.StepResult) *runcoilv1.CompleteStepResponse {
	if r.Retry != nil {
		return &runcoilv1.CompleteStepResponse{Result: &runcoilv1.CompleteStepResponse_RetryScheduled{
			RetryScheduled: &runcoilv1.RetryScheduled{
				NextAttemptAt: timestamppb.New(r.Retry.NextAttemptAt),
				TryCount:      uint32(r.Retry.TryCount), //nolint:gosec // At most MaxRetryAttempts.
			},
		}}
	}

	return &runcoilv1.CompleteStepResponse{
		Result: &runcoilv1.CompleteStepResponse_Record{Record: stepRecordToProto(r.Record)},
	}
}

// leaseFromProto reads a lease. The request rules have checked that run_id is a UUID.
func leaseFromProto(l *runcoilv1.Lease) (domain.Lease, error) {
	runID, err := uuid.Parse(l.GetRunId())
	if err != nil {
		return domain.Lease{}, apperr.Invalid(apperr.CodeInvalidRequest, "lease run_id must be a UUID")
	}

	return domain.Lease{RunID: runID, Epoch: l.GetEpoch()}, nil
}

func leaseToProto(l domain.Lease) *runcoilv1.Lease {
	return &runcoilv1.Lease{RunId: l.RunID.String(), Epoch: l.Epoch}
}

func streamMessageToProto(msg streamMessage) *runcoilv1.ListenResponse {
	switch {
	case msg.Assign != nil:
		a := msg.Assign
		steps := make([]*runcoilv1.StepRecord, 0, len(a.Steps))
		for _, st := range a.Steps {
			steps = append(steps, stepRecordToProto(st))
		}

		return &runcoilv1.ListenResponse{Kind: &runcoilv1.ListenResponse_Assign{Assign: &runcoilv1.Assign{
			Lease:     leaseToProto(a.Lease),
			TaskName:  a.TaskName,
			Input:     a.Input,
			Steps:     steps,
			CreatedAt: timestamppb.New(a.CreatedAt),
		}}}
	case msg.LeaseLost != nil:
		return &runcoilv1.ListenResponse{Kind: &runcoilv1.ListenResponse_Cancel{Cancel: &runcoilv1.Cancel{
			Lease:  leaseToProto(*msg.LeaseLost),
			Reason: runcoilv1.Cancel_REASON_LEASE_LOST,
		}}}
	default:
		return &runcoilv1.ListenResponse{Kind: &runcoilv1.ListenResponse_Keepalive{Keepalive: &runcoilv1.Keepalive{}}}
	}
}

var failureCodes = map[domain.FailureCode]runcoilv1.Failure_Code{
	domain.FailureCodeError:              runcoilv1.Failure_CODE_ERROR,
	domain.FailureCodePanic:              runcoilv1.Failure_CODE_PANIC,
	domain.FailureCodeStepFailed:         runcoilv1.Failure_CODE_STEP_FAILED,
	domain.FailureCodeAttemptLost:        runcoilv1.Failure_CODE_ATTEMPT_LOST,
	domain.FailureCodeStartDeadline:      runcoilv1.Failure_CODE_START_DEADLINE,
	domain.FailureCodeOutputNotEncodable: runcoilv1.Failure_CODE_OUTPUT_NOT_ENCODABLE,
	domain.FailureCodeOutputTooLarge:     runcoilv1.Failure_CODE_OUTPUT_TOO_LARGE,
}

// failureCodeFromProto maps what a worker reports to a failure code. An unset kind is an error.
func failureCodeFromProto(kind runcoilv1.ErrorKind) domain.FailureCode {
	switch kind {
	case runcoilv1.ErrorKind_ERROR_KIND_PANIC:
		return domain.FailureCodePanic
	case runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_NOT_ENCODABLE:
		return domain.FailureCodeOutputNotEncodable
	case runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE:
		return domain.FailureCodeOutputTooLarge
	default:
		return domain.FailureCodeError
	}
}

func stepRecordToProto(r domain.StepRecord) *runcoilv1.StepRecord {
	record := &runcoilv1.StepRecord{StepId: r.StepID, Kind: runcoilv1.StepRecord_KIND_DO}
	if r.Failure != nil {
		record.Result = &runcoilv1.StepRecord_Failure{Failure: &runcoilv1.Failure{
			Code:    failureCodes[r.Failure.Code],
			Message: r.Failure.Message,
			StepId:  r.StepID,
		}}

		return record
	}

	record.Result = &runcoilv1.StepRecord_Output{Output: r.Output}

	return record
}
