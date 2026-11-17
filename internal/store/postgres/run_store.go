package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.RunStore = (*Store)(nil)

// CompleteStep stores the outcome of a Do Step in one transaction and returns the stored
// record. A record of the same kind already stored is returned as it is: a resend
// changes nothing. A failed try that decide sends to a retry ends the attempt instead:
// the Run waits in run_delayed and no record is stored. It returns domain.ErrStaleLease
// when the lease is not this attempt's.
func (s *Store) CompleteStep(
	ctx context.Context,
	params domain.CompleteStepParams,
	decide domain.RetryDecider,
) (domain.StepResult, error) {
	lease := params.Lease
	var result domain.StepResult
	err := s.db.WithTx(ctx, func(tx DB) error {
		held, err := s.queries.LeaseLock(ctx, tx, gen.LeaseLockParams{
			RunID:      lease.RunID,
			LeaseEpoch: lease.Epoch,
			WorkerID:   params.WorkerID,
		})
		if isNoRowsError(err) {
			return domain.ErrStaleLease
		}

		if err != nil {
			return fmt.Errorf("lock lease: %w", err)
		}

		stored, found, err := s.getStep(ctx, tx, lease.RunID, params.StepID)
		if err != nil {
			return fmt.Errorf("read step: %w", err)
		}

		if found && stored.Kind == domain.StepKindDo {
			result.Record = stored
			return nil
		}

		tries := 1
		if held.RetryStepID != nil && *held.RetryStepID == params.StepID {
			tries = int(held.RetryCount) + 1
		}

		if params.Outcome.Failure != nil {
			retry, err := s.scheduleRetry(ctx, tx, scheduleRetryParams{
				Step:   params,
				Tries:  tries,
				Config: held.Config,
				Decide: decide,
			})
			if err != nil {
				return fmt.Errorf("schedule retry: %w", err)
			}

			if retry != nil {
				result.Retry = retry
				return nil
			}
		}

		record, err := s.createStep(ctx, tx, params, tries)
		if err != nil {
			return fmt.Errorf("create step: %w", err)
		}

		result.Record = record

		return nil
	})
	if err != nil {
		return domain.StepResult{}, fmt.Errorf("complete step in transaction: %w", err)
	}

	return result, nil
}

// scheduleRetryParams is a failed try of a Step that may be tried again.
type scheduleRetryParams struct {
	Step domain.CompleteStepParams
	// Tries counts the tries of the Step, this one included.
	Tries int
	// Config is the Run's runs.config.
	Config json.RawMessage
	Decide domain.RetryDecider
}

// scheduleRetry asks Decide whether the failed try is tried again and, when it is, moves
// the Run to run_delayed. It returns nil when the failure is final.
func (s *Store) scheduleRetry(ctx context.Context, tx DB, params scheduleRetryParams) (*domain.ScheduledRetry, error) {
	runPolicy, err := runRetryPolicy(params.Config)
	if err != nil {
		return nil, fmt.Errorf("read run retry policy: %w", err)
	}

	delay, retry := params.Decide(params.Tries, runPolicy)
	if !retry {
		return nil, nil //nolint:nilnil // No retry is not an error.
	}

	step := params.Step
	row, err := s.queries.LeaseScheduleStepRetry(ctx, tx, gen.LeaseScheduleStepRetryParams{
		Delay:      interval(&delay),
		StepID:     step.StepID,
		RunID:      step.Lease.RunID,
		LeaseEpoch: step.Lease.Epoch,
		WorkerID:   step.WorkerID,
	})
	if err != nil {
		return nil, fmt.Errorf("move run to run_delayed: %w", err)
	}

	return &domain.ScheduledRetry{NextAttemptAt: row.ReadyAt, TryCount: int(row.RetryCount)}, nil
}

// createStep stores the record of a Step that succeeded, or failed for good, at its try
// number tries.
func (s *Store) createStep(ctx context.Context, tx DB, params domain.CompleteStepParams, tries int) (domain.StepRecord, error) {
	step := gen.StepCreateParams{
		RunID:    params.Lease.RunID,
		StepID:   params.StepID,
		Status:   string(params.Outcome.Status()),
		Output:   params.Outcome.Output,
		TryCount: int32(tries), //nolint:gosec // retry_count is a smallint.
	}
	if failure := params.Outcome.Failure; failure != nil {
		code := string(failure.Code)
		step.Output = nil
		step.ErrorCode = &code
		step.ErrorMessage = &failure.Message
	}

	if _, err := s.queries.StepCreate(ctx, tx, step); err != nil {
		return domain.StepRecord{}, fmt.Errorf("insert step: %w", err)
	}

	err := s.queries.LeaseResetStepRetry(ctx, tx, gen.LeaseResetStepRetryParams{RunID: params.Lease.RunID, StepID: params.StepID})
	if err != nil {
		return domain.StepRecord{}, fmt.Errorf("reset step retry: %w", err)
	}

	return stepRecord(stepRecordParams{
		StepID:       step.StepID,
		Kind:         string(domain.StepKindDo),
		Status:       step.Status,
		Output:       step.Output,
		ErrorCode:    step.ErrorCode,
		ErrorMessage: step.ErrorMessage,
	}), nil
}

func (s *Store) getStep(ctx context.Context, tx DB, runID uuid.UUID, stepID string) (domain.StepRecord, bool, error) {
	row, err := s.queries.StepGet(ctx, tx, gen.StepGetParams{RunID: runID, StepID: stepID})
	if isNoRowsError(err) {
		return domain.StepRecord{}, false, nil
	}

	if err != nil {
		return domain.StepRecord{}, false, fmt.Errorf("get step: %w", err)
	}

	return stepRecord(stepRecordParams(row)), true, nil
}

// FinishRun ends the Run of the attempt that holds lease and reports whether this call
// ended it. A Run that has already ended is not changed, and the call succeeds, so a
// resend is safe. It returns domain.ErrStaleLease when the lease is not this attempt's and
// the Run has not ended.
func (s *Store) FinishRun(ctx context.Context, workerID uuid.UUID, lease domain.Lease, end domain.RunEnd) (bool, error) {
	params := gen.RunFinishParams{
		RunID:      lease.RunID,
		LeaseEpoch: lease.Epoch,
		WorkerID:   workerID,
		Status:     string(end.Status),
		Output:     end.Output,
	}
	if end.Failure != nil {
		code := string(end.Failure.Code)
		params.Output = nil
		params.ErrorCode = &code
		params.ErrorMessage = &end.Failure.Message
	}

	_, err := s.queries.RunFinish(ctx, s.db, params)
	if err == nil {
		return true, nil
	}

	if !isNoRowsError(err) {
		return false, fmt.Errorf("finish run: %w", err)
	}

	status, err := s.queries.RunGetStatus(ctx, s.db, lease.RunID)
	if isNoRowsError(err) {
		return false, domain.ErrStaleLease
	}

	if err != nil {
		return false, fmt.Errorf("get run status: %w", err)
	}

	if !domain.RunStatus(status).Ended() {
		return false, domain.ErrStaleLease
	}

	return false, nil
}

// PromoteDueDelayed moves at most batchSize Runs whose wait in run_delayed is over to
// run_queue, and returns how many it moved.
func (s *Store) PromoteDueDelayed(ctx context.Context, batchSize int) (int, error) {
	ids, err := s.queries.DelayedPromoteDue(ctx, s.db, int32(batchSize)) //nolint:gosec // A small batch size.
	if err != nil {
		return 0, fmt.Errorf("promote due delayed runs: %w", err)
	}

	return len(ids), nil
}

// ExpireStartDeadlines fails at most batchSize queued Runs past their start deadline, and
// returns how many it failed.
func (s *Store) ExpireStartDeadlines(ctx context.Context, batchSize int) (int, error) {
	ids, err := s.queries.QueueExpireStartDeadlines(ctx, s.db, int32(batchSize)) //nolint:gosec // A small batch size.
	if err != nil {
		return 0, fmt.Errorf("expire start deadlines: %w", err)
	}

	return len(ids), nil
}

// FireDueSchedules fires at most batchSize schedules whose tick has come, in one
// transaction: each makes a queued Run, unless it skips overlaps and its last Run is
// queued or running, and moves to its first tick after the database clock. So a schedule
// that missed ticks while the server was down fires once (catch-up latest). The last Runs
// are read after the schedules are locked, so a Run that another server made for the
// previous tick counts.
func (s *Store) FireDueSchedules(ctx context.Context, batchSize int) (domain.ScheduleTicks, error) {
	var ticks domain.ScheduleTicks
	err := s.db.WithTx(ctx, func(tx DB) error {
		rows, err := s.queries.ScheduleTakeDue(ctx, tx, int32(batchSize)) //nolint:gosec // A small batch size.
		if err != nil {
			return fmt.Errorf("take due schedules: %w", err)
		}

		if len(rows) == 0 {
			return nil
		}

		now := rows[0].Now
		active, err := s.listActiveLastRuns(ctx, tx, rows)
		if err != nil {
			return err
		}

		advance := gen.ScheduleAdvanceParams{}
		runs := gen.RunCreateScheduledParams{}
		for _, row := range rows {
			c, err := domain.ParseCron(row.Cron)
			if err != nil {
				// Register parses every cron before it stores it, so only a changed parser
				// gets here. The schedule stays due and is reported at each sweep.
				ticks.Broken++
				continue
			}

			runID := uuid.Nil()
			if domain.OverlapPolicy(row.OverlapPolicy) == domain.OverlapSkip && row.LastRunID != nil && active[*row.LastRunID] {
				ticks.Skipped++
			} else {
				runID = uuid.NewV7()
				runs.Ids = append(runs.Ids, runID)
				runs.TaskIds = append(runs.TaskIds, row.TaskID)
				ticks.Fired++
			}

			advance.Ids = append(advance.Ids, row.ID)
			advance.NextTickAts = append(advance.NextTickAts, c.Next(now))
			advance.RunIds = append(advance.RunIds, runID)
		}

		if len(runs.Ids) > 0 {
			if err := s.queries.RunCreateScheduled(ctx, tx, runs); err != nil {
				return fmt.Errorf("create scheduled runs: %w", err)
			}
		}

		if len(advance.Ids) > 0 {
			if err := s.queries.ScheduleAdvance(ctx, tx, advance); err != nil {
				return fmt.Errorf("advance schedules: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return domain.ScheduleTicks{}, fmt.Errorf("fire due schedules in transaction: %w", err)
	}

	return ticks, nil
}

// listActiveLastRuns returns which last Runs of the skip schedules among rows are queued
// or running. It reads them after ScheduleTakeDue holds the schedules' locks.
func (s *Store) listActiveLastRuns(ctx context.Context, tx DB, rows []gen.ScheduleTakeDueRow) (map[uuid.UUID]bool, error) {
	var lastIDs []uuid.UUID
	for _, row := range rows {
		if domain.OverlapPolicy(row.OverlapPolicy) == domain.OverlapSkip && row.LastRunID != nil {
			lastIDs = append(lastIDs, *row.LastRunID)
		}
	}

	if len(lastIDs) == 0 {
		return nil, nil
	}

	ids, err := s.queries.RunListActive(ctx, tx, lastIDs)
	if err != nil {
		return nil, fmt.Errorf("list active last runs: %w", err)
	}

	active := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		active[id] = true
	}

	return active, nil
}

// ListRuns returns at most params.Limit Runs of the project that match the filter, newest
// first, each with its Task.
func (s *Store) ListRuns(ctx context.Context, params store.ListRunsParams) ([]domain.RunSummary, error) {
	statuses := make([]string, 0, len(params.Filter.Statuses))
	for _, status := range params.Filter.Statuses {
		statuses = append(statuses, string(status))
	}

	rows, err := s.queries.RunList(ctx, s.db, gen.RunListParams{
		ProjectID: params.ProjectID,
		Statuses:  statuses,
		TaskID:    params.Filter.TaskID,
		AfterID:   params.AfterID,
		PageLimit: params.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	runs := make([]domain.RunSummary, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, runSummaryFromRow(row))
	}

	return runs, nil
}

// GetRunDetail returns the Run of the project with its Steps, and the queue, delay or
// lease it is in. It returns domain.ErrRunNotFound when the project has no such Run.
// Both reads share one snapshot, so the Steps match the Run's state.
func (s *Store) GetRunDetail(ctx context.Context, projectID, runID uuid.UUID) (domain.RunDetail, error) {
	var (
		row   gen.RunGetWithAttemptRow
		steps []gen.StepListByRunRow
	)
	err := s.db.WithTx(ctx, func(tx DB) error {
		if err := s.queries.RunReadSnapshot(ctx, tx); err != nil {
			return fmt.Errorf("set snapshot: %w", err)
		}

		var err error
		row, err = s.queries.RunGetWithAttempt(ctx, tx, gen.RunGetWithAttemptParams{ProjectID: projectID, ID: runID})
		if err != nil {
			if isNoRowsError(err) {
				return domain.ErrRunNotFound
			}

			return fmt.Errorf("get run: %w", err)
		}

		steps, err = s.queries.StepListByRun(ctx, tx, runID)
		if err != nil {
			return fmt.Errorf("list steps: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.RunDetail{}, err
	}

	run, err := runDetailFromRow(row, steps)
	if err != nil {
		return domain.RunDetail{}, fmt.Errorf("read run %s: %w", runID, err)
	}

	return run, nil
}
