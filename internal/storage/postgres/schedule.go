package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres/sqlc"
)

// ScheduleStore keeps the schedules with their next ticks. It implements the
// engine's schedule store. It holds no decisions: the engine decides, and each
// method performs one read or one write.
type ScheduleStore struct {
	db DB
	q  *sqlc.Queries
}

// NewScheduleStore creates a ScheduleStore on db.
func NewScheduleStore(db DB) *ScheduleStore {
	return &ScheduleStore{db: db, q: sqlc.New()}
}

// specJSON is how a schedule is stored in the spec column. The overlap rule is a column.
type specJSON struct {
	Kind    string          `json:"kind"`
	Cron    string          `json:"cron,omitempty"`
	EveryNs int64           `json:"every_ns,omitempty"`
	Daily   []timeOfDayJSON `json:"daily,omitempty"`
}

type timeOfDayJSON struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

func specToJSON(s domain.Schedule) specJSON {
	out := specJSON{Kind: string(s.Kind), Cron: s.Cron, EveryNs: int64(s.Every)}
	for _, t := range s.Daily {
		out.Daily = append(out.Daily, timeOfDayJSON{Hour: t.Hour, Minute: t.Minute})
	}

	return out
}

// scheduleFromSpec reads a stored schedule, with its overlap rule from the column.
func scheduleFromSpec(raw json.RawMessage, skipOverlap bool) (domain.Schedule, error) {
	var sj specJSON
	if err := json.Unmarshal(raw, &sj); err != nil {
		return domain.Schedule{}, fmt.Errorf("unmarshal schedule: %w", err)
	}

	out := domain.Schedule{
		Kind:        domain.ScheduleKind(sj.Kind),
		Cron:        sj.Cron,
		Every:       time.Duration(sj.EveryNs),
		SkipOverlap: skipOverlap,
	}

	for _, t := range sj.Daily {
		out.Daily = append(out.Daily, domain.TimeOfDay{Hour: t.Hour, Minute: t.Minute})
	}

	return out, nil
}

// LoadDue returns up to limit schedules whose next tick is at or before until, earliest first.
func (s *ScheduleStore) LoadDue(ctx context.Context, until time.Time, limit int) ([]domain.ScheduleTimer, error) {
	rows, err := s.q.ScheduleLoadDue(ctx, s.db, sqlc.ScheduleLoadDueParams{
		Until:    &until,
		RowLimit: clampInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("load due schedules: %w", err)
	}

	timers := make([]domain.ScheduleTimer, 0, len(rows))
	for _, row := range rows {
		if row.NextRunAt == nil {
			continue
		}

		timers = append(timers, domain.ScheduleTimer{
			Ref:  domain.TaskRef{WorkerID: row.WorkerID, TaskID: row.TaskID},
			Tick: *row.NextRunAt,
		})
	}

	return timers, nil
}

// Get returns the stored schedule of the Task. The bool is false when it has none.
func (s *ScheduleStore) Get(ctx context.Context, ref domain.TaskRef) (domain.ScheduleEntry, bool, error) {
	row, err := s.q.ScheduleGet(ctx, s.db, sqlc.ScheduleGetParams{WorkerID: ref.WorkerID, TaskID: ref.TaskID})
	if isNoRowsError(err) {
		return domain.ScheduleEntry{}, false, nil
	}

	if err != nil {
		return domain.ScheduleEntry{}, false, fmt.Errorf("get schedule: %w", err)
	}

	entry, err := entryFromRow(row.WorkerID, row.TaskID, row.Spec, row.SkipOverlap, row.NextRunAt)
	if err != nil {
		return domain.ScheduleEntry{}, false, err
	}

	return entry, true, nil
}

// Advance sets the next tick of the Task's schedule, only while its stored tick is
// tick. A zero next means the schedule has no further tick. It reports whether the
// stored tick was tick, so the write took effect.
func (s *ScheduleStore) Advance(ctx context.Context, ref domain.TaskRef, tick, next time.Time) (bool, error) {
	var nextPtr *time.Time
	if !next.IsZero() {
		nextPtr = &next
	}

	n, err := s.q.ScheduleAdvance(ctx, s.db, sqlc.ScheduleAdvanceParams{
		NextRunAt: nextPtr,
		WorkerID:  ref.WorkerID,
		TaskID:    ref.TaskID,
		Tick:      &tick,
	})
	if err != nil {
		return false, fmt.Errorf("advance schedule: %w", err)
	}

	return n > 0, nil
}

// ListMissed returns up to limit schedules whose next tick is before now, after the
// given Task in the order of (worker_id, task_id). Each call is bounded by limit.
func (s *ScheduleStore) ListMissed(
	ctx context.Context,
	now time.Time,
	after domain.TaskRef,
	limit int,
) ([]domain.ScheduleEntry, error) {
	rows, err := s.q.ScheduleListMissed(ctx, s.db, sqlc.ScheduleListMissedParams{
		Now:           &now,
		AfterWorkerID: after.WorkerID,
		AfterTaskID:   after.TaskID,
		RowLimit:      clampInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list missed schedules: %w", err)
	}

	out := make([]domain.ScheduleEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := entryFromRow(row.WorkerID, row.TaskID, row.Spec, row.SkipOverlap, row.NextRunAt)
		if err != nil {
			return nil, err
		}

		out = append(out, entry)
	}

	return out, nil
}

// List returns the schedules of a worker ID, ordered by task ID.
func (s *ScheduleStore) List(ctx context.Context, workerID string) ([]domain.ScheduleEntry, error) {
	rows, err := s.q.ScheduleListByWorkerID(ctx, s.db, workerID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	out := make([]domain.ScheduleEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := entryFromRow(row.WorkerID, row.TaskID, row.Spec, row.SkipOverlap, row.NextRunAt)
		if err != nil {
			return nil, err
		}

		out = append(out, entry)
	}

	return out, nil
}

// Replace stores the given schedules of a worker ID, and removes that worker ID's other
// schedules, in one transaction. A schedule whose rule is unchanged keeps the next
// tick the row holds, so a registration does not move a schedule that did not change.
func (s *ScheduleStore) Replace(ctx context.Context, workerID string, entries []domain.ScheduleEntry) error {
	return s.db.WithTx(ctx, func(tx DB) error {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			spec, err := json.Marshal(specToJSON(entry.Schedule))
			if err != nil {
				return fmt.Errorf("marshal schedule of %s: %w", entry.Ref.TaskID, err)
			}

			var next *time.Time
			if !entry.NextRunAt.IsZero() {
				at := entry.NextRunAt
				next = &at
			}

			rowID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("new schedule id of %s: %w", entry.Ref.TaskID, err)
			}

			if err := s.q.ScheduleUpsert(ctx, tx, sqlc.ScheduleUpsertParams{
				ID:          rowID.String(),
				WorkerID:    entry.Ref.WorkerID,
				TaskID:      entry.Ref.TaskID,
				Spec:        spec,
				SkipOverlap: entry.Schedule.SkipOverlap,
				NextRunAt:   next,
			}); err != nil {
				return fmt.Errorf("upsert schedule of %s: %w", entry.Ref.TaskID, err)
			}

			names = append(names, entry.Ref.TaskID)
		}

		if err := s.q.ScheduleDeleteExcept(ctx, tx, sqlc.ScheduleDeleteExceptParams{
			WorkerID: workerID,
			TaskIds:  names,
		}); err != nil {
			return fmt.Errorf("remove schedules: %w", err)
		}

		return nil
	})
}

func entryFromRow(workerID, taskID string, spec json.RawMessage, skipOverlap bool, next *time.Time) (domain.ScheduleEntry, error) {
	schedule, err := scheduleFromSpec(spec, skipOverlap)
	if err != nil {
		return domain.ScheduleEntry{}, err
	}

	entry := domain.ScheduleEntry{
		Ref:      domain.TaskRef{WorkerID: workerID, TaskID: taskID},
		Schedule: schedule,
	}

	if next != nil {
		entry.NextRunAt = *next
	}

	return entry, nil
}
