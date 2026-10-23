package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres/sqlc"
)

// RunStore keeps Runs, their Steps, the idempotency keys that created them, and the
// removal of finished Runs. It implements the engine's run queue and run store.
type RunStore struct {
	db DB
	q  *sqlc.Queries
}

// NewRunStore creates a RunStore on db.
func NewRunStore(db DB) *RunStore {
	return &RunStore{db: db, q: sqlc.New()}
}

// errKeyRetry reports that the key holder changed between the insert and the read.
var errKeyRetry = errors.New("idempotency key changed while it was read")

// CreateRun stores a new Run under its idempotency key, and the Steps it copies, in
// one transaction. An expired key that the prune has not removed yet counts as absent:
// the insert takes it over, replacing its fingerprint, Run ID, and window, so creating
// a Run never deletes a key. When an unexpired key already exists, nothing is stored
// and the holder is returned with created false. The Run is stored with the ID of the
// key record.
func (s *RunStore) CreateRun(
	ctx context.Context,
	run domain.Run,
	key domain.KeyRecord,
	copied []domain.StepRecord,
) (domain.KeyHolder, bool, error) {
	for range 3 {
		holder, created, err := s.createRunOnce(ctx, run, key, copied)
		if errors.Is(err, errKeyRetry) {
			continue
		}

		return holder, created, err
	}

	return domain.KeyHolder{}, false, errKeyRetry
}

func (s *RunStore) createRunOnce(
	ctx context.Context,
	run domain.Run,
	key domain.KeyRecord,
	copied []domain.StepRecord,
) (domain.KeyHolder, bool, error) {
	var holder domain.KeyHolder
	created := false
	err := s.db.WithTx(ctx, func(tx DB) error {
		var err error
		holder, created, err = s.insertRunWithKey(ctx, tx, run, key, copied)
		return err
	})
	if err != nil {
		return domain.KeyHolder{}, false, err
	}

	return holder, created, nil
}

// insertRunWithKey runs in tx: it inserts the key and the Run, or returns the holder of an
// existing key. The insert takes over an expired key, so the key is new when the insert
// affects a row. It affects none when a live key holds the name.
func (s *RunStore) insertRunWithKey(
	ctx context.Context,
	tx DB,
	run domain.Run,
	key domain.KeyRecord,
	copied []domain.StepRecord,
) (domain.KeyHolder, bool, error) {
	n, err := s.q.IdempotencyInsert(ctx, tx, sqlc.IdempotencyInsertParams{
		IdempotencyKey: key.Key,
		Fingerprint:    key.Fingerprint,
		RunID:          key.RunID,
		CreatedAt:      key.CreatedAt,
		ExpiresAt:      key.ExpiresAt,
	})
	if err != nil {
		return domain.KeyHolder{}, false, fmt.Errorf("insert key: %w", err)
	}

	if n == 0 {
		return s.readKeyHolder(ctx, tx, key)
	}

	run.ID = key.RunID
	params, err := runCreateParams(run)
	if err != nil {
		return domain.KeyHolder{}, false, err
	}

	if _, err := s.q.RunCreate(ctx, tx, params); err != nil {
		if isRerunSourceGone(err) {
			// The original Run was removed after the caller read it. Nothing is stored.
			return domain.KeyHolder{}, false, domain.ErrNotFound
		}

		return domain.KeyHolder{}, false, fmt.Errorf("create run: %w", err)
	}

	if err := s.copySteps(ctx, tx, run.ID, copied); err != nil {
		return domain.KeyHolder{}, false, err
	}

	return domain.KeyHolder{}, true, nil
}

// readKeyHolder reads the key that already exists, or returns errKeyRetry when it is gone.
func (s *RunStore) readKeyHolder(ctx context.Context, tx DB, key domain.KeyRecord) (domain.KeyHolder, bool, error) {
	row, err := s.q.IdempotencyGet(ctx, tx, sqlc.IdempotencyGetParams{
		IdempotencyKey: key.Key,
		Now:            key.CreatedAt,
	})
	if isNoRowsError(err) {
		return domain.KeyHolder{}, false, errKeyRetry
	}

	if err != nil {
		return domain.KeyHolder{}, false, fmt.Errorf("get key: %w", err)
	}

	return domain.KeyHolder{RunID: row.RunID, Fingerprint: row.Fingerprint}, false, nil
}

// copySteps stores the Steps that a rerun copies into its new Run, renumbered from 1.
func (s *RunStore) copySteps(ctx context.Context, tx DB, runID string, copied []domain.StepRecord) error {
	for i, step := range copied {
		step.Position = i + 1
		stepParams, err := stepUpsertParams(runID, step)
		if err != nil {
			return err
		}

		if err := s.q.StepUpsert(ctx, tx, stepParams); err != nil {
			return fmt.Errorf("copy step: %w", err)
		}
	}

	return nil
}

// RunByKey returns the Run that an unexpired key created, or domain.ErrNotFound.
func (s *RunStore) RunByKey(ctx context.Context, key string, now time.Time) (domain.Run, error) {
	row, err := s.q.RunGetByIdempotencyKey(ctx, s.db, sqlc.RunGetByIdempotencyKeyParams{
		IdempotencyKey: key,
		Now:            now,
	})
	if isNoRowsError(err) {
		return domain.Run{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Run{}, fmt.Errorf("get run by key: %w", err)
	}

	return runFromRow(row)
}

// RunStatus returns the status of the Run, or domain.ErrNotFound. It reads two
// columns, so a poll costs no more than the status.
func (s *RunStore) RunStatus(ctx context.Context, id string) (domain.RunStatus, error) {
	row, err := s.q.RunGetStatus(ctx, s.db, id)
	if isNoRowsError(err) {
		return "", domain.ErrNotFound
	}

	if err != nil {
		return "", fmt.Errorf("get run status: %w", err)
	}

	return domain.RunStatus(row.Status), nil
}

// GetRun returns the Run, or domain.ErrNotFound.
func (s *RunStore) GetRun(ctx context.Context, id string) (domain.Run, error) {
	row, err := s.q.RunGet(ctx, s.db, id)
	if isNoRowsError(err) {
		return domain.Run{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Run{}, fmt.Errorf("get run: %w", err)
	}

	return runFromRow(row)
}

// ListRuns returns one page of Runs, newest first, after the filter's cursor. The
// creation window is half-open. Each Run is returned without its payloads.
func (s *RunStore) ListRuns(ctx context.Context, f domain.RunFilter) ([]domain.Run, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, status := range f.Statuses {
		statuses = append(statuses, string(status))
	}

	params := sqlc.RunListPageParams{
		WorkerID:       f.WorkerID,
		TaskID:         f.TaskID,
		Statuses:       statuses,
		UnfinishedOnly: onlyUnfinished(f.Statuses),
		CreatedAfter:   f.CreatedAfter,
		CreatedBefore:  f.CreatedBefore,
		RowLimit:       clampInt32(f.Limit),
	}

	if f.After != nil {
		params.HasCursor = true
		params.CursorCreatedAt = f.After.CreatedAt
		params.CursorID = f.After.ID
	}

	rows, err := s.q.RunListPage(ctx, planPerCallDB{db: s.db}, params)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	runs := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		run, err := summaryFromRow(row)
		if err != nil {
			return nil, err
		}

		runs = append(runs, run)
	}

	return runs, nil
}

// onlyUnfinished reports whether the statuses are all unfinished, and there is at least one.
// Such a filter reads only the Runs without a finished time, which is what the index of the
// unfinished Runs holds.
func onlyUnfinished(statuses []domain.RunStatus) bool {
	if len(statuses) == 0 {
		return false
	}

	for _, status := range statuses {
		if status != domain.RunStatusQueued && status != domain.RunStatusRunning {
			return false
		}
	}

	return true
}

// summaryFromRow reads a Run from a listing row. The row has no payloads, so the
// Run's input and output stay empty.
func summaryFromRow(row sqlc.RunListPageRow) (domain.Run, error) {
	run := domain.Run{
		ID:            row.ID,
		WorkerID:      row.WorkerID,
		TaskID:        row.TaskID,
		Status:        domain.RunStatus(row.Status),
		QueuedReason:  domain.QueuedReason(row.QueuedReason),
		Source:        domain.RunSource(row.Source),
		Attempts:      int(row.Attempts),
		InstanceID:    row.InstanceID,
		DueAt:         row.DueAt,
		CreatedAt:     row.CreatedAt,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		NextAttemptAt: row.NextAttemptAt,
		RerunOf:       row.RerunOf,
	}

	failure, err := failureFromRaw(row.Failure)
	if err != nil {
		return domain.Run{}, err
	}

	run.Failure = failure
	return run, nil
}

// ListSteps returns the Step records of the Run in the order they were first reported.
func (s *RunStore) ListSteps(ctx context.Context, runID string) ([]domain.StepRecord, error) {
	rows, err := s.q.StepListByRun(ctx, s.db, runID)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}

	steps := make([]domain.StepRecord, 0, len(rows))
	for _, row := range rows {
		step, err := stepFromRow(row)
		if err != nil {
			return nil, err
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// ExpireNeverStarted fails up to limit queued Runs that never started and whose due
// time plus their Task's start deadline has passed at now. It returns the references of the Runs it failed.
func (s *RunStore) ExpireNeverStarted(ctx context.Context, now time.Time, limit int) ([]domain.RunRef, error) {
	rows, err := s.q.RunExpireNeverStarted(ctx, s.db, sqlc.RunExpireNeverStartedParams{
		Now:            &now,
		FailureCode:    string(domain.FailureCodeStartDeadline),
		FailureMessage: domain.StartDeadlineMessage,
		RowLimit:       clampInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("expire never started runs: %w", err)
	}

	refs := make([]domain.RunRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.RunRef{ID: row.ID, WorkerID: row.WorkerID, TaskID: row.TaskID})
	}

	return refs, nil
}

// ExpireWorkerWaits fails up to limit queued Runs that have waited for a worker for
// longer than the wait limit at now. It returns the references of the Runs it failed.
func (s *RunStore) ExpireWorkerWaits(ctx context.Context, now time.Time, wait time.Duration, limit int) ([]domain.RunRef, error) {
	cutoff := now.Add(-wait)
	rows, err := s.q.RunExpireWorkerWaits(ctx, s.db, sqlc.RunExpireWorkerWaitsParams{
		Now:            &now,
		FailureCode:    string(domain.FailureCodeWorkerWaitLimit),
		FailureMessage: domain.WorkerWaitLimitMessage,
		Cutoff:         &cutoff,
		RowLimit:       clampInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("expire worker waits: %w", err)
	}

	refs := make([]domain.RunRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.RunRef{ID: row.ID, WorkerID: row.WorkerID, TaskID: row.TaskID})
	}

	return refs, nil
}

// ListExpiredLeases returns up to limit running Runs whose lease expired at or before now.
func (s *RunStore) ListExpiredLeases(ctx context.Context, now time.Time, limit int) ([]string, error) {
	ids, err := s.q.RunListExpiredLeases(ctx, s.db, sqlc.RunListExpiredLeasesParams{
		Now:      &now,
		RowLimit: clampInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list expired leases: %w", err)
	}

	return ids, nil
}

// RenewLeases renews the leases of the attempts of one instance in one transaction,
// and records the Step each attempt is running with its policy. It returns the
// attempts whose lease is no longer current. The Steps and payloads are not read.
func (s *RunStore) RenewLeases(
	ctx context.Context,
	instanceID string,
	attempts []domain.AttemptRef,
	expiresAt time.Time,
) ([]domain.AttemptRef, error) {
	var stale []domain.AttemptRef
	err := s.db.WithTx(ctx, func(tx DB) error {
		stale = stale[:0]
		for _, attempt := range attempts {
			var policy *json.RawMessage
			if attempt.StepID != "" {
				raw, err := retryRawParams(attempt.Retry)
				if err != nil {
					return err
				}

				policy = raw
			}

			n, err := s.q.RunRenewLeaseStep(ctx, tx, sqlc.RunRenewLeaseStepParams{
				LeaseExpiresAt:     &expiresAt,
				RunningStepID:      attempt.StepID,
				RunningStepAttempt: clampInt32(attempt.StepAttempt),
				RunningStepPolicy:  policy,
				ID:                 attempt.RunID,
				LeaseToken:         attempt.Token,
				InstanceID:         instanceID,
			})
			if err != nil {
				return fmt.Errorf("renew lease: %w", err)
			}

			if n == 0 {
				stale = append(stale, attempt)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return stale, nil
}

// CreateTickRun inserts the Run of one schedule tick. The insert does nothing when the
// Task already has a schedule Run with the tick's due time, so a repeated insert after
// a crash is harmless. It reports whether a Run was inserted.
func (s *RunStore) CreateTickRun(ctx context.Context, run domain.Run) (bool, error) {
	rowID, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("new run id: %w", err)
	}

	n, err := s.q.RunInsertTick(ctx, s.db, sqlc.RunInsertTickParams{
		ID:           rowID.String(),
		WorkerID:     run.WorkerID,
		TaskID:       run.TaskID,
		QueuedReason: string(run.QueuedReason),
		DueAt:        run.DueAt,
		CreatedAt:    run.CreatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("create tick run: %w", err)
	}

	return n > 0, nil
}

// HasUnfinishedScheduled reports whether the Task has a queued or running Run of its schedule.
func (s *RunStore) HasUnfinishedScheduled(ctx context.Context, workerID, taskID string) (bool, error) {
	unfinished, err := s.q.RunHasUnfinishedScheduled(ctx, s.db, sqlc.RunHasUnfinishedScheduledParams{
		WorkerID: workerID,
		TaskID:   taskID,
	})
	if err != nil {
		return false, fmt.Errorf("check unfinished runs: %w", err)
	}

	return unfinished, nil
}

// ClaimNext takes the oldest queued Run of the Task that is due, and starts an
// attempt on it. It reports false when no such Run was free to take.
func (s *RunStore) ClaimNext(ctx context.Context, workerID, taskID string, claim domain.Claim) (domain.Run, bool, error) {
	row, err := s.q.RunClaimNext(ctx, s.db, sqlc.RunClaimNextParams{
		WorkerID:       workerID,
		TaskID:         taskID,
		LeaseToken:     claim.LeaseToken,
		LeaseExpiresAt: &claim.LeaseExpiresAt,
		InstanceID:     claim.InstanceID,
		Now:            &claim.Now,
	})
	if isNoRowsError(err) {
		return domain.Run{}, false, nil
	}

	if err != nil {
		return domain.Run{}, false, fmt.Errorf("claim run: %w", err)
	}

	run, err := runFromRow(row)
	if err != nil {
		return domain.Run{}, false, err
	}

	return run, true, nil
}

// Mutate runs fn on the Run and its Steps under the Run's row lock, in one
// transaction, and saves what fn returns. fn decides; it must not do I/O. When fn
// returns an error, nothing is saved and the error is returned as it is. A missing
// Run gives domain.ErrNotFound.
func (s *RunStore) Mutate(
	ctx context.Context,
	id string,
	fn func(domain.Snapshot) (domain.Write, error),
) error {
	return s.mutate(ctx, id, true, fn)
}

// MutateRun is Mutate for a decision that needs only the Run: its Steps are not read.
// The snapshot it gets has no Steps.
func (s *RunStore) MutateRun(
	ctx context.Context,
	id string,
	fn func(domain.Snapshot) (domain.Write, error),
) error {
	return s.mutate(ctx, id, false, fn)
}

func (s *RunStore) mutate(
	ctx context.Context,
	id string,
	withSteps bool,
	fn func(domain.Snapshot) (domain.Write, error),
) error {
	return s.db.WithTx(ctx, func(tx DB) error {
		row, err := s.q.RunLock(ctx, tx, id)
		if isNoRowsError(err) {
			return domain.ErrNotFound
		}

		if err != nil {
			return fmt.Errorf("lock run: %w", err)
		}

		run, err := runFromRow(row)
		if err != nil {
			return err
		}

		var steps []domain.StepRecord
		if withSteps {
			steps, err = s.readSteps(ctx, tx, id)
			if err != nil {
				return err
			}
		}

		write, err := fn(domain.Snapshot{Run: run, Steps: steps})
		if err != nil {
			return err
		}

		params, err := runUpdateParams(write.Run)
		if err != nil {
			return err
		}

		if err := s.q.RunUpdate(ctx, tx, params); err != nil {
			return fmt.Errorf("update run: %w", err)
		}

		if write.Step != nil {
			stepParams, err := stepUpsertParams(id, *write.Step)
			if err != nil {
				return err
			}

			if err := s.q.StepUpsert(ctx, tx, stepParams); err != nil {
				return fmt.Errorf("upsert step: %w", err)
			}
		}

		return nil
	})
}

// readSteps reads the Step records of a Run, in the order they were first reported.
func (s *RunStore) readSteps(ctx context.Context, tx DB, id string) ([]domain.StepRecord, error) {
	stepRows, err := s.q.StepListByRun(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}

	steps := make([]domain.StepRecord, 0, len(stepRows))
	for _, stepRow := range stepRows {
		step, err := stepFromRow(stepRow)
		if err != nil {
			return nil, err
		}

		steps = append(steps, step)
	}

	return steps, nil
}

// ListQueuedTasks returns the Tasks that have at least one queued Run.
func (s *RunStore) ListQueuedTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.q.RunListQueuedTasks(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("list queued tasks: %w", err)
	}

	refs := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.Task{WorkerID: row.WorkerID, TaskID: row.TaskID})
	}

	return refs, nil
}

// SetQueuedReason sets why the due queued Runs of the Task wait. Runs that are not due yet keep their reason.
func (s *RunStore) SetQueuedReason(
	ctx context.Context,
	workerID, taskID string,
	reason domain.QueuedReason,
	now time.Time,
) error {
	_, err := s.q.RunSetQueuedReason(ctx, s.db, sqlc.RunSetQueuedReasonParams{
		QueuedReason: string(reason),
		WorkerID:     workerID,
		TaskID:       taskID,
		Now:          now,
	})
	if err != nil {
		return fmt.Errorf("set queued reason: %w", err)
	}

	return nil
}

// CountRunning returns the number of attempts the instance holds.
func (s *RunStore) CountRunning(ctx context.Context, instanceID string) (int, error) {
	n, err := s.q.RunCountRunning(ctx, s.db, instanceID)
	if err != nil {
		return 0, fmt.Errorf("count running: %w", err)
	}

	return int(n), nil
}

// DeleteFinishedBefore removes up to limit finished Runs that finished before cutoff,
// with their Step records, in one transaction. It returns how many Runs it removed.
// A Run that a rerun refers to is kept as the rerun's history: the reference is cleared.
func (s *RunStore) DeleteFinishedBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	removed := 0
	err := s.db.WithTx(ctx, func(tx DB) error {
		ids, err := s.q.RunListFinishedBefore(ctx, tx, sqlc.RunListFinishedBeforeParams{
			Cutoff:   &cutoff,
			RowLimit: clampInt32(limit),
		})
		if err != nil {
			return fmt.Errorf("list finished runs: %w", err)
		}

		if len(ids) == 0 {
			return nil
		}

		if _, err := s.q.StepDeleteByRunIDs(ctx, tx, ids); err != nil {
			return fmt.Errorf("delete steps: %w", err)
		}

		n, err := s.q.RunDeleteByIDs(ctx, tx, ids)
		if err != nil {
			return fmt.Errorf("delete runs: %w", err)
		}

		removed = int(n)
		return nil
	})
	if err != nil {
		return 0, err
	}

	return removed, nil
}

// PruneKeys removes up to limit idempotency keys whose window has passed at now. A
// Run is never removed by this: only its key record goes.
func (s *RunStore) PruneKeys(ctx context.Context, now time.Time, limit int) (int, error) {
	n, err := s.q.IdempotencyPruneExpired(ctx, s.db, sqlc.IdempotencyPruneExpiredParams{
		Now:      now,
		RowLimit: clampInt32(limit),
	})
	if err != nil {
		return 0, fmt.Errorf("prune keys: %w", err)
	}

	return int(n), nil
}

// failureJSON is how a Failure is stored in the failure column of a Run or a Step.
type failureJSON struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Attempts int    `json:"attempts"`
	StepID   string `json:"step_id,omitempty"`
}

func failureToJSON(f domain.Failure) failureJSON {
	return failureJSON{
		Code:     string(f.Code),
		Message:  f.Message,
		Attempts: f.Attempts,
		StepID:   f.StepID,
	}
}

func failureFromRaw(raw *json.RawMessage) (*domain.Failure, error) {
	if raw == nil {
		return nil, nil
	}

	var fj failureJSON
	if err := json.Unmarshal(*raw, &fj); err != nil {
		return nil, fmt.Errorf("unmarshal failure: %w", err)
	}

	return &domain.Failure{
		Code:     domain.FailureCode(fj.Code),
		Message:  fj.Message,
		Attempts: fj.Attempts,
		StepID:   fj.StepID,
	}, nil
}

func failureParams(f *domain.Failure) (*json.RawMessage, error) {
	if f == nil {
		return nil, nil
	}

	raw, err := json.Marshal(failureToJSON(*f))
	if err != nil {
		return nil, fmt.Errorf("marshal failure: %w", err)
	}

	msg := json.RawMessage(raw)
	return &msg, nil
}

func runCreateParams(run domain.Run) (sqlc.RunCreateParams, error) {
	params := sqlc.RunCreateParams{
		ID:           run.ID,
		WorkerID:     run.WorkerID,
		TaskID:       run.TaskID,
		Status:       string(run.Status),
		QueuedReason: string(run.QueuedReason),
		Source:       string(run.Source),
		Input:        run.Input,
		DueAt:        run.DueAt,
		CreatedAt:    run.CreatedAt,
	}

	params.RerunOf = run.RerunOf

	return params, nil
}

func runFromRow(row sqlc.Run) (domain.Run, error) {
	run := domain.Run{
		ID:                  row.ID,
		WorkerID:            row.WorkerID,
		TaskID:              row.TaskID,
		Status:              domain.RunStatus(row.Status),
		QueuedReason:        domain.QueuedReason(row.QueuedReason),
		Source:              domain.RunSource(row.Source),
		Attempts:            int(row.Attempts),
		Input:               row.Input,
		Output:              row.Output,
		LeaseToken:          row.LeaseToken,
		LeaseExpiresAt:      row.LeaseExpiresAt,
		InstanceID:          row.InstanceID,
		AckedAt:             row.AckedAt,
		DueAt:               row.DueAt,
		CreatedAt:           row.CreatedAt,
		StartedAt:           row.StartedAt,
		FinishedAt:          row.FinishedAt,
		HandlerAttemptsUsed: int(row.HandlerAttemptsUsed),
		EndedToken:          row.EndedToken,
		EndedKind:           domain.EndKind(row.EndedKind),
		NextAttemptAt:       row.NextAttemptAt,
		EndedAt:             row.EndedAt,
		RunningStepID:       row.RunningStepID,
		RunningStepAttempt:  int(row.RunningStepAttempt),
		WorkerWaitSince:     row.WorkerWaitSince,
		RerunOf:             row.RerunOf,
	}

	failure, err := failureFromRaw(row.Failure)
	if err != nil {
		return domain.Run{}, err
	}

	run.Failure = failure
	if row.RunningStepPolicy != nil {
		policy, err := retryFromRaw(*row.RunningStepPolicy)
		if err != nil {
			return domain.Run{}, err
		}

		run.RunningStepPolicy = &policy
	}

	return run, nil
}

func runUpdateParams(run domain.Run) (sqlc.RunUpdateParams, error) {
	failure, err := failureParams(run.Failure)
	if err != nil {
		return sqlc.RunUpdateParams{}, err
	}

	policy, err := retryRawParams(run.RunningStepPolicy)
	if err != nil {
		return sqlc.RunUpdateParams{}, err
	}

	return sqlc.RunUpdateParams{
		ID:                  run.ID,
		Status:              string(run.Status),
		QueuedReason:        string(run.QueuedReason),
		Attempts:            clampInt32(run.Attempts),
		Output:              run.Output,
		Failure:             failure,
		LeaseToken:          run.LeaseToken,
		LeaseExpiresAt:      run.LeaseExpiresAt,
		InstanceID:          run.InstanceID,
		AckedAt:             run.AckedAt,
		NextAttemptAt:       run.NextAttemptAt,
		EndedAt:             run.EndedAt,
		RunningStepID:       run.RunningStepID,
		RunningStepAttempt:  clampInt32(run.RunningStepAttempt),
		RunningStepPolicy:   policy,
		WorkerWaitSince:     run.WorkerWaitSince,
		StartedAt:           run.StartedAt,
		FinishedAt:          run.FinishedAt,
		HandlerAttemptsUsed: clampInt32(run.HandlerAttemptsUsed),
		EndedToken:          run.EndedToken,
		EndedKind:           string(run.EndedKind),
	}, nil
}

func stepFromRow(row sqlc.Step) (domain.StepRecord, error) {
	failure, err := failureFromRaw(row.Failure)
	if err != nil {
		return domain.StepRecord{}, err
	}

	return domain.StepRecord{
		StepID:         row.StepID,
		Position:       int(row.Position),
		Status:         domain.StepStatus(row.Status),
		AttemptsUsed:   int(row.AttemptsUsed),
		Output:         row.Output,
		Failure:        failure,
		RetryNotBefore: row.RetryNotBefore,
	}, nil
}

func stepUpsertParams(runID string, step domain.StepRecord) (sqlc.StepUpsertParams, error) {
	failure, err := failureParams(step.Failure)
	if err != nil {
		return sqlc.StepUpsertParams{}, err
	}

	rowID, err := uuid.NewV7()
	if err != nil {
		return sqlc.StepUpsertParams{}, fmt.Errorf("new step id: %w", err)
	}

	return sqlc.StepUpsertParams{
		ID:             rowID.String(),
		RunID:          runID,
		StepID:         step.StepID,
		Position:       clampInt32(step.Position),
		Status:         string(step.Status),
		AttemptsUsed:   clampInt32(step.AttemptsUsed),
		Output:         step.Output,
		Failure:        failure,
		RetryNotBefore: step.RetryNotBefore,
	}, nil
}

// clampInt32 converts a count to the database's INTEGER. A count outside the int32
// range is clamped, so a bad value cannot wrap around.
func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n) //nolint:gosec // n is within the int32 range here.
}

// GiveBack puts a running Run back in the queue and takes its attempt back, under
// the Run's row lock. It applies only while the lease token is the Run's current one.
func (s *RunStore) GiveBack(
	ctx context.Context,
	id string,
	token string,
	reason domain.QueuedReason,
	now time.Time,
) error {
	return s.MutateRun(ctx, id, func(snap domain.Snapshot) (domain.Write, error) {
		return domain.GiveBack(snap, token, reason, now), nil
	})
}

// Ready is the hook for a Run that became ready. The runs table is polled by the
// dispatcher, so nothing is needed here.
func (s *RunStore) Ready(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}
