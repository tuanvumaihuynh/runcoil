package domain

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser reads the standard five-field cron expression. Descriptors such as
// @daily are not accepted.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ScheduleTimer is a schedule whose next tick is near: the Task and the tick.
type ScheduleTimer struct {
	WorkerID string
	TaskID   string
	Tick     time.Time
}

// ScheduleEntry is a stored schedule: its Task, its rule, and its next tick. A zero
// NextRunAt means the schedule has no further tick.
type ScheduleEntry struct {
	WorkerID  string
	TaskID    string
	Schedule  Schedule
	NextRunAt time.Time
}

// TickAction is what the scheduler does with one loaded tick.
type TickAction int

const (
	// TickDrop means the stored schedule no longer has this tick, so nothing is created.
	TickDrop TickAction = iota + 1
	// TickSkip means the schedule skips overlap and an unfinished Run of it exists, so
	// no Run is created for this tick.
	TickSkip
	// TickCreate means the tick's Run is created.
	TickCreate
)

// TickDecision is what one tick does and the schedule's next tick after it.
type TickDecision struct {
	Action TickAction
	// Next is the first tick after max(now, tick). Zero when the schedule has none.
	Next time.Time
}

// ParseCron checks a five-field cron expression and returns its schedule.
func ParseCron(spec string) (cron.Schedule, error) {
	// The parser accepts a time zone prefix. Schedules are read in UTC only.
	trimmed := strings.TrimSpace(spec)
	if strings.HasPrefix(trimmed, "TZ=") || strings.HasPrefix(trimmed, "CRON_TZ=") {
		return nil, errors.New("cron expression must not name a time zone: schedules are read in UTC")
	}

	return cronParser.Parse(trimmed)
}

// NextTick returns the first tick of the schedule that is after the given time,
// in UTC. It reports false when the schedule has no further tick, which only a
// cron expression that never matches can have.
//
// Ticks are the five-field cron times, every multiple of an interval counted from
// the Unix epoch, or each daily time in UTC.
func NextTick(s Schedule, after time.Time) (time.Time, bool) {
	after = after.UTC()
	switch s.Kind {
	case ScheduleKindCron:
		parsed, err := ParseCron(s.Cron)
		if err != nil {
			return time.Time{}, false
		}

		next := parsed.Next(after)
		if next.IsZero() {
			return time.Time{}, false
		}

		return next.UTC(), true
	case ScheduleKindEvery:
		if s.Every < time.Second {
			return time.Time{}, false
		}

		interval := int64(s.Every)
		// The multiple after after: floor(after / interval) + 1, with floor for times before the epoch.
		n := after.UnixNano()
		q := n / interval
		if n%interval < 0 {
			q--
		}

		return time.Unix(0, (q+1)*interval).UTC(), true
	case ScheduleKindDaily:
		if len(s.Daily) == 0 {
			return time.Time{}, false
		}

		var best time.Time
		// The next daily time is today or tomorrow, so two days are enough.
		for dayOffset := 0; dayOffset <= 1; dayOffset++ {
			day := time.Date(after.Year(), after.Month(), after.Day()+dayOffset, 0, 0, 0, 0, time.UTC)
			for _, at := range s.Daily {
				candidate := day.Add(time.Duration(at.Hour)*time.Hour + time.Duration(at.Minute)*time.Minute)
				if candidate.After(after) && (best.IsZero() || candidate.Before(best)) {
					best = candidate
				}
			}
		}

		return best, !best.IsZero()
	}

	return time.Time{}, false
}

// SkipTick reports whether a tick creates no Run. That happens when the schedule
// skips overlap and the Task still has an unfinished Run from its schedule. A
// queued Run counts as unfinished.
func SkipTick(s Schedule, unfinishedScheduled bool) bool {
	return s.SkipOverlap && unfinishedScheduled
}

// DecideTick decides what a loaded tick does, from the schedule as it is stored now.
// entry is nil when the schedule is gone. unfinished reports whether the Task has an
// unfinished Run from its schedule; the caller asks only when the schedule skips overlap.
// The next tick is the first one after the later of now and tick, so a tick handled
// late does not produce a tick in the past.
func DecideTick(entry *ScheduleEntry, tick time.Time, unfinished bool, now time.Time) TickDecision {
	if entry == nil || entry.NextRunAt.IsZero() || !entry.NextRunAt.Equal(tick) {
		return TickDecision{Action: TickDrop}
	}

	action := TickCreate
	if SkipTick(entry.Schedule, unfinished) {
		action = TickSkip
	}

	from := now
	if tick.After(from) {
		from = tick
	}

	next, ok := NextTick(entry.Schedule, from)
	if !ok {
		next = time.Time{}
	}

	return TickDecision{Action: action, Next: next}
}

// SameSchedule reports whether two schedules have the same timing and the same overlap rule.
func SameSchedule(a, b Schedule) bool {
	return a.Kind == b.Kind && a.Cron == b.Cron && a.Every == b.Every &&
		a.SkipOverlap == b.SkipOverlap && slices.Equal(a.Daily, b.Daily)
}

// PlanSchedules returns the schedules a registration leaves in place of the stored
// ones, for the Tasks that declare one. A schedule whose rule is unchanged keeps its
// stored next tick. A new or changed schedule starts at its first tick after now.
// Tasks without a schedule have none in the result, so their stored schedules go.
func PlanSchedules(stored []ScheduleEntry, tasks []Task, now time.Time) []ScheduleEntry {
	byTask := make(map[string]ScheduleEntry, len(stored))
	for _, entry := range stored {
		byTask[entry.TaskID] = entry
	}

	var out []ScheduleEntry
	for _, task := range tasks {
		if task.Schedule == nil {
			continue
		}

		entry := ScheduleEntry{WorkerID: task.WorkerID, TaskID: task.TaskID, Schedule: *task.Schedule}
		if prev, ok := byTask[task.TaskID]; ok && SameSchedule(prev.Schedule, *task.Schedule) {
			entry.NextRunAt = prev.NextRunAt
		} else if next, ok := NextTick(*task.Schedule, now); ok {
			entry.NextRunAt = next
		}

		out = append(out, entry)
	}

	return out
}
