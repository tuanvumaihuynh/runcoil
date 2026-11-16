package postgres

import (
	"encoding/json"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

// retryPolicyJSON is how tasks.retry_policy and the retry of runs.config store a policy.
// Delays are Go duration strings, such as "1m30s".
type retryPolicyJSON struct {
	MaxAttempts int      `json:"max_attempts,omitempty"`
	Delays      []string `json:"delays,omitempty"`
}

// runConfigJSON is the part of runs.config that the engine reads: the Task's settings when
// the Run was created.
type runConfigJSON struct {
	Retry *retryPolicyJSON `json:"retry"`
}

func retryPolicyToJSON(p domain.RetryPolicy) retryPolicyJSON {
	policy := retryPolicyJSON{MaxAttempts: p.MaxAttempts}
	for _, d := range p.Delays {
		policy.Delays = append(policy.Delays, d.String())
	}

	return policy
}

func retryPolicyFromJSON(p retryPolicyJSON) (domain.RetryPolicy, error) {
	policy := domain.RetryPolicy{MaxAttempts: p.MaxAttempts}
	for _, s := range p.Delays {
		d, err := time.ParseDuration(s)
		if err != nil {
			return domain.RetryPolicy{}, fmt.Errorf("parse delay %q: %w", s, err)
		}

		policy.Delays = append(policy.Delays, d)
	}

	return policy, nil
}

// runRetryPolicy returns the retry policy in a Run's config, or nil when it has none.
func runRetryPolicy(config json.RawMessage) (*domain.RetryPolicy, error) {
	if len(config) == 0 {
		return nil, nil //nolint:nilnil // No policy is not an error.
	}

	var c runConfigJSON
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("decode run config: %w", err)
	}

	if c.Retry == nil {
		return nil, nil //nolint:nilnil // No policy is not an error.
	}

	policy, err := retryPolicyFromJSON(*c.Retry)
	if err != nil {
		return nil, fmt.Errorf("decode retry policy: %w", err)
	}

	return &policy, nil
}

func taskUpsertParams(id, projectID uuid.UUID, app string, def domain.TaskDefinition) (gen.TaskUpsertParams, error) {
	params := gen.TaskUpsertParams{
		ID:            id,
		ProjectID:     projectID,
		App:           app,
		Name:          def.Name,
		StartDeadline: interval(def.StartDeadline),
	}

	if def.Retry != nil {
		raw, err := json.Marshal(retryPolicyToJSON(*def.Retry))
		if err != nil {
			return gen.TaskUpsertParams{}, fmt.Errorf("encode retry policy: %w", err)
		}

		msg := json.RawMessage(raw)
		params.RetryPolicy = &msg
	}

	return params, nil
}

// stepRecordParams is a row of steps, whichever query read it.
type stepRecordParams struct {
	StepID       string
	Kind         string
	Status       string
	Output       []byte
	ErrorCode    *string
	ErrorMessage *string
}

// stepRecord builds a Step record from its row.
func stepRecord(params stepRecordParams) domain.StepRecord {
	record := domain.StepRecord{StepID: params.StepID, Kind: domain.StepKind(params.Kind), Output: params.Output}
	if domain.StepStatus(params.Status) != domain.StepStatusFailed {
		return record
	}

	record.Output = nil
	record.Failure = &domain.Failure{}
	if params.ErrorCode != nil {
		record.Failure.Code = domain.FailureCode(*params.ErrorCode)
	}

	if params.ErrorMessage != nil {
		record.Failure.Message = *params.ErrorMessage
	}

	return record
}

// interval converts an optional duration to an interval; nil is NULL.
func interval(d *time.Duration) pgtype.Interval {
	if d == nil {
		return pgtype.Interval{}
	}

	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func projectRoleFromRow(row gen.ProjectMemberListByUserRow) domain.ProjectRole {
	return domain.ProjectRole{
		ProjectID:   row.ID,
		ProjectName: row.Name,
		Role:        domain.Role(row.Role),
	}
}

func memberFromRow(row gen.ProjectMemberListRow) domain.Member {
	return domain.Member{
		UserID:   row.ID,
		Username: row.Username,
		Email:    row.Email,
		Role:     domain.Role(row.Role),
	}
}

func apiKeyFromRow(row gen.ApiKeyListRow) domain.APIKey {
	return domain.APIKey(row)
}

func workerFromRow(row gen.WorkerGetBySessionTokenRow) domain.Worker {
	return domain.Worker{
		ID:        row.ID,
		ProjectID: row.ProjectID,
		APIKeyID:  row.ApiKeyID,
		App:       row.App,
		TaskIDs:   row.TaskIds,
		Slots:     int(row.Slots),
	}
}

func assignFromRow(row gen.RunListForAssignRow, epoch int64, steps []domain.StepRecord) domain.Assign {
	return domain.Assign{
		Lease:     domain.Lease{RunID: row.ID, Epoch: epoch},
		TaskName:  row.TaskName,
		Input:     row.Input,
		Steps:     steps,
		CreatedAt: row.CreatedAt,
	}
}

// workerRecordFromRow gives the worker its Tasks, by name, from names; it leaves out a
// Task that names does not hold.
func workerRecordFromRow(row gen.WorkerListEndedRow, names map[uuid.UUID]string) domain.WorkerRecord {
	tasks := make([]domain.TaskRef, 0, len(row.TaskIds))
	for _, id := range row.TaskIds {
		if name, ok := names[id]; ok {
			tasks = append(tasks, domain.TaskRef{ID: id, Name: name})
		}
	}

	return domain.WorkerRecord{
		ID:          row.ID,
		App:         row.App,
		Slots:       int(row.Slots),
		Tasks:       tasks,
		ConnectedAt: row.ConnectedAt,
		HeartbeatAt: row.HeartbeatAt,
		EndedAt:     row.EndedAt,
		EndReason:   (*domain.WorkerEndReason)(row.EndReason),
	}
}

func runFailure(errorCode, errorMessage *string) *domain.Failure {
	if errorCode == nil {
		return nil
	}

	failure := &domain.Failure{Code: domain.FailureCode(*errorCode)}
	if errorMessage != nil {
		failure.Message = *errorMessage
	}

	return failure
}

func runSummaryFromRow(row gen.RunListRow) domain.RunSummary {
	return domain.RunSummary{
		ID:        row.ID,
		Status:    domain.RunStatus(row.Status),
		TaskID:    row.TaskID,
		TaskApp:   row.TaskApp,
		TaskName:  row.TaskName,
		Failure:   runFailure(row.ErrorCode, nil),
		CreatedAt: row.CreatedAt,
		StartedAt: row.StartedAt,
		EndedAt:   row.EndedAt,
	}
}

func runDetailFromRow(row gen.RunGetWithAttemptRow, steps []gen.StepListByRunRow) (domain.RunDetail, error) {
	retry, err := runRetryPolicy(row.Config)
	if err != nil {
		return domain.RunDetail{}, err
	}

	run := domain.RunDetail{
		RunSummary: domain.RunSummary{
			ID:        row.ID,
			Status:    domain.RunStatus(row.Status),
			TaskID:    row.TaskID,
			TaskApp:   row.TaskApp,
			TaskName:  row.TaskName,
			Failure:   runFailure(row.ErrorCode, row.ErrorMessage),
			CreatedAt: row.CreatedAt,
			StartedAt: row.StartedAt,
			EndedAt:   row.EndedAt,
		},
		Input:  row.Input,
		Output: row.Output,
		Retry:  retry,
		Steps:  make([]domain.StepDetail, 0, len(steps)),
	}

	switch {
	case row.QueueReadyAt != nil:
		run.Waiting = &domain.RunWaiting{
			Reason:          domain.RunWaitWorker,
			ReadyAt:         *row.QueueReadyAt,
			RetryStepID:     row.QueueRetryStepID,
			RetryCount:      int(*row.QueueRetryCount),
			StartDeadlineAt: row.QueueStartDeadlineAt,
		}
	case row.DelayedReadyAt != nil:
		run.Waiting = &domain.RunWaiting{
			Reason:          domain.RunWaitRetry,
			ReadyAt:         *row.DelayedReadyAt,
			RetryStepID:     row.DelayedRetryStepID,
			RetryCount:      int(*row.DelayedRetryCount),
			StartDeadlineAt: row.DelayedStartDeadlineAt,
		}
	case row.LeaseWorkerID != nil:
		run.Attempt = &domain.RunAttempt{WorkerID: *row.LeaseWorkerID, StartedAt: *row.LeaseStartedAt}
	}

	for _, step := range steps {
		run.Steps = append(run.Steps, domain.StepDetail{
			StepRecord: stepRecord(stepRecordParams{
				StepID:       step.StepID,
				Kind:         step.Kind,
				Status:       step.Status,
				Output:       step.Output,
				ErrorCode:    step.ErrorCode,
				ErrorMessage: step.ErrorMessage,
			}),
			TryCount:   int(step.TryCount),
			RecordedAt: step.CreatedAt,
		})
	}

	return run, nil
}

// duration converts an interval written by interval back to a duration; NULL is nil.
func duration(i pgtype.Interval) *time.Duration {
	if !i.Valid {
		return nil
	}

	d := time.Duration(i.Microseconds)*time.Microsecond + time.Duration(i.Days)*24*time.Hour

	return &d
}

type taskScheduleColumns struct {
	cron       *string
	overlap    *string
	nextTickAt *time.Time
	pausedAt   *time.Time
}

func taskSchedule(c taskScheduleColumns) *domain.TaskSchedule {
	if c.cron == nil || c.overlap == nil || c.nextTickAt == nil {
		return nil
	}

	return &domain.TaskSchedule{
		Cron:       *c.cron,
		Overlap:    domain.OverlapPolicy(*c.overlap),
		NextTickAt: *c.nextTickAt,
		PausedAt:   c.pausedAt,
	}
}

func lastRun(id *uuid.UUID, status *string, createdAt *time.Time) *domain.LastRun {
	if id == nil || status == nil || createdAt == nil {
		return nil
	}

	return &domain.LastRun{ID: *id, Status: domain.RunStatus(*status), CreatedAt: *createdAt}
}

func taskSummaryFromRow(row gen.TaskListRow) domain.TaskSummary {
	return domain.TaskSummary{
		ID:   row.ID,
		App:  row.App,
		Name: row.Name,
		Schedule: taskSchedule(taskScheduleColumns{
			cron:       row.ScheduleCron,
			overlap:    row.ScheduleOverlapPolicy,
			nextTickAt: row.ScheduleNextTickAt,
			pausedAt:   row.SchedulePausedAt,
		}),
		LastRun:          lastRun(row.LastRunID, row.LastRunStatus, row.LastRunCreatedAt),
		LiveWorkerCount:  int(row.LiveWorkerCount),
		LastRegisteredAt: row.LastRegisteredAt,
	}
}

func taskDetailFromRow(row gen.TaskGetRow, workers []gen.WorkerListLiveByTaskRow) (domain.TaskDetail, error) {
	var retry *domain.RetryPolicy
	if row.RetryPolicy != nil {
		var p retryPolicyJSON
		if err := json.Unmarshal(*row.RetryPolicy, &p); err != nil {
			return domain.TaskDetail{}, fmt.Errorf("decode retry policy: %w", err)
		}

		policy, err := retryPolicyFromJSON(p)
		if err != nil {
			return domain.TaskDetail{}, fmt.Errorf("decode retry policy: %w", err)
		}

		retry = &policy
	}

	task := domain.TaskDetail{
		TaskSummary: domain.TaskSummary{
			ID:   row.ID,
			App:  row.App,
			Name: row.Name,
			Schedule: taskSchedule(taskScheduleColumns{
				cron:       row.ScheduleCron,
				overlap:    row.ScheduleOverlapPolicy,
				nextTickAt: row.ScheduleNextTickAt,
				pausedAt:   row.SchedulePausedAt,
			}),
			LastRun:          lastRun(row.LastRunID, row.LastRunStatus, row.LastRunCreatedAt),
			LiveWorkerCount:  len(workers),
			LastRegisteredAt: row.LastRegisteredAt,
		},
		Retry:         retry,
		StartDeadline: duration(row.StartDeadline),
		Workers:       make([]domain.TaskWorker, 0, len(workers)),
		CreatedAt:     row.CreatedAt,
	}

	for _, worker := range workers {
		task.Workers = append(task.Workers, domain.TaskWorker{
			ID:          worker.ID,
			Slots:       int(worker.Slots),
			ConnectedAt: worker.ConnectedAt,
			HeartbeatAt: worker.HeartbeatAt,
		})
	}

	return task, nil
}
