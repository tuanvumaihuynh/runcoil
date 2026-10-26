package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultTable is the server default: 4 tries, and the delays of cmd/runcoil/config.yml.
var defaultTable = RetryConfig{
	MaxAttempts: 4,
	Delays: []time.Duration{
		5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute,
		3 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour,
	},
	Jitter: 0.2,
}

func TestResolveRetryOrder(t *testing.T) {
	stepPolicy := &RetryPolicy{MaxAttempts: 2, Delays: []time.Duration{time.Second}}
	taskPolicy := &RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{2 * time.Second}}

	tests := []struct {
		name        string
		step, task  *RetryPolicy
		wantMax     int
		wantFirstMS time.Duration
	}{
		{"step policy wins over task", stepPolicy, taskPolicy, 2, time.Second},
		{"task policy when no step policy", nil, taskPolicy, 3, 2 * time.Second},
		{"server default when neither", nil, nil, 4, 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRetry(tt.step, tt.task, defaultTable)
			require.Equal(t, tt.wantMax, got.MaxAttempts, "max attempts")
			require.Equal(t, tt.wantFirstMS, got.Delays[0], "first delay")
		})
	}
}

func TestResolveRetryIsFieldByField(t *testing.T) {
	taskPolicy := &RetryPolicy{MaxAttempts: 7, Delays: []time.Duration{time.Second}}

	// A Step with no limit and no delays gets the Task's limit and delays.
	got := ResolveRetry(&RetryPolicy{}, taskPolicy, defaultTable)
	require.Equal(t, 7, got.MaxAttempts, "empty step policy")
	require.Len(t, got.Delays, 1, "empty step policy")
	require.Equal(t, time.Second, got.Delays[0], "empty step policy")

	// A Step with only delays keeps the Task's limit, which the old rule lost.
	onlyDelays := ResolveRetry(&RetryPolicy{Delays: []time.Duration{2 * time.Second}}, taskPolicy, defaultTable)
	require.Equal(t, 7, onlyDelays.MaxAttempts, "delays-only step policy")
	require.Len(t, onlyDelays.Delays, 1, "delays-only step policy")
	require.Equal(t, 2*time.Second, onlyDelays.Delays[0], "delays-only step policy")

	// A Step with only a limit keeps the Task's delays.
	onlyLimit := ResolveRetry(&RetryPolicy{MaxAttempts: 2}, taskPolicy, defaultTable)
	require.Equal(t, 2, onlyLimit.MaxAttempts, "limit-only step policy")
	require.Len(t, onlyLimit.Delays, 1, "limit-only step policy")
	require.Equal(t, time.Second, onlyLimit.Delays[0], "limit-only step policy")

	// Without a Task policy, the server default fills each field that is not set.
	defaulted := ResolveRetry(&RetryPolicy{MaxAttempts: 2}, nil, defaultTable)
	require.Equal(t, 2, defaulted.MaxAttempts, "no task policy")
	require.Len(t, defaulted.Delays, len(defaultTable.Delays), "no task policy")

	none := ResolveRetry(nil, nil, defaultTable)
	require.Equal(t, 4, none.MaxAttempts, "no policy at all")
	require.Len(t, none.Delays, len(defaultTable.Delays), "no policy at all")
}

// A failure outside any Step passes no Step policy: the Task's values, then the default.
func TestResolveRetryOutsideStepUsesTaskThenDefault(t *testing.T) {
	task := &RetryPolicy{MaxAttempts: 3}
	got := ResolveRetry(nil, task, defaultTable)
	require.Equal(t, 3, got.MaxAttempts, "the Task's limit")
	require.Len(t, got.Delays, len(defaultTable.Delays), "the default delays")
}

func TestResolveRetryDoesNotAliasDefaults(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 2, Delays: []time.Duration{time.Second}}
	got := ResolveRetry(nil, nil, cfg)
	got.Delays[0] = time.Hour
	require.Equal(t, time.Second, cfg.Delays[0], "resolved delays share memory with the config")
}

func TestDelayAfterFollowsTableAndRepeatsLast(t *testing.T) {
	p := ResolveRetry(nil, nil, defaultTable)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 30 * time.Second},
		{4, time.Minute},
		{7, 30 * time.Minute},
		{8, time.Hour},
		{50, time.Hour},
	}

	for _, tt := range tests {
		// unit 0.5 is the middle of the jitter range, so the table value is exact.
		assert.Equal(t, tt.want, DelayAfter(p, tt.attempt, nil, 0.2, 0.5), "DelayAfter(attempt %d)", tt.attempt)
	}
}

func TestDelayAfterJitterStaysWithinBounds(t *testing.T) {
	p := RetryPolicy{Delays: []time.Duration{10 * time.Second}}
	const jitter = 0.2
	low := DelayAfter(p, 1, nil, jitter, 0)
	high := DelayAfter(p, 1, nil, jitter, 0.999999)

	assert.Equal(t, 8*time.Second, low, "lowest jittered delay")
	assert.GreaterOrEqual(t, high, 11*time.Second, "highest jittered delay")
	assert.LessOrEqual(t, high, 12*time.Second, "highest jittered delay, just under 12s")

	for i := range 100 {
		unit := float64(i) / 100
		got := DelayAfter(p, 1, nil, jitter, unit)
		require.GreaterOrEqual(t, got, low, "unit %v", unit)
		require.LessOrEqual(t, got, high, "unit %v", unit)
	}
}

func TestDelayAfterZeroJitterIsExact(t *testing.T) {
	p := RetryPolicy{Delays: []time.Duration{3 * time.Second}}
	for _, unit := range []float64{0, 0.5, 0.99} {
		assert.Equal(t, 3*time.Second, DelayAfter(p, 1, nil, 0, unit), "unit %v", unit)
	}
}

func TestDelayAfterRetryAfterIsExact(t *testing.T) {
	p := RetryPolicy{Delays: []time.Duration{time.Hour}}
	for _, unit := range []float64{0, 0.3, 0.99} {
		d := 42 * time.Second
		assert.Equal(t, d, DelayAfter(p, 1, &d, 0.2, unit), "unit %v: retry_after", unit)
	}
}

func TestDelayAfterZeroRetryAfterOverridesPolicy(t *testing.T) {
	p := RetryPolicy{Delays: []time.Duration{3 * time.Second}}
	d := time.Duration(0)
	assert.Zero(t, DelayAfter(p, 1, &d, 0, 0.5), "set zero retry_after means no delay")
}

func TestExhaustedNeverRetriedAndNormalizeCode(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 3}
	assert.False(t, p.Exhausted(2), "attempt 2 of 3 must not be exhausted")
	assert.True(t, p.Exhausted(3), "attempt 3 of 3 must be exhausted")

	assert.True(t, NeverRetried(FailureCodePanic), "PANIC must never be retried")
	assert.True(t, NeverRetried(FailureCodeOutputNotEncodable), "OUTPUT_NOT_ENCODABLE must never be retried")
	assert.False(t, NeverRetried(FailureCodeError), "ERROR must be retried")
	assert.False(t, NeverRetried(FailureCodeStepFailed), "a failed Step must be retried")

	tests := map[FailureCode]FailureCode{
		FailureCodeError:              FailureCodeError,
		FailureCodePanic:              FailureCodePanic,
		FailureCodeOutputNotEncodable: FailureCodeOutputNotEncodable,
		FailureCodeStepFailed:         FailureCodeError,
		FailureCodeAttemptLost:        FailureCodeError,
		FailureCode(""):               FailureCodeError,
	}

	for in, want := range tests {
		assert.Equal(t, want, NormalizeCode(in), "NormalizeCode(%q)", in)
	}
}
