package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScheduledStartDeadline(t *testing.T) {
	tick := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		now  time.Time
		next time.Time
		want time.Time
	}{
		{
			name: "a cron of one second waits at least a minute",
			now:  tick,
			next: tick.Add(time.Second),
			want: tick.Add(time.Minute),
		},
		{
			name: "a cron of one minute waits until the next tick",
			now:  tick,
			next: tick.Add(time.Minute),
			want: tick.Add(time.Minute),
		},
		{
			name: "a daily cron waits until the next tick",
			now:  tick,
			next: tick.Add(24 * time.Hour),
			want: tick.Add(24 * time.Hour),
		},
		{
			name: "a tick fired slightly late counts from the tick, not the sweep",
			now:  tick.Add(5 * time.Second),
			next: tick.Add(time.Minute),
			want: tick.Add(time.Minute),
		},
		{
			name: "a tick fired over a minute late on a one-second cron waits a minute from now",
			now:  tick.Add(5 * time.Minute),
			next: tick.Add(5*time.Minute + time.Second),
			want: tick.Add(6 * time.Minute),
		},
		{
			name: "a daily tick fired over a minute late still waits until the next tick",
			now:  tick.Add(5 * time.Minute),
			next: tick.Add(24 * time.Hour),
			want: tick.Add(24 * time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, scheduledStartDeadline(tick, tt.now, tt.next))
		})
	}
}
