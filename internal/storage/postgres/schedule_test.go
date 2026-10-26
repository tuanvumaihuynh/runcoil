package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

func TestScheduleIsStoredWithTheseFields(t *testing.T) {
	tests := []struct {
		name     string
		schedule domain.Schedule
		want     string
	}{
		{
			name:     "a cron schedule keeps its expression and leaves the other fields out",
			schedule: domain.Schedule{Kind: domain.ScheduleKindCron, Cron: "0 9 * * 1", SkipOverlap: true},
			want:     `{"kind":"cron","cron":"0 9 * * 1"}`,
		},
		{
			name:     "an every schedule stores its interval in nanoseconds",
			schedule: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: 90 * time.Second},
			want:     `{"kind":"every","every_ns":90000000000}`,
		},
		{
			name: "a daily schedule stores hour and minute pairs in order",
			schedule: domain.Schedule{
				Kind:  domain.ScheduleKindDaily,
				Daily: []domain.TimeOfDay{{Hour: 9, Minute: 30}, {Hour: 17, Minute: 0}},
			},
			want: `{"kind":"daily","daily":[{"hour":9,"minute":30},{"hour":17,"minute":0}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(specToJSON(tt.schedule))
			require.NoError(t, err)

			assert.JSONEq(t, tt.want, string(raw))
		})
	}
}

func TestScheduleReadsStoredSpec(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		skipOverlap bool
		want        domain.Schedule
	}{
		{
			name:        "a cron spec",
			spec:        `{"kind":"cron","cron":"0 9 * * 1"}`,
			skipOverlap: true,
			want:        domain.Schedule{Kind: domain.ScheduleKindCron, Cron: "0 9 * * 1", SkipOverlap: true},
		},
		{
			name:        "an every spec reads nanoseconds",
			spec:        `{"kind":"every","every_ns":90000000000}`,
			skipOverlap: false,
			want:        domain.Schedule{Kind: domain.ScheduleKindEvery, Every: 90 * time.Second},
		},
		{
			name:        "a daily spec reads its times in order",
			spec:        `{"kind":"daily","daily":[{"hour":9,"minute":30},{"hour":17,"minute":0}]}`,
			skipOverlap: false,
			want: domain.Schedule{
				Kind:  domain.ScheduleKindDaily,
				Daily: []domain.TimeOfDay{{Hour: 9, Minute: 30}, {Hour: 17, Minute: 0}},
			},
		},
		{
			name:        "the overlap rule comes from the column, not from the spec",
			spec:        `{"kind":"every","every_ns":60000000000,"skip_overlap":true}`,
			skipOverlap: false,
			want:        domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Minute, SkipOverlap: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scheduleFromSpec(json.RawMessage(tt.spec), tt.skipOverlap)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestScheduleSurvivesWriteAndRead(t *testing.T) {
	tests := []struct {
		name     string
		schedule domain.Schedule
	}{
		{"cron", domain.Schedule{Kind: domain.ScheduleKindCron, Cron: "*/5 * * * *", SkipOverlap: true}},
		{"every", domain.Schedule{Kind: domain.ScheduleKindEvery, Every: 2 * time.Second}},
		{
			"daily",
			domain.Schedule{
				Kind:  domain.ScheduleKindDaily,
				Daily: []domain.TimeOfDay{{Hour: 17, Minute: 5}, {Hour: 0, Minute: 0}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(specToJSON(tt.schedule))
			require.NoError(t, err)

			got, err := scheduleFromSpec(raw, tt.schedule.SkipOverlap)
			require.NoError(t, err)

			assert.Equal(t, tt.schedule, got)
		})
	}
}

func TestInvalidScheduleSpecIsAnError(t *testing.T) {
	_, err := scheduleFromSpec(json.RawMessage(`{"kind":`), false)
	assert.ErrorContains(t, err, "unmarshal schedule")
}

func TestEntryFromRowSetsRefAndNextTick(t *testing.T) {
	spec := json.RawMessage(`{"kind":"every","every_ns":1000000000}`)
	next := time.Date(2026, time.October, 12, 9, 30, 1, 0, time.UTC)

	tests := []struct {
		name string
		next *time.Time
		want *time.Time
	}{
		{"a schedule with a next tick", &next, &next},
		{"a NULL next tick stays nil", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := entryFromRow("worker-a", "task-b", spec, true, tt.next)
			require.NoError(t, err)

			assert.Equal(t, "worker-a", got.WorkerID)
			assert.Equal(t, "task-b", got.TaskID)
			assert.Equal(t, domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Second, SkipOverlap: true}, got.Schedule)
			assert.Equal(t, tt.want, got.NextRunAt)
		})
	}
}
