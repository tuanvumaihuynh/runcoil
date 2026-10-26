package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func utc(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

func cronSchedule(spec string) Schedule {
	return Schedule{Kind: ScheduleKindCron, Cron: spec}
}

func everySchedule(d time.Duration) Schedule {
	return Schedule{Kind: ScheduleKindEvery, Every: d}
}

func dailySchedule(times ...TimeOfDay) Schedule {
	return Schedule{Kind: ScheduleKindDaily, Daily: times}
}

func TestNextTickTable(t *testing.T) {
	tests := []struct {
		name  string
		sched Schedule
		after time.Time
		want  time.Time
		ok    bool
	}{
		{"cron every five minutes", cronSchedule("*/5 * * * *"), utc(2026, 10, 11, 10, 2, 0), utc(2026, 10, 11, 10, 5, 0), true},
		{"cron exactly on a tick moves to the next", cronSchedule("*/5 * * * *"), utc(2026, 10, 11, 10, 5, 0), utc(2026, 10, 11, 10, 10, 0), true},
		{"cron every minute from mid minute", cronSchedule("* * * * *"), utc(2026, 10, 11, 10, 0, 30), utc(2026, 10, 11, 10, 1, 0), true},
		{"cron weekly on Monday at nine", cronSchedule("0 9 * * 1"), utc(2026, 10, 11, 10, 0, 0), utc(2026, 10, 12, 9, 0, 0), true},
		{"cron daily at nine, after nine", cronSchedule("0 9 * * *"), utc(2026, 10, 11, 9, 0, 1), utc(2026, 10, 12, 9, 0, 0), true},
		{"cron with a time zone in UTC input", cronSchedule("30 14 * * *"), time.Date(2026, 10, 11, 21, 0, 0, 0, time.FixedZone("x", 7*3600)), utc(2026, 10, 11, 14, 30, 0), true},
		{"cron month boundary", cronSchedule("0 0 1 * *"), utc(2026, 1, 31, 23, 59, 0), utc(2026, 2, 1, 0, 0, 0), true},
		{"cron year boundary", cronSchedule("0 0 1 1 *"), utc(2026, 12, 31, 12, 0, 0), utc(2027, 1, 1, 0, 0, 0), true},
		{"cron leap day", cronSchedule("0 12 29 2 *"), utc(2027, 3, 1, 0, 0, 0), utc(2028, 2, 29, 12, 0, 0), true},
		{"cron that never matches", cronSchedule("0 0 30 2 *"), utc(2026, 1, 1, 0, 0, 0), time.Time{}, false},
		{"cron that does not parse", cronSchedule("every minute please"), utc(2026, 1, 1, 0, 0, 0), time.Time{}, false},
		{"cron six fields is not standard", cronSchedule("0 */5 * * * *"), utc(2026, 1, 1, 0, 0, 0), time.Time{}, false},

		{"every, between boundaries", everySchedule(2 * time.Second), utc(2026, 10, 11, 10, 0, 1), utc(2026, 10, 11, 10, 0, 2), true},
		{"every, exactly on a boundary", everySchedule(2 * time.Second), utc(2026, 10, 11, 10, 0, 2), utc(2026, 10, 11, 10, 0, 4), true},
		{"every, fraction after a boundary", everySchedule(2 * time.Second), utc(2026, 10, 11, 10, 0, 1), utc(2026, 10, 11, 10, 0, 2), true},
		{"every, aligned to the epoch not to the start", everySchedule(10 * time.Second), utc(2026, 10, 11, 10, 0, 7), utc(2026, 10, 11, 10, 0, 10), true},
		{"every, one hour", everySchedule(time.Hour), utc(2026, 10, 11, 10, 59, 59), utc(2026, 10, 11, 11, 0, 0), true},
		{"every, after the epoch's own boundary", everySchedule(time.Second), time.Unix(1, 500_000_000).UTC(), time.Unix(2, 0).UTC(), true},
		{"every, before the epoch", everySchedule(2 * time.Second), time.Unix(-1, 0).UTC(), time.Unix(0, 0).UTC(), true},
		{"every, below one second has no tick", everySchedule(500 * time.Millisecond), utc(2026, 1, 1, 0, 0, 0), time.Time{}, false},

		{"daily, several times in one day", dailySchedule(TimeOfDay{Hour: 17, Minute: 0}, TimeOfDay{Hour: 9, Minute: 30}),
			utc(2026, 10, 11, 8, 0, 0), utc(2026, 10, 11, 9, 30, 0), true},
		{"daily, between two times", dailySchedule(TimeOfDay{Hour: 17}, TimeOfDay{Hour: 9, Minute: 30}),
			utc(2026, 10, 11, 10, 0, 0), utc(2026, 10, 11, 17, 0, 0), true},
		{"daily, after the last time rolls to the next day", dailySchedule(TimeOfDay{Hour: 17}, TimeOfDay{Hour: 9, Minute: 30}),
			utc(2026, 10, 11, 17, 0, 0), utc(2026, 10, 12, 9, 30, 0), true},
		{"daily, late evening rolls over midnight", dailySchedule(TimeOfDay{Hour: 6}), utc(2026, 10, 11, 23, 30, 0),
			utc(2026, 10, 12, 6, 0, 0), true},
		{"daily, midnight time after 23:59 goes to the next day", dailySchedule(TimeOfDay{Hour: 0, Minute: 0}),
			utc(2026, 10, 11, 23, 59, 0), utc(2026, 10, 12, 0, 0, 0), true},
		{"daily, month boundary", dailySchedule(TimeOfDay{Hour: 1}), utc(2026, 1, 31, 2, 0, 0), utc(2026, 2, 1, 1, 0, 0), true},
		{"daily, year boundary", dailySchedule(TimeOfDay{Hour: 0, Minute: 1}), utc(2026, 12, 31, 23, 59, 59),
			utc(2027, 1, 1, 0, 1, 0), true},
		{"daily, no times", dailySchedule(), utc(2026, 1, 1, 0, 0, 0), time.Time{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NextTick(tt.sched, tt.after)
			require.Equal(t, tt.ok, ok, "tick %v", got)
			if ok {
				require.True(t, tt.want.Equal(got), "NextTick = %v, want %v", got, tt.want)
				require.Equal(t, time.UTC, got.Location(), "tick must be in UTC")
			}
		})
	}
}

// Ticks are strictly increasing: each one comes after the previous one.
func TestNextTickAdvances(t *testing.T) {
	for _, sched := range []Schedule{
		cronSchedule("*/7 * * * *"),
		everySchedule(3 * time.Second),
		dailySchedule(TimeOfDay{Hour: 23, Minute: 59}, TimeOfDay{Hour: 0}),
	} {
		at := utc(2026, 12, 31, 22, 0, 0)
		for range 200 {
			next, ok := NextTick(sched, at)
			require.True(t, ok, "%s: no tick after %v", sched.Kind, at)
			require.True(t, next.After(at), "%s: tick %v is not after the previous tick %v", sched.Kind, next, at)

			at = next
		}
	}
}

func TestSkipTick(t *testing.T) {
	allow := Schedule{Kind: ScheduleKindEvery, Every: time.Second}
	skip := allow
	skip.SkipOverlap = true

	assert.False(t, SkipTick(allow, true), "allow mode must not skip a tick while a Run is unfinished")
	assert.False(t, SkipTick(skip, false), "skip mode must not skip a tick when nothing is unfinished")
	assert.True(t, SkipTick(skip, true), "skip mode must skip a tick while a schedule Run is unfinished")
}

func TestParseCronRejectsTimeZonePrefixes(t *testing.T) {
	for _, spec := range []string{"TZ=Europe/Paris 0 9 * * *", "CRON_TZ=UTC 0 9 * * *", " TZ=UTC * * * * *"} {
		_, err := ParseCron(spec)
		assert.Error(t, err, "ParseCron(%q) accepted a time zone prefix", spec)

		_, ok := NextTick(cronSchedule(spec), utc(2026, 1, 1, 0, 0, 0))
		assert.False(t, ok, "NextTick for %q must report no tick", spec)
	}

	_, err := ParseCron("0 9 * * *")
	assert.NoError(t, err, "a plain cron rejected")
}

// at is 10:00 UTC plus the given seconds.
func at(sec int) time.Time {
	return time.Date(2026, 10, 11, 10, 0, sec, 0, time.UTC)
}

func everyEntry(d time.Duration, skip bool, next time.Time) *ScheduleEntry {
	return &ScheduleEntry{
		WorkerID:  "demo",
		TaskID:    "t",
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: d, SkipOverlap: skip},
		NextRunAt: &next,
	}
}

func TestDecideTickDropsWhenTheStoredTickDiffers(t *testing.T) {
	stored := everyEntry(2*time.Second, false, at(4))
	got := DecideTick(stored, at(2), false, at(2))
	require.Equal(t, TickDrop, got.Action, "a tick the schedule no longer has")

	got = DecideTick(nil, at(4), false, at(4))
	require.Equal(t, TickDrop, got.Action, "a removed schedule")

	got = DecideTick(everyEntry(2*time.Second, false, time.Time{}), at(4), false, at(4))
	require.Equal(t, TickDrop, got.Action, "a schedule with no tick")
}

func TestDecideTickCreatesAndAdvancesFromTheLaterOfNowAndTick(t *testing.T) {
	stored := everyEntry(2*time.Second, false, at(4))
	got := DecideTick(stored, at(4), false, at(4))
	require.Equal(t, TickCreate, got.Action, "on time")
	require.True(t, got.Next != nil && at(6).Equal(*got.Next), "on time: next = %v, want 10:00:06", got.Next)

	// A tick handled late: the next tick is after now, not after the old tick.
	got = DecideTick(stored, at(4), false, at(9))
	require.Equal(t, TickCreate, got.Action, "handled at 9s")
	require.True(t, got.Next != nil && at(10).Equal(*got.Next), "handled at 9s: next = %v, want 10:00:10", got.Next)
}

func TestDecideTickSkipsOnlyWithSkipOverlapAndAnUnfinishedRun(t *testing.T) {
	stored := everyEntry(2*time.Second, true, at(4))
	got := DecideTick(stored, at(4), true, at(4))
	require.Equal(t, TickSkip, got.Action, "skip overlap with an unfinished run")
	require.True(t, got.Next != nil && at(6).Equal(*got.Next), "skip overlap with an unfinished run: next = %v, want 10:00:06", got.Next)

	got = DecideTick(stored, at(4), false, at(4))
	require.Equal(t, TickCreate, got.Action, "skip overlap with nothing unfinished")

	plain := everyEntry(2*time.Second, false, at(4))
	got = DecideTick(plain, at(4), true, at(4))
	require.Equal(t, TickCreate, got.Action, "no skip overlap ignores an unfinished run")
}

func TestPlanSchedulesKeepsUnchangedTicksAndStartsChangedOnes(t *testing.T) {
	stored := []ScheduleEntry{
		{WorkerID: "demo", TaskID: "same", Schedule: Schedule{Kind: ScheduleKindEvery, Every: 2 * time.Second}, NextRunAt: atPtr(at(4))},
		{WorkerID: "demo", TaskID: "changed", Schedule: Schedule{Kind: ScheduleKindEvery, Every: 2 * time.Second}, NextRunAt: atPtr(at(4))},
		{WorkerID: "demo", TaskID: "removed", Schedule: Schedule{Kind: ScheduleKindEvery, Every: 2 * time.Second}, NextRunAt: atPtr(at(4))},
	}

	same := Schedule{Kind: ScheduleKindEvery, Every: 2 * time.Second}
	changed := Schedule{Kind: ScheduleKindEvery, Every: 5 * time.Second}
	tasks := []Task{
		{WorkerID: "demo", TaskID: "same", Schedule: &same},
		{WorkerID: "demo", TaskID: "changed", Schedule: &changed},
		{WorkerID: "demo", TaskID: "removed"},
		{WorkerID: "demo", TaskID: "new", Schedule: &same},
	}

	plan := PlanSchedules(stored, tasks, at(1))
	got := map[string]*time.Time{}
	for _, e := range plan {
		got[e.TaskID] = e.NextRunAt
	}

	require.Len(t, plan, 3, "removed has no schedule")
	require.True(t, got["same"] != nil && at(4).Equal(*got["same"]), "unchanged schedule tick = %v, want 10:00:04", got["same"])
	require.True(t, got["changed"] != nil && at(5).Equal(*got["changed"]), "changed schedule tick = %v, want 10:00:05", got["changed"])
	require.True(t, got["new"] != nil && at(2).Equal(*got["new"]), "new schedule tick = %v, want 10:00:02", got["new"])

	_, ok := got["removed"]
	require.False(t, ok, "a Task without a schedule keeps none")
}

func atPtr(t time.Time) *time.Time { return &t }
