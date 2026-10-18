package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

var _ runcoilv1connect.WorkerServiceHandler = workerHandler{}

// workerHandler serves the WorkerService, the calls that a worker makes. It maps
// messages to engine calls and back, and holds no logic of its own.
type workerHandler struct {
	s *Service
}

// protocolVersion is the only version of the worker protocol this server speaks.
const protocolVersion = 1

// Register declares the Tasks of the worker and the instance's slots.
func (h workerHandler) Register(
	ctx context.Context,
	req *runcoilv1.RegisterRequest,
) (*runcoilv1.RegisterResponse, error) {
	reg, err := registrationFromProto(req, h.s.clock())
	if err != nil {
		return nil, err
	}

	if err := h.s.register(ctx, reg); err != nil {
		return nil, err
	}

	return &runcoilv1.RegisterResponse{
		HeartbeatInterval: durationpb.New(h.s.cfg.HeartbeatInterval),
		LeaseDuration:     durationpb.New(h.s.cfg.LeaseDuration),
	}, nil
}

// Listen opens the stream of an instance. The server sends dispatches and keepalive on it.
func (h workerHandler) Listen(
	ctx context.Context,
	req *runcoilv1.ListenRequest,
	out runcoilv1connect.WorkerServiceListenServerStream,
) error {
	instanceID := req.GetInstanceId()
	sess, err := h.s.attachStream(ctx, instanceID)
	if err != nil {
		return err
	}

	attachedAt := h.s.clock()
	h.s.logger.DebugContext(ctx, "listen started", slog.String("instance_id", instanceID))
	defer func() {
		h.s.detachStream(ctx, instanceID, sess, attachedAt, ctx.Err() != nil)
	}()

	return serveStream(ctx, sess, out, h.s.cfg.HeartbeatInterval)
}

// Heartbeat renews the leases of the attempts the worker names, and returns the ones that are stale.
func (h workerHandler) Heartbeat(
	ctx context.Context,
	req *runcoilv1.HeartbeatRequest,
) (*runcoilv1.HeartbeatResponse, error) {
	var attempts []domain.AttemptRef
	var stale []*runcoilv1.AttemptRef
	for i, attempt := range req.GetAttempts() {
		// An over-long Step ID is logged and not stored: the attempt holds no running Step, and
		// its lease is still renewed, as with an invalid policy, so no other lease is held back.
		stepID := attempt.GetStepId()
		if err := requireShortID(fmt.Sprintf("attempts[%d].step_id", i), stepID); err != nil {
			h.s.logger.WarnContext(
				ctx,
				"heartbeat step id ignored",
				slog.String("run_id", attempt.GetRunId()),
				slog.Int("step_id_bytes", len(stepID)),
				slog.Any("error", err),
			)

			stepID = ""
		}

		// An invalid policy on one attempt is logged and ignored: the attempt's lease is
		// still renewed, so the rest of the instance's leases are not held back.
		policy, err := retryFromProto(attempt.GetRetry())
		if err != nil {
			h.s.logger.WarnContext(
				ctx,
				"heartbeat step policy ignored",
				slog.String("run_id", attempt.GetRunId()),
				slog.String("step_id", attempt.GetStepId()),
				slog.Any("error", err),
			)

			policy = nil
		}

		attempts = append(attempts, domain.AttemptRef{
			RunID:       attempt.GetRunId(),
			Token:       attempt.GetLeaseToken(),
			StepID:      stepID,
			StepAttempt: int(attempt.GetStepAttempt()),
			Retry:       policy,
		})
	}

	staleRefs, expiresAt, err := h.s.heartbeat(ctx, req.GetInstanceId(), attempts)
	if err != nil {
		return nil, err
	}

	for _, ref := range staleRefs {
		stale = append(stale, &runcoilv1.AttemptRef{RunId: ref.RunID, LeaseToken: ref.Token})
	}

	return &runcoilv1.HeartbeatResponse{
		Stale:          stale,
		LeasesExpireAt: timestamppb.New(expiresAt),
	}, nil
}

// AckDispatch records that the worker owns the attempt.
func (h workerHandler) AckDispatch(
	ctx context.Context,
	req *runcoilv1.AckDispatchRequest,
) (*runcoilv1.AckDispatchResponse, error) {
	if err := requireRunID(req.GetRunId()); err != nil {
		return nil, err
	}

	if err := h.s.ackDispatch(ctx, req.GetRunId(), req.GetLeaseToken()); err != nil {
		return nil, err
	}

	return &runcoilv1.AckDispatchResponse{}, nil
}

// RecordStep stores one try of one Step, and answers with the record or with a retry.
func (h workerHandler) RecordStep(
	ctx context.Context,
	req *runcoilv1.RecordStepRequest,
) (*runcoilv1.RecordStepResponse, error) {
	if err := requireRunID(req.GetRunId()); err != nil {
		return nil, err
	}

	rep, err := stepReportFromProto(req)
	if err != nil {
		return nil, apierr.Invalid(apierr.CodeInvalidRequest, err.Error())
	}

	answer, err := h.s.recordStep(ctx, req.GetRunId(), req.GetLeaseToken(), rep)
	if err != nil {
		return nil, err
	}

	if answer.Retry != nil {
		return &runcoilv1.RecordStepResponse{
			Result: &runcoilv1.RecordStepResponse_Retry{Retry: retryScheduledToProto(*answer.Retry)},
		}, nil
	}

	return &runcoilv1.RecordStepResponse{
		Result: &runcoilv1.RecordStepResponse_Recorded{Recorded: stepRecordToProto(*answer.Record)},
	}, nil
}

// FinishRun ends the attempt with the result the worker reports.
func (h workerHandler) FinishRun(
	ctx context.Context,
	req *runcoilv1.FinishRunRequest,
) (*runcoilv1.FinishRunResponse, error) {
	if err := requireRunID(req.GetRunId()); err != nil {
		return nil, err
	}

	in, err := finishInputFromProto(req)
	if err != nil {
		return nil, apierr.Invalid(apierr.CodeInvalidRequest, err.Error())
	}

	answer, err := h.s.finishRun(ctx, req.GetRunId(), in)
	if err != nil {
		return nil, err
	}

	if answer.Retry != nil {
		return &runcoilv1.FinishRunResponse{
			Result: &runcoilv1.FinishRunResponse_Retry{Retry: retryScheduledToProto(*answer.Retry)},
		}, nil
	}

	return &runcoilv1.FinishRunResponse{
		Result: &runcoilv1.FinishRunResponse_Finished{Finished: runToProto(answer.Run)},
	}, nil
}

// registrationFromProto validates a Register request and converts it. The whole
// request is checked before anything is returned, so a request that fails
// validation registers nothing.
func registrationFromProto(msg *runcoilv1.RegisterRequest, now time.Time) (domain.Registration, error) {
	if msg.GetProtocolVersion() != protocolVersion {
		return domain.Registration{}, apierr.FailedPrecondition(
			apierr.CodeIncompatibleProtocol,
			fmt.Sprintf("protocol version %d is not supported, the server speaks version %d",
				msg.GetProtocolVersion(), protocolVersion),
		)
	}

	if msg.GetWorkerId() == "" {
		return domain.Registration{}, apierr.Invalid(apierr.CodeInvalidRequest, "worker_id is required")
	}

	if err := requireShortID("worker_id", msg.GetWorkerId()); err != nil {
		return domain.Registration{}, invalidRegistration(err.Error(), "")
	}

	if msg.GetInstanceId() == "" {
		return domain.Registration{}, apierr.Invalid(apierr.CodeInvalidRequest, "instance_id is required")
	}

	if err := requireShortID("instance_id", msg.GetInstanceId()); err != nil {
		return domain.Registration{}, invalidRegistration(err.Error(), "")
	}

	if msg.GetMaxConcurrentAttempts() < 1 {
		return domain.Registration{}, apierr.Invalid(
			apierr.CodeInvalidRequest,
			"max_concurrent_attempts must be at least 1",
		)
	}

	if len(msg.GetTasks()) == 0 {
		return domain.Registration{}, apierr.Invalid(apierr.CodeInvalidRequest, "at least one task is required")
	}

	seen := make(map[string]struct{}, len(msg.GetTasks()))
	tasks := make([]domain.Task, 0, len(msg.GetTasks()))
	for _, def := range msg.GetTasks() {
		taskID := def.GetTaskId()
		if taskID == "" {
			return domain.Registration{}, invalidRegistration("task_id is required", taskID)
		}

		if err := requireShortID("task_id", taskID); err != nil {
			return domain.Registration{}, invalidRegistration(err.Error(), taskID)
		}

		if _, dup := seen[taskID]; dup {
			return domain.Registration{}, invalidRegistration("task_id is registered twice", taskID)
		}

		seen[taskID] = struct{}{}

		task, err := taskFromProto(msg.GetWorkerId(), def, now)
		if err != nil {
			return domain.Registration{}, invalidRegistration(err.Error(), taskID)
		}

		tasks = append(tasks, task)
	}

	return domain.Registration{
		WorkerID:   msg.GetWorkerId(),
		InstanceID: msg.GetInstanceId(),
		Slots:      int(msg.GetMaxConcurrentAttempts()),
		Tasks:      tasks,
		SDKName:    msg.GetSdkName(),
		SDKVersion: msg.GetSdkVersion(),
	}, nil
}

// invalidRegistration is the error for one Task definition that is wrong.
func invalidRegistration(message, taskID string) *apierr.Error {
	return apierr.Invalid(apierr.CodeInvalidRegistration, message).WithDetail("task_id", taskID)
}

// taskFromProto converts one Task definition and checks its schedule, retry policy,
// and start deadline. The error names the first thing that is wrong.
func taskFromProto(workerID string, def *runcoilv1.TaskDefinition, now time.Time) (domain.Task, error) {
	schedule, err := scheduleFromProto(def.GetSchedule())
	if err != nil {
		return domain.Task{}, err
	}

	if schedule != nil {
		if _, ok := domain.NextTick(*schedule, now); !ok {
			return domain.Task{}, errors.New("schedule has no tick that comes after now")
		}
	}

	retry, err := retryFromProto(def.GetRetry())
	if err != nil {
		return domain.Task{}, err
	}

	deadline, err := startDeadlineFromProto(def.GetStartDeadline())
	if err != nil {
		return domain.Task{}, err
	}

	return domain.Task{
		Ref:           domain.TaskRef{WorkerID: workerID, TaskID: def.GetTaskId()},
		Schedule:      schedule,
		Retry:         retry,
		StartDeadline: deadline,
	}, nil
}

// register stores the Tasks of a worker, and the schedules of its worker ID, and records
// the instance. The Tasks are stored first, then the schedules are
// replaced; a failure after the first step is returned, and the worker registers again,
// which is safe because both steps are idempotent. The whole path holds one lock, so
// concurrent registrations apply in one order. The scheduler is signalled to poll now,
// and the dispatcher is woken, because the instance may serve queued Runs now.
func (s *Service) register(ctx context.Context, reg domain.Registration) error {
	s.regMu.Lock()
	defer s.regMu.Unlock()

	now := s.clock()
	if err := s.tasks.UpsertTasks(ctx, reg.Tasks); err != nil {
		return fmt.Errorf("upsert tasks: %w", err)
	}

	stored, err := s.schedules.List(ctx, reg.WorkerID)
	if err != nil {
		return err
	}

	plan := domain.PlanSchedules(stored, reg.Tasks, now)
	if err := s.schedules.Replace(ctx, reg.WorkerID, plan); err != nil {
		return fmt.Errorf("replace schedules: %w", err)
	}

	s.instances.Register(reg, now)
	first := !s.known[reg.InstanceID]
	s.known[reg.InstanceID] = true

	logger := s.logger.With(slog.String("worker_id", reg.WorkerID))
	logger.InfoContext(
		ctx,
		"worker registered",
		slog.String("instance_id", reg.InstanceID),
		slog.Int("tasks", len(reg.Tasks)),
		slog.Int("slots", reg.Slots),
		slog.String("sdk_name", reg.SDKName),
		slog.String("sdk_version", reg.SDKVersion),
		slog.Bool("first_registration", first),
	)

	if added, changed, removed := scheduleChanges(stored, plan); added+changed+removed > 0 {
		logger.InfoContext(
			ctx,
			"schedules changed",
			slog.Int("added", added),
			slog.Int("changed", changed),
			slog.Int("removed", removed),
		)
	}

	s.signals.Notify(TopicSchedules)
	s.dispatcher.wakeUp()
	return nil
}

// scheduleChanges counts how a registration changed the schedules of a worker ID: the
// ones it adds, the ones whose rule it changes, and the stored ones it removes.
func scheduleChanges(stored, plan []domain.ScheduleEntry) (added, changed, removed int) {
	byTask := make(map[string]domain.ScheduleEntry, len(stored))
	for _, entry := range stored {
		byTask[entry.Ref.TaskID] = entry
	}

	for _, entry := range plan {
		prev, ok := byTask[entry.Ref.TaskID]
		switch {
		case !ok:
			added++
		case !domain.SameSchedule(prev.Schedule, entry.Schedule):
			changed++
		}

		delete(byTask, entry.Ref.TaskID)
	}

	return added, changed, len(byTask)
}

// attachStream gives the instance a new stream. It ends the stream the instance
// had before. It fails with NOT_REGISTERED when the instance is unknown, and with
// Unavailable when the service is stopping.
func (s *Service) attachStream(ctx context.Context, instanceID string) (*stream, error) {
	previous := s.instances.StreamOf(instanceID)
	sess, ok := s.instances.Attach(instanceID, s.clock())
	if !ok {
		return nil, apierr.FailedPrecondition(
			apierr.CodeNotRegistered,
			"instance is not registered, register it, then listen again",
		)
	}

	// The drain ends the streams only after stopped is closed, so a stream that was attached
	// before this check is ended by CloseAll. A stream attached after the check is refused
	// here, and it is removed at once, so that no stream is left open during the drain.
	if isStopping(s.stopped) {
		s.instances.Detach(instanceID, sess)
		return nil, errServerShuttingDown()
	}

	s.logger.InfoContext(
		ctx,
		"worker stream attached",
		slog.String("instance_id", instanceID),
		slog.Bool("replaced", previous != nil),
	)

	s.dispatcher.wakeUp()
	return sess, nil
}

// detachStream ends the stream of the instance. The server stops dispatching to
// it at once. The cause is decided before the detach: a stream that was replaced, or
// closed by the dispatcher or the server, is not a stream the worker closed.
func (s *Service) detachStream(
	ctx context.Context,
	instanceID string,
	sess *stream,
	attachedAt time.Time,
	clientGone bool,
) {
	cause := s.streamEndCause(instanceID, sess, clientGone)
	s.instances.Detach(instanceID, sess)

	s.logger.InfoContext(
		ctx,
		"worker stream detached",
		slog.String("instance_id", instanceID),
		slog.String("cause", cause),
		slog.Duration("duration", s.clock().Sub(attachedAt)),
	)

	s.dispatcher.wakeUp()
}

// streamEndCause says why a stream ended. The server stopping is checked first, then a
// replacement by a newer stream, then a close the dispatcher made, and then the worker's
// own disconnect. The only other way a stream ends is the silence sweep of the instances.
func (s *Service) streamEndCause(instanceID string, sess *stream, clientGone bool) string {
	select {
	case <-s.stopped:
		return "server stopping"
	default:
	}

	if current := s.instances.StreamOf(instanceID); current != nil && current != sess {
		return "replaced by a new stream"
	}

	if cause := s.dispatcher.ends.take(sess); cause != "" {
		return cause
	}

	if clientGone {
		return "closed by the worker"
	}

	return "ended for silence"
}

// serveStream writes the messages the instance's stream hands over, and a keepalive
// every heartbeat interval. It returns when the stream ends or the connection breaks.
// It converts the domain messages to the wire format, and reports each write back.
func serveStream(
	ctx context.Context,
	sess *stream,
	out runcoilv1connect.WorkerServiceListenServerStream,
	heartbeatInterval time.Duration,
) error {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sess.Done():
			return nil
		case item := <-sess.Outbox():
			err := out.Send(listenMessage(item.Message))
			item.Result <- err
			if err != nil {
				return fmt.Errorf("send on listen stream: %w", err)
			}
		case <-ticker.C:
			if err := out.Send(keepaliveMessage()); err != nil {
				return fmt.Errorf("send keepalive: %w", err)
			}
		}
	}
}

// heartbeat records that the instance is alive, and renews the leases of its named
// attempts in one transaction. It returns the attempts whose lease is no longer
// current, and the new expiry time. The Step each attempt is running, and its policy,
// are recorded in the same statement.
func (s *Service) heartbeat(
	ctx context.Context,
	instanceID string,
	attempts []domain.AttemptRef,
) ([]domain.AttemptRef, time.Time, error) {
	now := s.clock()
	s.instances.Touch(instanceID, now)
	expiresAt := now.Add(s.cfg.LeaseDuration)
	if len(attempts) == 0 {
		return nil, expiresAt, nil
	}

	stale, err := s.runs.RenewLeases(ctx, instanceID, attempts, expiresAt)
	if err != nil {
		return nil, time.Time{}, err
	}

	return stale, expiresAt, nil
}

// listenMessage converts a domain stream message to its wire message.
func listenMessage(msg streamMessage) *runcoilv1.ListenResponse {
	if msg.Cancel != nil {
		return cancelMessage(msg.Cancel.RunID, msg.Cancel.Token)
	}

	return dispatchMessage(*msg.Run, msg.Steps)
}

// ackDispatch records that the worker owns the attempt. It is idempotent while the
// attempt runs under the same token, and refused once the lease has expired. It reads
// only the Run: the Steps and their outputs are not needed for it.
func (s *Service) ackDispatch(ctx context.Context, id string, token string) error {
	var acked domain.Run
	err := s.runs.MutateRun(ctx, id, func(snap domain.Snapshot) (domain.Write, error) {
		write, err := domain.AckRun(snap, token, s.clock())
		acked = write.Run
		return write, err
	})
	if err != nil {
		return s.runError(id, err)
	}

	s.logger.DebugContext(
		ctx,
		"run acked",
		slog.String("run_id", id),
		slog.String("worker_id", acked.Task.WorkerID),
		slog.String("task_id", acked.Task.TaskID),
		slog.String("instance_id", acked.InstanceID),
		slog.Int("attempt", acked.Attempts),
	)

	return nil
}

// recordStep applies one try of one Step. It wakes the dispatcher when the try
// ends the attempt, because the slot is free then.
func (s *Service) recordStep(ctx context.Context, id string, token string, rep domain.StepReport) (domain.StepAnswer, error) {
	if rep.StepID == "" {
		return domain.StepAnswer{}, apierr.Invalid(apierr.CodeInvalidRequest, "step_id is required")
	}

	if err := requireShortID("step_id", rep.StepID); err != nil {
		return domain.StepAnswer{}, err
	}

	if rep.Attempt < 1 {
		return domain.StepAnswer{}, apierr.Invalid(apierr.CodeInvalidRequest, "step_attempt must be at least 1")
	}

	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		return domain.StepAnswer{}, s.runError(id, err)
	}

	taskPolicy, err := s.taskPolicy(ctx, run.Task)
	if err != nil {
		return domain.StepAnswer{}, err
	}

	now := s.clock()
	var answer domain.StepAnswer
	err = s.runs.Mutate(ctx, id, func(snap domain.Snapshot) (domain.Write, error) {
		write, ans, err := domain.RecordStep(snap, token, rep, taskPolicy, s.retry, s.limits, now, s.unit())
		answer = ans
		return write, err
	})
	if err != nil {
		return domain.StepAnswer{}, s.runError(id, err)
	}

	if answer.Record != nil {
		s.logger.DebugContext(
			ctx,
			"step recorded",
			slog.String("run_id", id),
			slog.String("worker_id", run.Task.WorkerID),
			slog.String("task_id", run.Task.TaskID),
			slog.String("step_id", rep.StepID),
			slog.String("status", string(answer.Record.Status)),
			slog.Int("step_attempt", rep.Attempt),
		)
	}

	if answer.Retry != nil {
		s.announceStepRetry(ctx, id, run.Task, rep, *answer.Retry)
	}

	return answer, nil
}

// announceStepRetry logs a Step retry, makes its Run ready at the retry time, and wakes the dispatcher.
func (s *Service) announceStepRetry(
	ctx context.Context,
	id string,
	task domain.TaskRef,
	rep domain.StepReport,
	retry domain.RetryAnswer,
) {
	s.logger.DebugContext(
		ctx,
		"retry scheduled",
		slog.String("run_id", id),
		slog.String("worker_id", task.WorkerID),
		slog.String("task_id", task.TaskID),
		slog.String("reason", "step failed"),
		slog.String("step_id", rep.StepID),
		slog.Int("step_attempt", rep.Attempt),
		slog.Time("next_attempt_at", retry.NotBefore),
	)

	s.announceReady(ctx, task, retry.NotBefore)
	s.dispatcher.wakeUp()
}

// finishRun applies a worker's end of an attempt. It wakes the dispatcher, because
// the attempt has left its slot, and wakes WaitRun calls when the Run is finished.
func (s *Service) finishRun(ctx context.Context, id string, in domain.FinishInput) (domain.FinishAnswer, error) {
	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		return domain.FinishAnswer{}, s.runError(id, err)
	}

	taskPolicy, err := s.taskPolicy(ctx, run.Task)
	if err != nil {
		return domain.FinishAnswer{}, err
	}

	if in.Kind == domain.FinishReleased {
		in.ReleaseReason = queuedReasonOf(s.instances, run.Task)
	}

	now := s.clock()
	var answer domain.FinishAnswer
	err = s.runs.Mutate(ctx, id, func(snap domain.Snapshot) (domain.Write, error) {
		write, ans, err := domain.FinishRun(snap, in, taskPolicy, s.retry, s.limits, now, s.unit())
		answer = ans
		return write, err
	})
	if err != nil {
		return domain.FinishAnswer{}, s.runError(id, err)
	}

	s.logFinish(ctx, run, in.Kind, answer, now)

	if answer.Retry != nil {
		s.announceReady(ctx, run.Task, answer.Retry.NotBefore)
	}

	if answer.Run.Status.Terminal() {
		s.signals.Notify(topicRunFinished(id))
	}

	s.dispatcher.wakeUp()
	return answer, nil
}

// logFinish writes the line of a worker's end of an attempt: the Run finished, the Run
// was released, or a retry was scheduled. The Run is decided; the line is written after.
func (s *Service) logFinish(ctx context.Context, run domain.Run, kind domain.FinishKind, answer domain.FinishAnswer, now time.Time) {
	logger := s.logger.With(
		slog.String("run_id", run.ID),
		slog.String("worker_id", run.Task.WorkerID),
		slog.String("task_id", run.Task.TaskID),
	)

	switch {
	case kind == domain.FinishReleased && answer.Retry != nil:
		logger.DebugContext(ctx, "run released", slog.Int("attempt", run.Attempts))
	case answer.Retry != nil:
		logger.DebugContext(
			ctx,
			"retry scheduled",
			slog.String("reason", "handler failed"),
			slog.Time("next_attempt_at", answer.Retry.NotBefore),
		)
	case answer.Run.Status.Terminal():
		logger.DebugContext(
			ctx,
			"run finished",
			slog.String("status", string(answer.Run.Status)),
			slog.Int("attempts", answer.Run.Attempts),
			slog.Duration("duration", now.Sub(run.CreatedAt)),
		)
	}
}
