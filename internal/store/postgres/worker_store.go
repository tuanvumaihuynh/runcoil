package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.WorkerStore = (*Store)(nil)

// Register stores the Tasks of the registration and the worker session, in one
// transaction. It returns domain.ErrAPIKeyNotFound when no API key has keyHash.
func (s *Store) Register(ctx context.Context, keyHash []byte, session domain.WorkerSession) (domain.Worker, error) {
	var worker domain.Worker
	err := s.db.WithTx(ctx, func(tx DB) error {
		key, err := s.queries.ApiKeyGetForRegister(ctx, tx, keyHash)
		if isNoRowsError(err) {
			return domain.ErrAPIKeyNotFound
		}

		if err != nil {
			return fmt.Errorf("get api key: %w", err)
		}

		reg := session.Registration
		taskIDs := make([]uuid.UUID, 0, len(reg.Tasks))
		for _, i := range byTaskName(reg.Tasks) {
			def := reg.Tasks[i]
			params, err := taskUpsertParams(session.TaskIDs[i], key.ProjectID, reg.App, def)
			if err != nil {
				return fmt.Errorf("task %q: %w", def.Name, err)
			}

			id, err := s.queries.TaskUpsert(ctx, tx, params)
			if err != nil {
				return fmt.Errorf("upsert task %q: %w", def.Name, err)
			}

			if err := s.syncSchedule(ctx, tx, key.ProjectID, id, def.Schedule); err != nil {
				return fmt.Errorf("task %q: %w", def.Name, err)
			}

			taskIDs = append(taskIDs, id)
		}

		err = s.queries.WorkerCreate(ctx, tx, gen.WorkerCreateParams{
			ID:        session.ID,
			ProjectID: key.ProjectID,
			ApiKeyID:  key.ID,
			TokenHash: session.TokenHash,
			App:       reg.App,
			TaskIds:   taskIDs,
			Slots:     int32(reg.Slots), //nolint:gosec // Registration.Validate caps the slots at domain.MaxSlots.
		})
		if err != nil {
			return fmt.Errorf("create worker: %w", err)
		}

		worker = domain.Worker{
			ID:        session.ID,
			ProjectID: key.ProjectID,
			App:       reg.App,
			TaskIDs:   taskIDs,
			Slots:     reg.Slots,
		}

		return nil
	})
	if err != nil {
		return domain.Worker{}, fmt.Errorf("register in transaction: %w", err)
	}

	return worker, nil
}

// syncSchedule stores the Task's schedule, or removes the one it had when it declares
// none. A new or changed cron starts from its first tick after now.
func (s *Store) syncSchedule(ctx context.Context, tx DB, projectID, taskID uuid.UUID, schedule *domain.Schedule) error {
	if schedule == nil {
		if err := s.queries.ScheduleDelete(ctx, tx, taskID); err != nil {
			return fmt.Errorf("delete schedule: %w", err)
		}

		return nil
	}

	c, err := domain.ParseCron(schedule.Cron)
	if err != nil {
		return fmt.Errorf("parse schedule: %w", err)
	}

	now, err := s.queries.ScheduleNow(ctx, tx)
	if err != nil {
		return fmt.Errorf("read database clock: %w", err)
	}

	err = s.queries.ScheduleUpsert(ctx, tx, gen.ScheduleUpsertParams{
		ID:            uuid.NewV7(),
		ProjectID:     projectID,
		TaskID:        taskID,
		Cron:          c.String(),
		OverlapPolicy: string(schedule.Overlap),
		NextTickAt:    c.Next(now),
	})
	if err != nil {
		return fmt.Errorf("upsert schedule: %w", err)
	}

	return nil
}

// byTaskName returns the indexes of tasks in name order. Every Register locks the Task
// rows in this order, so two Registers of the same Tasks cannot deadlock.
func byTaskName(tasks []domain.TaskDefinition) []int {
	order := make([]int, len(tasks))
	for i := range order {
		order[i] = i
	}

	slices.SortFunc(order, func(a, b int) int { return strings.Compare(tasks[a].Name, tasks[b].Name) })

	return order
}

// GetWorkerBySessionToken returns the live session whose token has tokenHash, or
// domain.ErrSessionEnded.
func (s *Store) GetWorkerBySessionToken(ctx context.Context, tokenHash []byte) (domain.Worker, error) {
	row, err := s.queries.WorkerGetBySessionToken(ctx, s.db, tokenHash)
	if isNoRowsError(err) {
		return domain.Worker{}, domain.ErrSessionEnded
	}

	if err != nil {
		return domain.Worker{}, fmt.Errorf("get worker: %w", err)
	}

	return workerFromRow(row), nil
}

// AttachStream moves the worker to a new stream and reconciles its leases, in one
// transaction: the leases it does not report count as lost and go back to the queue, or
// fail their Run above params.MaxLost, and the reported Runs it no longer holds are
// returned. It returns domain.ErrSessionEnded when the session has ended.
func (s *Store) AttachStream(ctx context.Context, params domain.AttachStreamParams) (domain.AttachStreamResult, error) {
	runIDs := make([]uuid.UUID, 0, len(params.Running))
	epochs := make([]int64, 0, len(params.Running))
	for _, l := range params.Running {
		runIDs = append(runIDs, l.RunID)
		epochs = append(epochs, l.Epoch)
	}

	var result domain.AttachStreamResult
	err := s.db.WithTx(ctx, func(tx DB) error {
		_, err := s.queries.WorkerAttachStream(ctx, tx, gen.WorkerAttachStreamParams{
			ID:           params.WorkerID,
			DispatcherID: params.DispatcherID,
			StreamID:     params.StreamID,
		})
		if isNoRowsError(err) {
			return domain.ErrSessionEnded
		}

		if err != nil {
			return fmt.Errorf("attach stream: %w", err)
		}

		released, err := s.queries.LeaseReleaseUnknown(ctx, tx, gen.LeaseReleaseUnknownParams{
			WorkerID:   params.WorkerID,
			HeldEpochs: epochs,
			MaxLost:    int32(params.MaxLost), //nolint:gosec // Validated against a small cap.
		})
		if err != nil {
			return fmt.Errorf("release unknown leases: %w", err)
		}

		for _, row := range released {
			if row.Failed {
				result.Failed++
			} else {
				result.Requeued++
			}
		}

		rows, err := s.queries.LeaseListNotHeld(ctx, tx, gen.LeaseListNotHeldParams{
			WorkerID:    params.WorkerID,
			RunIds:      runIDs,
			LeaseEpochs: epochs,
		})
		if err != nil {
			return fmt.Errorf("list leases not held: %w", err)
		}

		result.NotHeld = make([]domain.Lease, 0, len(rows))
		for _, row := range rows {
			result.NotHeld = append(result.NotHeld, domain.Lease{RunID: row.RunID, Epoch: row.LeaseEpoch})
		}

		params.Attached(result.NotHeld)

		return nil
	})
	if err != nil {
		return domain.AttachStreamResult{}, fmt.Errorf("attach stream in transaction: %w", err)
	}

	return result, nil
}

// FlushHeartbeats writes the last heartbeat of each worker. A time older than the stored
// one is ignored.
func (s *Store) FlushHeartbeats(ctx context.Context, ids []uuid.UUID, beats []time.Time) error {
	if err := s.queries.WorkerFlushHeartbeats(ctx, s.db, gen.WorkerFlushHeartbeatsParams{Ids: ids, Beats: beats}); err != nil {
		return fmt.Errorf("flush heartbeats: %w", err)
	}

	return nil
}

// BeatDispatcher records that the dispatcher is alive.
func (s *Store) BeatDispatcher(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.DispatcherBeat(ctx, s.db, id); err != nil {
		return fmt.Errorf("beat dispatcher: %w", err)
	}

	return nil
}

// ClaimRuns leases queued Runs to the worker in one transaction, then reads what their
// attempts need. It returns domain.ErrStreamFenced when the worker is no longer on the
// stream.
func (s *Store) ClaimRuns(ctx context.Context, params domain.ClaimRunsParams) ([]domain.Assign, error) {
	var leases []gen.LeaseClaimRow
	err := s.db.WithTx(ctx, func(tx DB) error {
		_, err := s.queries.WorkerLockForClaim(ctx, tx, gen.WorkerLockForClaimParams{
			ID:           params.WorkerID,
			DispatcherID: params.DispatcherID,
			StreamID:     params.StreamID,
		})
		if isNoRowsError(err) {
			return domain.ErrStreamFenced
		}

		if err != nil {
			return fmt.Errorf("lock worker: %w", err)
		}

		leases, err = s.queries.LeaseClaim(ctx, tx, gen.LeaseClaimParams{
			WorkerID: params.WorkerID,
			TaskIds:  params.TaskIDs,
			Slots:    int32(params.Slots), //nolint:gosec // Slots come from the worker row, an integer.
		})
		if err != nil {
			return fmt.Errorf("claim runs: %w", err)
		}

		if len(leases) == 0 {
			return nil
		}

		if err := s.queries.RunMarkStarted(ctx, tx, leaseRunIDs(leases)); err != nil {
			return fmt.Errorf("mark runs started: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim in transaction: %w", err)
	}

	if len(leases) == 0 {
		return nil, nil
	}

	assigns, err := s.listAssigns(ctx, leases)
	if err != nil {
		return nil, fmt.Errorf("read assigns: %w", err)
	}

	return assigns, nil
}

func leaseRunIDs(leases []gen.LeaseClaimRow) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(leases))
	for _, l := range leases {
		ids = append(ids, l.RunID)
	}

	return ids
}

// listAssigns reads the input and the journal of the claimed Runs, outside the claim.
func (s *Store) listAssigns(ctx context.Context, leases []gen.LeaseClaimRow) ([]domain.Assign, error) {
	ids := leaseRunIDs(leases)

	runs, err := s.queries.RunListForAssign(ctx, s.db, ids)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	steps, err := s.queries.StepListForRuns(ctx, s.db, ids)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}

	journal := make(map[uuid.UUID][]domain.StepRecord, len(runs))
	for _, st := range steps {
		journal[st.RunID] = append(journal[st.RunID], stepRecord(stepRecordParams{
			StepID:       st.StepID,
			Kind:         st.Kind,
			Status:       st.Status,
			Output:       st.Output,
			ErrorCode:    st.ErrorCode,
			ErrorMessage: st.ErrorMessage,
		}))
	}

	epochs := make(map[uuid.UUID]int64, len(leases))
	for _, l := range leases {
		epochs[l.RunID] = l.LeaseEpoch
	}

	assigns := make([]domain.Assign, 0, len(runs))
	for _, r := range runs {
		assigns = append(assigns, assignFromRow(r, epochs[r.ID], journal[r.ID]))
	}

	return assigns, nil
}

// Deregister ends the worker's session and puts the attempts it still holds back in the
// queue, in one transaction. A drain is not the worker's fault, so they do not count as
// lost. It returns how many it put back, and domain.ErrSessionEnded when the session has
// already ended.
func (s *Store) Deregister(ctx context.Context, workerID uuid.UUID) (int, error) {
	var released int
	err := s.db.WithTx(ctx, func(tx DB) error {
		_, err := s.queries.WorkerDeregister(ctx, tx, workerID)
		if isNoRowsError(err) {
			return domain.ErrSessionEnded
		}

		if err != nil {
			return fmt.Errorf("end session: %w", err)
		}

		ids, err := s.queries.LeaseReleaseOfWorker(ctx, tx, workerID)
		if err != nil {
			return fmt.Errorf("release leases: %w", err)
		}

		released = len(ids)

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("deregister in transaction: %w", err)
	}

	return released, nil
}

// EndDeadWorkers ends at most batchSize sessions with no heartbeat for longer than ttl,
// and puts the attempts they held back in the queue, in one transaction. Each attempt
// counts as lost; a Run lost more than maxLost times fails with attempt_lost instead.
func (s *Store) EndDeadWorkers(ctx context.Context, ttl time.Duration, maxLost, batchSize int) (domain.DeadWorkers, error) {
	var dead domain.DeadWorkers
	err := s.db.WithTx(ctx, func(tx DB) error {
		ids, err := s.queries.WorkerEndDead(ctx, tx, gen.WorkerEndDeadParams{
			Ttl:       interval(&ttl),
			BatchSize: int32(batchSize), //nolint:gosec // A small batch size.
		})
		if err != nil {
			return fmt.Errorf("end dead sessions: %w", err)
		}

		if len(ids) == 0 {
			return nil
		}

		rows, err := s.queries.LeaseRequeueLost(ctx, tx, gen.LeaseRequeueLostParams{
			WorkerIds: ids,
			MaxLost:   int32(maxLost), //nolint:gosec // Validated against a small cap.
		})
		if err != nil {
			return fmt.Errorf("requeue lost leases: %w", err)
		}

		dead.Ended = len(ids)
		for _, row := range rows {
			if row.Failed {
				dead.Failed++
			} else {
				dead.Requeued++
			}
		}

		return nil
	})
	if err != nil {
		return domain.DeadWorkers{}, fmt.Errorf("end dead workers in transaction: %w", err)
	}

	return dead, nil
}
