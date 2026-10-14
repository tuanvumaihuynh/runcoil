package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

var _ runcoilv1connect.RunServiceHandler = runHandler{}

// runHandler serves the RunService, the calls that a caller or an operator makes.
// It maps messages to engine calls and back, and holds no logic of its own.
type runHandler struct {
	s *Service
}

// taskListing is a registered Task with its next schedule tick, and the number of
// connected workers that can run it now.
type taskListing struct {
	task         domain.Task
	readyWorkers int
}

// Invoke creates a queued Run of a registered Task, or returns the Run of a repeated key.
func (h runHandler) Invoke(
	ctx context.Context,
	req *runcoilv1.InvokeRequest,
) (*runcoilv1.InvokeResponse, error) {
	id, deduplicated, err := h.s.invoke(
		ctx,
		domain.TaskRef{WorkerID: req.GetWorkerId(), TaskID: req.GetTaskId()},
		req.GetIdempotencyKey(),
		req.GetInput(),
	)
	if err != nil {
		return nil, err
	}

	return &runcoilv1.InvokeResponse{RunId: id, Deduplicated: deduplicated}, nil
}

// GetRun returns a snapshot of a Run by its ID, or by the idempotency key that created it.
func (h runHandler) GetRun(
	ctx context.Context,
	req *runcoilv1.GetRunRequest,
) (*runcoilv1.GetRunResponse, error) {
	var (
		run   domain.Run
		steps []domain.StepRecord
		err   error
	)

	switch by := req.GetBy().(type) {
	case *runcoilv1.GetRunRequest_RunId:
		if err := requireRunID(by.RunId); err != nil {
			return nil, err
		}

		run, steps, err = h.s.getRun(ctx, by.RunId, req.GetIncludeSteps())
	case *runcoilv1.GetRunRequest_IdempotencyKey:
		run, steps, err = h.s.getRunByKey(ctx, by.IdempotencyKey, req.GetIncludeSteps())
	default:
		return nil, apierr.Invalid(apierr.CodeInvalidRequest, "run_id or idempotency_key is required")
	}

	if err != nil {
		return nil, err
	}

	out := runToProto(run)
	if req.GetIncludeSteps() {
		out.Steps = stepsToProto(steps)
	}

	return &runcoilv1.GetRunResponse{Run: out}, nil
}

// WaitRun returns the Run once it is finished, or when the timeout passes.
func (h runHandler) WaitRun(
	ctx context.Context,
	req *runcoilv1.WaitRunRequest,
) (*runcoilv1.WaitRunResponse, error) {
	if err := requireRunID(req.GetRunId()); err != nil {
		return nil, err
	}

	var timeout time.Duration
	if d := req.GetTimeout(); d != nil {
		timeout = d.AsDuration()
		if timeout < 0 {
			return nil, apierr.Invalid(apierr.CodeInvalidRequest, "timeout must not be negative")
		}
	}

	run, steps, timedOut, err := h.s.waitRun(ctx, req.GetRunId(), timeout, req.GetIncludeSteps())
	if err != nil {
		return nil, err
	}

	out := runToProto(run)
	if req.GetIncludeSteps() {
		out.Steps = stepsToProto(steps)
	}

	return &runcoilv1.WaitRunResponse{Run: out, TimedOut: timedOut}, nil
}

// ListRuns returns one page of Runs, newest first, without Steps and without payloads.
func (h runHandler) ListRuns(
	ctx context.Context,
	req *runcoilv1.ListRunsRequest,
) (*runcoilv1.ListRunsResponse, error) {
	filter, err := runFilterFromProto(req)
	if err != nil {
		return nil, err
	}

	runs, next, err := h.s.listRuns(ctx, filter, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}

	out := make([]*runcoilv1.Run, 0, len(runs))
	for _, run := range runs {
		summary := runToProto(run)
		summary.Input = nil
		summary.Output = nil
		out = append(out, summary)
	}

	return &runcoilv1.ListRunsResponse{Runs: out, NextPageToken: next}, nil
}

// Rerun creates a new Run from a finished one, from its start or from a Step that has a record.
func (h runHandler) Rerun(
	ctx context.Context,
	req *runcoilv1.RerunRequest,
) (*runcoilv1.RerunResponse, error) {
	if err := requireRunID(req.GetRunId()); err != nil {
		return nil, err
	}

	newID, deduplicated, err := h.s.rerun(ctx, req.GetRunId(), req.GetFromStepId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}

	return &runcoilv1.RerunResponse{RunId: newID, Deduplicated: deduplicated}, nil
}

// ListTasks returns the registered Tasks of a worker ID, with their schedules and next ticks.
func (h runHandler) ListTasks(
	ctx context.Context,
	req *runcoilv1.ListTasksRequest,
) (*runcoilv1.ListTasksResponse, error) {
	listing, err := h.s.listTasks(ctx, req.GetWorkerId())
	if err != nil {
		return nil, err
	}

	out := make([]*runcoilv1.TaskInfo, 0, len(listing))
	for _, l := range listing {
		out = append(out, taskInfoToProto(l))
	}

	return &runcoilv1.ListTasksResponse{Tasks: out}, nil
}

// invoke creates a queued Run of a registered Task. The Run and its key are stored in
// one transaction. A repeat of the same key and input returns the first Run, marked as
// deduplicated; the same key with a different input is a conflict that names that Run.
func (s *Service) invoke(ctx context.Context, ref domain.TaskRef, key string, input []byte) (string, bool, error) {
	if ref.WorkerID == "" || ref.TaskID == "" {
		return "", false, apierr.Invalid(apierr.CodeInvalidRequest, "worker_id and task_id are required")
	}

	if err := requireShortID("worker_id", ref.WorkerID); err != nil {
		return "", false, err
	}

	if err := requireShortID("task_id", ref.TaskID); err != nil {
		return "", false, err
	}

	if err := validateKey(key); err != nil {
		return "", false, err
	}

	if domain.InputTooLarge(input, s.limits) {
		return "", false, apierr.Invalid(apierr.CodeInputTooLarge, "run input is over its size limit")
	}

	if _, err := s.tasks.GetTask(ctx, ref); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", false, apierr.NotFound(apierr.CodeTaskNotFound, "task is not registered").
				WithDetail("task_id", ref.TaskID)
		}

		return "", false, err
	}

	now := s.clock()
	reason := queuedReasonOf(s.instances, ref)
	run := domain.Run{
		Task:         ref,
		Status:       domain.RunStatusQueued,
		QueuedReason: reason,
		Source:       domain.RunSourceInvoke,
		Input:        input,
		DueAt:        now,
		CreatedAt:    now,
	}

	fingerprint := domain.Fingerprint("invoke", ref.WorkerID, ref.TaskID, string(input))
	return s.createRun(ctx, run, key, fingerprint, nil, now)
}

// validateKey checks an idempotency key. The key is required, because a repeat is
// recognized by it, and it is at most 255 characters.
func validateKey(key string) error {
	if key == "" {
		return apierr.Invalid(apierr.CodeInvalidRequest, "idempotency_key is required")
	}

	if utf8.RuneCountInString(key) > domain.MaxIdempotencyKeyLength {
		return apierr.Invalid(apierr.CodeInvalidRequest, "idempotency key is longer than 255 characters")
	}

	return nil
}

// createRun stores a new Run under its idempotency key, or returns the Run that already
// holds the key. A new Run is announced to the queue after it is committed. It returns
// the Run's ID and whether the answer is a repeat of an earlier request.
func (s *Service) createRun(
	ctx context.Context,
	run domain.Run,
	key, fingerprint string,
	copied []domain.StepRecord,
	now time.Time,
) (string, bool, error) {
	v7, err := uuid.NewV7()
	if err != nil {
		return "", false, fmt.Errorf("new run id: %w", err)
	}

	runID := v7.String()
	record := domain.KeyRecord{
		Key:         key,
		Fingerprint: fingerprint,
		RunID:       runID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.cfg.Idempotency.Window),
	}

	run.ID = runID
	holder, created, err := s.runs.CreateRun(ctx, run, record, copied)
	if err != nil {
		return "", false, err
	}

	if !created {
		return s.answerExistingKey(ctx, run, fingerprint, holder)
	}

	s.logger.DebugContext(
		ctx,
		"run created",
		slog.String("run_id", runID),
		slog.String("worker_id", run.Task.WorkerID),
		slog.String("task_id", run.Task.TaskID),
		slog.String("source", string(run.Source)),
		slog.Bool("deduplicated", false),
	)

	s.announceReady(ctx, run.Task, now)
	s.dispatcher.wakeUp()
	return runID, false, nil
}

// answerExistingKey answers a create whose key exists: the first Run again, or a conflict.
func (s *Service) answerExistingKey(
	ctx context.Context,
	run domain.Run,
	fingerprint string,
	holder domain.KeyHolder,
) (string, bool, error) {
	switch domain.DecideKey(&holder, fingerprint) {
	case domain.KeyDeduplicated:
		s.logger.DebugContext(
			ctx,
			"run deduplicated",
			slog.String("run_id", holder.RunID),
			slog.String("worker_id", run.Task.WorkerID),
			slog.String("task_id", run.Task.TaskID),
			slog.String("source", string(run.Source)),
		)

		return holder.RunID, true, nil
	case domain.KeyConflict:
		return "", false, apierr.Conflict(
			apierr.CodeIdempotencyConflict,
			"the idempotency key was used for a different request",
		).WithDetail("run_id", holder.RunID)
	default:
		return "", false, errors.New("idempotency key held no decision")
	}
}

// getRun returns a Run, and its finished Steps in order when includeSteps is set.
func (s *Service) getRun(ctx context.Context, id string, includeSteps bool) (domain.Run, []domain.StepRecord, error) {
	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		return domain.Run{}, nil, s.runError(id, err)
	}

	if !includeSteps {
		return run, nil, nil
	}

	steps, err := s.runs.ListSteps(ctx, id)
	if err != nil {
		return domain.Run{}, nil, err
	}

	return run, finishedSteps(steps), nil
}

// finishedSteps returns the Steps that have a final record, in order.
func finishedSteps(steps []domain.StepRecord) []domain.StepRecord {
	out := make([]domain.StepRecord, 0, len(steps))
	for _, step := range steps {
		if step.Finished() {
			out = append(out, step)
		}
	}

	return out
}

// getRunByKey returns the Run that an unexpired key created, and its finished Steps when asked.
func (s *Service) getRunByKey(ctx context.Context, key string, includeSteps bool) (domain.Run, []domain.StepRecord, error) {
	if err := validateKey(key); err != nil {
		return domain.Run{}, nil, err
	}

	run, err := s.runs.RunByKey(ctx, key, s.clock())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Run{}, nil, apierr.NotFound(apierr.CodeRunNotFound, "no run has this idempotency key")
	}

	if err != nil {
		return domain.Run{}, nil, err
	}

	if !includeSteps {
		return run, nil, nil
	}

	steps, err := s.runs.ListSteps(ctx, run.ID)
	if err != nil {
		return domain.Run{}, nil, err
	}

	return run, finishedSteps(steps), nil
}

// waitRun returns the Run once it is finished, or when the wait ends. The wait is the
// request's timeout, capped at the configured maximum; zero means the maximum. A
// waiting call polls only the Run's status and wakes on the finished signal, so that
// it reads the whole Run once, at the end. It returns timedOut when the Run was not
// finished at the end. A wait does not change the Run. A call that is cancelled or
// passes its deadline stops waiting, and so does a call still waiting at shutdown. A
// call over the configured number of waiters is refused.
func (s *Service) waitRun(
	ctx context.Context,
	id string,
	timeout time.Duration,
	includeSteps bool,
) (domain.Run, []domain.StepRecord, bool, error) {
	select {
	case s.waitSlots <- struct{}{}:
		defer func() { <-s.waitSlots }()
	default:
		return domain.Run{}, nil, false, apierr.Unavailable("", "too many WaitRun calls are waiting; try again later")
	}

	wait := s.cfg.WaitRun.MaxTimeout
	if timeout > 0 && timeout < wait {
		wait = timeout
	}

	finished, unsubscribe := s.signals.Subscribe(topicRunFinished(id))
	defer unsubscribe()
	deadline, stopDeadline := s.after(wait)
	defer stopDeadline()

	for {
		status, err := s.runs.RunStatus(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return domain.Run{}, nil, false, waitCtxError(ctx.Err())
			}

			return domain.Run{}, nil, false, s.runError(id, err)
		}

		if status.Terminal() {
			return s.finalRun(ctx, id, includeSteps)
		}

		poll, stopPoll := s.after(s.cfg.WaitRun.PollInterval)
		select {
		case <-ctx.Done():
			stopPoll()
			return domain.Run{}, nil, false, waitCtxError(ctx.Err())
		case <-s.stopped:
			stopPoll()
			return domain.Run{}, nil, false, apierr.Unavailable("", "the server is shutting down")
		case <-deadline:
			stopPoll()
			return s.finalRun(ctx, id, includeSteps)
		case <-finished:
		case <-poll:
		}

		stopPoll()
	}
}

// waitCtxError is the answer for a WaitRun call whose context ended: a passed deadline
// is deadline-exceeded, and anything else is a cancellation by the client.
func waitCtxError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return apierr.DeadlineExceeded("the call's deadline passed while waiting")
	}

	return apierr.Canceled("the call was cancelled while waiting")
}

// finalRun reads the whole Run once, with its Steps when asked. timedOut is set when
// the Run is not finished at that time.
func (s *Service) finalRun(ctx context.Context, id string, includeSteps bool) (domain.Run, []domain.StepRecord, bool, error) {
	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		if ctx.Err() != nil {
			return domain.Run{}, nil, false, waitCtxError(ctx.Err())
		}

		return domain.Run{}, nil, false, s.runError(id, err)
	}

	timedOut := !run.Status.Terminal()
	if !includeSteps {
		return run, nil, timedOut, nil
	}

	steps, err := s.runs.ListSteps(ctx, run.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "list steps for wait", slog.Any("error", err))
		return domain.Run{}, nil, false, err
	}

	return run, finishedSteps(steps), timedOut, nil
}

// listRuns returns one page of Runs, newest first, and the token of the next page. A
// page holds up to the page size; the page after it starts at the token.
func (s *Service) listRuns(ctx context.Context, filter domain.RunFilter, requestedSize uint32, pageToken string) ([]domain.Run, string, error) {
	size, err := pageSize(requestedSize)
	if err != nil {
		return nil, "", apierr.Invalid(apierr.CodeInvalidRequest, err.Error())
	}

	if pageToken != "" {
		cursor, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", apierr.Invalid(apierr.CodeInvalidRequest, "page_token is not valid")
		}

		filter.AfterCreatedAt = cursor.CreatedAt
		filter.AfterID = cursor.ID
	}

	// One more Run than the page holds tells whether another page follows.
	filter.Limit = size + 1
	runs, err := s.runs.ListRuns(ctx, filter)
	if err != nil {
		return nil, "", err
	}

	if len(runs) <= size {
		return runs, "", nil
	}

	runs = runs[:size]
	last := runs[size-1]
	return runs, encodeCursor(runCursor{CreatedAt: last.CreatedAt, ID: last.ID}), nil
}

const (
	// defaultPageSize is the number of Runs a ListRuns page holds when the request names none.
	defaultPageSize = 50
	// maxPageSize is the largest number of Runs one ListRuns page may hold.
	maxPageSize = 200
)

// errPageSize reports a page size over the maximum.
var errPageSize = fmt.Errorf("page size must be at most %d", maxPageSize)

// errPageToken reports a page token that this server did not issue.
var errPageToken = errors.New("page token is not valid")

// runCursor is the position of the last Run of a page: its creation time and ID.
type runCursor struct {
	CreatedAt time.Time
	ID        string
}

// cursorJSON is the content of a page token. The token is opaque to callers.
type cursorJSON struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

// pageSize returns the page size for a request: the default when none is given, and an
// error when it is over the maximum.
func pageSize(requested uint32) (int, error) {
	if requested == 0 {
		return defaultPageSize, nil
	}

	if requested > maxPageSize {
		return 0, errPageSize
	}

	return int(requested), nil
}

// encodeCursor returns the opaque page token for a position.
func encodeCursor(c runCursor) string {
	raw, _ := json.Marshal(cursorJSON{CreatedAt: c.CreatedAt.UTC(), ID: c.ID}) // Cannot fail: plain fields.
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reads a page token. A token that does not decode gives errPageToken. So
// does one whose ID has a NUL byte. The request check sees only the encoded token, so
// the ID inside it is checked here.
func decodeCursor(token string) (runCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return runCursor{}, errPageToken
	}

	var cj cursorJSON
	if err := json.Unmarshal(raw, &cj); err != nil || cj.ID == "" || cj.CreatedAt.IsZero() {
		return runCursor{}, errPageToken
	}

	if strings.ContainsRune(cj.ID, 0) {
		return runCursor{}, errPageToken
	}

	return runCursor(cj), nil
}

// rerun creates a new Run from a finished Run: from its start, or from a Step that has
// a record. The new Run keeps the input and the Steps recorded before the selected one.
// It is keyed like an invoke, and the same key with the same request is a repeat.
func (s *Service) rerun(
	ctx context.Context,
	id string,
	fromStepID, key string,
) (string, bool, error) {
	if err := validateKey(key); err != nil {
		return "", false, err
	}

	original, err := s.runs.GetRun(ctx, id)
	if err != nil {
		return "", false, s.runError(id, err)
	}

	var steps []domain.StepRecord
	if fromStepID != "" {
		steps, err = s.runs.ListSteps(ctx, id)
		if err != nil {
			return "", false, err
		}
	}

	plan, err := domain.PlanRerun(original, steps, fromStepID)
	switch {
	case errors.Is(err, domain.ErrRunNotFinished):
		return "", false, apierr.FailedPrecondition(apierr.CodeRunNotFinished, "the run is not finished").
			WithDetail("run_id", id)
	case errors.Is(err, domain.ErrStepNotFound):
		return "", false, apierr.NotFound(apierr.CodeStepNotFound, "the step has no record in this run").
			WithDetail("run_id", id)
	case err != nil:
		return "", false, err
	}

	now := s.clock()
	reason := queuedReasonOf(s.instances, original.Task)
	run := domain.Run{
		Task:         original.Task,
		Status:       domain.RunStatusQueued,
		QueuedReason: reason,
		Source:       domain.RunSourceRerun,
		Input:        original.Input,
		RerunOf:      original.ID,
		DueAt:        now,
		CreatedAt:    now,
	}

	fingerprint := domain.Fingerprint("rerun", id, fromStepID)
	newID, deduplicated, err := s.createRun(ctx, run, key, fingerprint, plan.Copied, now)
	if err != nil {
		// A source Run removed by retention between the read and the insert is not found.
		return "", false, s.runError(id, err)
	}

	return newID, deduplicated, nil
}

// listTasks returns the Tasks of a worker ID, with their schedules, their next ticks,
// and the ready workers. The schedules come from the schedule store, by Task.
func (s *Service) listTasks(ctx context.Context, workerID string) ([]taskListing, error) {
	if workerID == "" {
		return nil, apierr.Invalid(apierr.CodeInvalidRequest, "worker_id is required")
	}

	tasks, err := s.tasks.ListTasks(ctx, workerID)
	if err != nil {
		return nil, err
	}

	schedules, err := s.schedules.List(ctx, workerID)
	if err != nil {
		return nil, err
	}

	byTask := make(map[string]domain.ScheduleEntry, len(schedules))
	for _, entry := range schedules {
		byTask[entry.Ref.TaskID] = entry
	}

	out := make([]taskListing, 0, len(tasks))
	for _, task := range tasks {
		if entry, ok := byTask[task.Ref.TaskID]; ok {
			schedule := entry.Schedule
			task.Schedule = &schedule
			task.NextRunAt = entry.NextRunAt
		}

		candidates := s.instances.Serving(task.Ref)
		out = append(out, taskListing{task: task, readyWorkers: len(candidates)})
	}

	return out, nil
}
