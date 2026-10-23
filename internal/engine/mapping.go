package engine

import (
	"errors"
	"fmt"
	"math"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

var runStatusToProto = map[domain.RunStatus]runcoilv1.RunStatus{
	domain.RunStatusQueued:    runcoilv1.RunStatus_RUN_STATUS_QUEUED,
	domain.RunStatusRunning:   runcoilv1.RunStatus_RUN_STATUS_RUNNING,
	domain.RunStatusSucceeded: runcoilv1.RunStatus_RUN_STATUS_SUCCEEDED,
	domain.RunStatusFailed:    runcoilv1.RunStatus_RUN_STATUS_FAILED,
}

var queuedReasonToProto = map[domain.QueuedReason]runcoilv1.QueuedReason{
	domain.QueuedReasonWaitingForWorker:   runcoilv1.QueuedReason_QUEUED_REASON_WAITING_FOR_WORKER,
	domain.QueuedReasonWaitingForCapacity: runcoilv1.QueuedReason_QUEUED_REASON_WAITING_FOR_CAPACITY,
	domain.QueuedReasonRetryBackoff:       runcoilv1.QueuedReason_QUEUED_REASON_RETRY_BACKOFF,
}

var runSourceToProto = map[domain.RunSource]runcoilv1.RunSource{
	domain.RunSourceInvoke:   runcoilv1.RunSource_RUN_SOURCE_INVOKE,
	domain.RunSourceSchedule: runcoilv1.RunSource_RUN_SOURCE_SCHEDULE,
	domain.RunSourceRerun:    runcoilv1.RunSource_RUN_SOURCE_RERUN,
}

// runStatusFromProto maps the statuses a ListRuns filter may name.
var runStatusFromProto = map[runcoilv1.RunStatus]domain.RunStatus{
	runcoilv1.RunStatus_RUN_STATUS_QUEUED:    domain.RunStatusQueued,
	runcoilv1.RunStatus_RUN_STATUS_RUNNING:   domain.RunStatusRunning,
	runcoilv1.RunStatus_RUN_STATUS_SUCCEEDED: domain.RunStatusSucceeded,
	runcoilv1.RunStatus_RUN_STATUS_FAILED:    domain.RunStatusFailed,
}

var stepStatusToProto = map[domain.StepStatus]runcoilv1.StepStatus{
	domain.StepStatusSucceeded: runcoilv1.StepStatus_STEP_STATUS_SUCCEEDED,
	domain.StepStatusFailed:    runcoilv1.StepStatus_STEP_STATUS_FAILED,
}

var failureCodeToProto = map[domain.FailureCode]runcoilv1.FailureCode{
	domain.FailureCodeError:              runcoilv1.FailureCode_FAILURE_CODE_ERROR,
	domain.FailureCodePanic:              runcoilv1.FailureCode_FAILURE_CODE_PANIC,
	domain.FailureCodeStepFailed:         runcoilv1.FailureCode_FAILURE_CODE_STEP_FAILED,
	domain.FailureCodeAttemptLost:        runcoilv1.FailureCode_FAILURE_CODE_ATTEMPT_LOST,
	domain.FailureCodeOutputNotEncodable: runcoilv1.FailureCode_FAILURE_CODE_OUTPUT_NOT_ENCODABLE,
	domain.FailureCodeOutputTooLarge:     runcoilv1.FailureCode_FAILURE_CODE_OUTPUT_TOO_LARGE,
	domain.FailureCodeRunDataLimit:       runcoilv1.FailureCode_FAILURE_CODE_RUN_DATA_LIMIT,
	domain.FailureCodeWorkerWaitLimit:    runcoilv1.FailureCode_FAILURE_CODE_WORKER_WAIT_LIMIT,
	domain.FailureCodeStartDeadline:      runcoilv1.FailureCode_FAILURE_CODE_START_DEADLINE,
}

// internalMessage is the only text a client sees for an unexpected failure.
const internalMessage = "internal error"

// errorCodes maps every API code to its schema ErrorCode. A code missing from
// this map would reach the client as ERROR_CODE_UNSPECIFIED; a test checks that none does.
var errorCodes = map[apierr.Code]runcoilv1.ErrorCode{
	apierr.CodeInvalidRequest:       runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST,
	apierr.CodeUnauthenticated:      runcoilv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED,
	apierr.CodeIncompatibleProtocol: runcoilv1.ErrorCode_ERROR_CODE_INCOMPATIBLE_PROTOCOL,
	apierr.CodeInvalidRegistration:  runcoilv1.ErrorCode_ERROR_CODE_INVALID_REGISTRATION,
	apierr.CodeTaskNotFound:         runcoilv1.ErrorCode_ERROR_CODE_TASK_NOT_FOUND,
	apierr.CodeRunNotFound:          runcoilv1.ErrorCode_ERROR_CODE_RUN_NOT_FOUND,
	apierr.CodeIdempotencyConflict:  runcoilv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT,
	apierr.CodeInputTooLarge:        runcoilv1.ErrorCode_ERROR_CODE_INPUT_TOO_LARGE,
	apierr.CodeNotRegistered:        runcoilv1.ErrorCode_ERROR_CODE_NOT_REGISTERED,
	apierr.CodeStaleLease:           runcoilv1.ErrorCode_ERROR_CODE_STALE_LEASE,
	apierr.CodeRunNotFinished:       runcoilv1.ErrorCode_ERROR_CODE_RUN_NOT_FINISHED,
	apierr.CodeStepNotFound:         runcoilv1.ErrorCode_ERROR_CODE_STEP_NOT_FOUND,
}

// failureCodeFromProto maps a code of a request to a domain code. An unset or
// unknown code is ERROR. The domain then accepts only the codes a worker may report.
func failureCodeFromProto(code runcoilv1.FailureCode) domain.FailureCode {
	for domainCode, protoCode := range failureCodeToProto {
		if protoCode == code {
			return domainCode
		}
	}

	return domain.FailureCodeError
}

// scheduleFromProto converts and checks a schedule. An unset schedule gives nil.
// A schedule must set exactly one kind, and each kind must be valid.
func scheduleFromProto(s *runcoilv1.Schedule) (*domain.Schedule, error) {
	if s == nil {
		return nil, nil
	}

	switch kind := s.GetKind().(type) {
	case *runcoilv1.Schedule_Cron:
		if _, err := domain.ParseCron(kind.Cron); err != nil {
			return nil, errors.New("cron expression is not valid: " + err.Error())
		}

		return &domain.Schedule{
			Kind:        domain.ScheduleKindCron,
			Cron:        kind.Cron,
			SkipOverlap: s.GetSkipOverlap(),
		}, nil
	case *runcoilv1.Schedule_Every:
		every := kind.Every.AsDuration()
		if every < time.Second {
			return nil, errors.New("every must be at least 1 second")
		}

		return &domain.Schedule{
			Kind:        domain.ScheduleKindEvery,
			Every:       every,
			SkipOverlap: s.GetSkipOverlap(),
		}, nil
	case *runcoilv1.Schedule_Daily:
		if len(kind.Daily.GetAt()) == 0 {
			return nil, errors.New("daily needs at least one time")
		}

		schedule := &domain.Schedule{
			Kind:        domain.ScheduleKindDaily,
			SkipOverlap: s.GetSkipOverlap(),
		}

		for _, at := range kind.Daily.GetAt() {
			if at.GetHour() > 23 || at.GetMinute() > 59 {
				return nil, errors.New("daily time must have hour 0 to 23 and minute 0 to 59")
			}

			schedule.Daily = append(schedule.Daily, domain.TimeOfDay{
				Hour:   int(at.GetHour()),
				Minute: int(at.GetMinute()),
			})
		}

		return schedule, nil
	default:
		return nil, errors.New("schedule must set one of cron, every, or daily")
	}
}

// retryFromProto converts and checks a retry policy. An unset policy gives nil.
func retryFromProto(r *runcoilv1.RetryPolicy) (*domain.RetryPolicy, error) {
	if r == nil {
		return nil, nil
	}

	policy := &domain.RetryPolicy{MaxAttempts: int(r.GetMaxAttempts())}
	for _, delay := range r.GetDelays() {
		d := delay.AsDuration()
		if d <= 0 {
			return nil, errors.New("retry delays must be greater than 0")
		}

		policy.Delays = append(policy.Delays, d)
	}

	return policy, nil
}

// startDeadlineFromProto converts and checks a start deadline. An unset deadline is 0.
func startDeadlineFromProto(d *durationpb.Duration) (time.Duration, error) {
	if d == nil {
		return 0, nil
	}

	if d.AsDuration() <= 0 {
		return 0, errors.New("start deadline must be greater than 0")
	}

	return d.AsDuration(), nil
}

// attemptErrorFromProto reads one failed try. Its code is normalized by the domain.
func attemptErrorFromProto(e *runcoilv1.AttemptError) domain.AttemptError {
	return domain.AttemptError{
		Message:    e.GetMessage(),
		Permanent:  e.GetPermanent(),
		RetryAfter: e.GetRetryAfter().AsDuration(),
		Code:       failureCodeFromProto(e.GetCode()),
	}
}

// finishInputFromProto reads a FinishRun request. A request without a result is an error.
func finishInputFromProto(msg *runcoilv1.FinishRunRequest) (domain.FinishInput, error) {
	in := domain.FinishInput{Token: msg.GetLeaseToken()}
	switch result := msg.GetResult().(type) {
	case *runcoilv1.FinishRunRequest_Succeeded:
		in.Kind = domain.FinishSucceeded
		in.Output = result.Succeeded.GetOutput()
	case *runcoilv1.FinishRunRequest_Failed:
		in.Kind = domain.FinishFailed
		in.Error = attemptErrorFromProto(result.Failed.GetError())
		in.StepID = result.Failed.GetStepId()
	case *runcoilv1.FinishRunRequest_Released:
		in.Kind = domain.FinishReleased
	default:
		return domain.FinishInput{}, errors.New("finish run needs a result")
	}

	return in, nil
}

// stepReportFromProto reads a RecordStep request. A request without an outcome is an error.
func stepReportFromProto(msg *runcoilv1.RecordStepRequest) (domain.StepReport, error) {
	policy, err := retryFromProto(msg.GetRetry())
	if err != nil {
		return domain.StepReport{}, err
	}

	rep := domain.StepReport{
		StepID:     msg.GetStepId(),
		Attempt:    int(msg.GetStepAttempt()),
		StepPolicy: policy,
	}

	switch outcome := msg.GetOutcome().(type) {
	case *runcoilv1.RecordStepRequest_Succeeded:
		rep.Succeeded = true
		rep.Output = outcome.Succeeded.GetOutput()
	case *runcoilv1.RecordStepRequest_Failed:
		rep.Error = attemptErrorFromProto(outcome.Failed)
	default:
		return domain.StepReport{}, errors.New("record step needs an outcome")
	}

	return rep, nil
}

// runToProto converts a Run. Its Steps are not filled in; the caller adds them when asked.
func runToProto(run domain.Run) *runcoilv1.Run {
	out := &runcoilv1.Run{
		RunId:        run.ID,
		WorkerId:     run.WorkerID,
		TaskId:       run.TaskID,
		Status:       runStatusToProto[run.Status],
		QueuedReason: queuedReasonToProto[run.QueuedReason],
		Terminal:     run.Status.Terminal(),
		Source:       runSourceToProto[run.Source],
		Attempts:     clampUint32(run.Attempts),
		Input:        run.Input,
		Output:       run.Output,
		CreatedAt:    timestampOrNil(run.CreatedAt),
		DueAt:        timestampOrNil(run.DueAt),
		StartedAt:    timestampOrNil(run.StartedAt),
		FinishedAt:   timestampOrNil(run.FinishedAt),
		InstanceId:   run.InstanceID,
	}

	if run.Failure != nil {
		out.Failure = failureToProto(*run.Failure)
	}

	if run.Status == domain.RunStatusQueued {
		out.NextAttemptAt = timestampOrNil(run.NextAttemptAt)
	}

	if run.RerunOf != "" {
		out.RerunOf = run.RerunOf
	}

	return out
}

// clampUint32 converts a count to the schema's uint32. A negative count gives 0,
// and a count above the uint32 range gives the largest value.
func clampUint32(n int) uint32 {
	if n <= 0 {
		return 0
	}

	if int64(n) > math.MaxUint32 {
		return math.MaxUint32
	}

	return uint32(n) //nolint:gosec // n is within the uint32 range here.
}

// timestampOrNil returns nil for a zero time, which the schema leaves unset.
func timestampOrNil(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}

func failureToProto(f domain.Failure) *runcoilv1.Failure {
	return &runcoilv1.Failure{
		Code:     failureCodeToProto[f.Code],
		Message:  f.Message,
		Attempts: clampUint32(f.Attempts),
		StepId:   f.StepID,
	}
}

// stepsToProto converts finished Step records. The caller passes only finished ones.
func stepsToProto(steps []domain.StepRecord) []*runcoilv1.StepRecord {
	out := make([]*runcoilv1.StepRecord, 0, len(steps))
	for _, step := range steps {
		out = append(out, stepRecordToProto(step))
	}

	return out
}

func stepRecordToProto(step domain.StepRecord) *runcoilv1.StepRecord {
	out := &runcoilv1.StepRecord{
		StepId: step.StepID,
		Index:  clampUint32(step.Position),
		Status: stepStatusToProto[step.Status],
		Output: step.Output,
	}

	if step.Failure != nil {
		out.Failure = failureToProto(*step.Failure)
	}

	return out
}

func retryScheduledToProto(r domain.RetryAnswer) *runcoilv1.RetryScheduled {
	return &runcoilv1.RetryScheduled{
		NotBefore:    timestamppb.New(r.NotBefore),
		AttemptsUsed: clampUint32(r.AttemptsUsed),
	}
}

// dispatchMessage is the message that gives a claimed attempt to its worker. It
// carries the finished Steps of the Run, and the Steps that have failed tries.
func dispatchMessage(run domain.Run, steps []domain.StepRecord) *runcoilv1.ListenResponse {
	dispatch := &runcoilv1.Dispatch{
		RunId:          run.ID,
		Attempt:        clampUint32(run.Attempts),
		LeaseToken:     run.LeaseToken,
		LeaseExpiresAt: timestamppb.New(run.LeaseExpiresAt),
		TaskId:         run.TaskID,
		Input:          run.Input,
		Source:         runSourceToProto[run.Source],
		DueAt:          timestamppb.New(run.DueAt),
		Steps:          stepsToProto(finishedSteps(steps)),
	}

	for _, step := range steps {
		if step.Status == domain.StepStatusPending {
			dispatch.Progress = append(dispatch.Progress, &runcoilv1.StepProgress{
				StepId:       step.StepID,
				AttemptsUsed: clampUint32(step.AttemptsUsed),
			})
		}
	}

	return &runcoilv1.ListenResponse{
		Message: &runcoilv1.ListenResponse_Dispatch{Dispatch: dispatch},
	}
}

// taskInfoToProto converts a registered Task for ListTasks.
func taskInfoToProto(l taskListing) *runcoilv1.TaskInfo {
	info := &runcoilv1.TaskInfo{
		WorkerId:     l.task.WorkerID,
		TaskId:       l.task.TaskID,
		Schedule:     scheduleToProto(l.task.Schedule),
		Retry:        retryToProto(l.task.Retry),
		RegisteredAt: timestampOrNil(l.task.RegisteredAt),
		NextRunAt:    timestampOrNil(l.task.NextRunAt),
		ReadyWorkers: clampUint32(l.readyWorkers),
	}

	if l.task.StartDeadline > 0 {
		info.StartDeadline = durationpb.New(l.task.StartDeadline)
	}

	return info
}

// scheduleToProto converts a schedule. A Task without one gives nil.
func scheduleToProto(s *domain.Schedule) *runcoilv1.Schedule {
	if s == nil {
		return nil
	}

	out := &runcoilv1.Schedule{SkipOverlap: s.SkipOverlap}
	switch s.Kind {
	case domain.ScheduleKindCron:
		out.Kind = &runcoilv1.Schedule_Cron{Cron: s.Cron}
	case domain.ScheduleKindEvery:
		out.Kind = &runcoilv1.Schedule_Every{Every: durationpb.New(s.Every)}
	case domain.ScheduleKindDaily:
		daily := &runcoilv1.DailyTimes{}
		for _, at := range s.Daily {
			daily.At = append(daily.At, &runcoilv1.TimeOfDay{
				Hour:   clampUint32(at.Hour),
				Minute: clampUint32(at.Minute),
			})
		}

		out.Kind = &runcoilv1.Schedule_Daily{Daily: daily}
	}

	return out
}

// retryToProto converts a retry policy. A Task without one gives nil.
func retryToProto(r *domain.RetryPolicy) *runcoilv1.RetryPolicy {
	if r == nil {
		return nil
	}

	out := &runcoilv1.RetryPolicy{MaxAttempts: clampUint32(r.MaxAttempts)}
	for _, d := range r.Delays {
		out.Delays = append(out.Delays, durationpb.New(d))
	}

	return out
}

// cancelMessage tells the worker to stop an attempt whose result will not be accepted.
func cancelMessage(id string, token string) *runcoilv1.ListenResponse {
	return &runcoilv1.ListenResponse{
		Message: &runcoilv1.ListenResponse_Cancel{
			Cancel: &runcoilv1.CancelAttempt{RunId: id, LeaseToken: token},
		},
	}
}

// keepaliveMessage is sent on a stream every heartbeat interval.
func keepaliveMessage() *runcoilv1.ListenResponse {
	return &runcoilv1.ListenResponse{
		Message: &runcoilv1.ListenResponse_Keepalive{Keepalive: &runcoilv1.Keepalive{}},
	}
}

// toConnectError maps an apierr.Error to a ConnectRPC error. The Kind chooses the
// code. An error with a schema code also carries an ErrorDetail with that code and
// the IDs in its details.
func toConnectError(e *apierr.Error) (*connect.Error, error) {
	connectErr := connect.NewError(connectCodeOf(e.Kind), e.Message)
	if e.Code == "" {
		return connectErr, nil
	}

	detail, err := connectproto.NewErrorDetail(&runcoilv1.ErrorDetail{
		Code:   errorCodeOf(e.Code),
		RunId:  e.Details["run_id"],
		TaskId: e.Details["task_id"],
	})
	if err != nil {
		return nil, fmt.Errorf("new error detail: %w", err)
	}

	return connectErr.WithDetail(detail), nil
}

// connectCodeOf maps a Kind to a ConnectRPC code.
func connectCodeOf(kind apierr.Kind) connect.Code {
	switch kind {
	case apierr.KindInvalid:
		return connect.CodeInvalidArgument
	case apierr.KindUnauthenticated:
		return connect.CodeUnauthenticated
	case apierr.KindNotFound:
		return connect.CodeNotFound
	case apierr.KindConflict:
		return connect.CodeAlreadyExists
	case apierr.KindFailedPrecondition:
		return connect.CodeFailedPrecondition
	case apierr.KindUnavailable:
		return connect.CodeUnavailable
	case apierr.KindCanceled:
		return connect.CodeCanceled
	case apierr.KindDeadlineExceeded:
		return connect.CodeDeadlineExceeded
	default:
		return connect.CodeInternal
	}
}

// errorCodeOf returns the schema ErrorCode of an API code. An unknown code gives
// ERROR_CODE_UNSPECIFIED.
func errorCodeOf(code apierr.Code) runcoilv1.ErrorCode {
	return errorCodes[code]
}

// runFilterFromProto reads the filters of a ListRuns request. A status that is not one of
// the four is refused, so a typo does not silently list everything.
func runFilterFromProto(msg *runcoilv1.ListRunsRequest) (domain.RunFilter, error) {
	filter := domain.RunFilter{
		WorkerID: msg.GetWorkerId(),
		TaskID:   msg.GetTaskId(),
	}

	for _, status := range msg.GetStatuses() {
		s, ok := runStatusFromProto[status]
		if !ok {
			return domain.RunFilter{}, apierr.Invalid(apierr.CodeInvalidRequest, "statuses must be queued, running, succeeded, or failed")
		}

		filter.Statuses = append(filter.Statuses, s)
	}

	if t := msg.GetCreatedAfter(); t != nil {
		filter.CreatedAfter = t.AsTime()
	}

	if t := msg.GetCreatedBefore(); t != nil {
		filter.CreatedBefore = t.AsTime()
	}

	return filter, nil
}

// requireRunID refuses an empty run_id. Any other text is looked up as a Run ID, and
// an ID that names no Run is not found.
func requireRunID(s string) error {
	if s == "" {
		return apierr.Invalid(apierr.CodeInvalidRequest, "run_id is required")
	}

	return nil
}

// maxIDBytes is the longest identifier that a caller or a worker supplies and the server
// stores: a worker ID, a Task ID, an instance ID, or a Step ID.
const maxIDBytes = 255

// requireShortID refuses an identifier longer than maxIDBytes. The field names the value
// in the message, such as attempts[1].step_id. An empty identifier passes, because each
// caller checks the identifiers it requires.
func requireShortID(field, id string) error {
	if len(id) > maxIDBytes {
		return apierr.Invalid(apierr.CodeInvalidRequest, fmt.Sprintf("%s is longer than %d bytes", field, maxIDBytes))
	}

	return nil
}

// runError translates a store or decision error about one Run into the API error.
// Any other error is returned as it is, for the transport to log.
func (s *Service) runError(id string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return apierr.NotFound(apierr.CodeRunNotFound, "run not found").WithDetail("run_id", id)
	case errors.Is(err, domain.ErrStaleLease):
		return staleLease(id)
	case errors.Is(err, domain.ErrStepOrder):
		return apierr.Invalid(
			apierr.CodeInvalidRequest,
			"step_attempt is out of order, or the step is already final",
		)
	default:
		return err
	}
}

// staleLease is the answer for a report whose lease is no longer the Run's current one.
func staleLease(id string) *apierr.Error {
	return apierr.FailedPrecondition(apierr.CodeStaleLease, "the lease of this attempt is no longer current").
		WithDetail("run_id", id)
}
