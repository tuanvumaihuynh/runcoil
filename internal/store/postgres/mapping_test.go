package postgres

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

func TestRunRetryPolicy(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   *domain.RetryPolicy
	}{
		{name: "no config", config: ``},
		{name: "empty config", config: `{}`},
		{name: "no retry", config: `{"retry": null}`},
		{name: "a policy", config: `{"retry": {"max_attempts": 3, "delays": ["5s", "1m30s"]}}`,
			want: &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{5 * time.Second, 90 * time.Second}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runRetryPolicy(json.RawMessage(tt.config))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err := runRetryPolicy(json.RawMessage(`{"retry": {"delays": ["soon"]}}`))
	assert.ErrorContains(t, err, `parse delay "soon"`)
}

func TestRetryPolicyJSONRoundTrip(t *testing.T) {
	policy := domain.RetryPolicy{MaxAttempts: 5, Delays: []time.Duration{time.Second, time.Hour}}

	got, err := retryPolicyFromJSON(retryPolicyToJSON(policy))
	require.NoError(t, err)
	assert.Equal(t, policy, got)
}

func TestRunDetailFromRow(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	workerID := uuid.NewV7()
	step := "sum-totals"
	count := int16(2)
	base := gen.RunGetWithAttemptRow{
		ID:           uuid.NewV7(),
		Status:       "failed",
		Config:       json.RawMessage(`{"retry": {"max_attempts": 3, "delays": ["2s"]}}`),
		ErrorCode:    new("step_failed"),
		ErrorMessage: new("totals service timed out"),
	}

	run, err := runDetailFromRow(base, []gen.StepListByRunRow{
		{StepID: "load-orders", Kind: "do", Status: "succeeded", Output: []byte(`142`), TryCount: 1, CreatedAt: now},
		{StepID: "sum-totals", Kind: "do", Status: "failed", ErrorCode: new("error"), ErrorMessage: new("timed out"), TryCount: 3, CreatedAt: now},
	})
	require.NoError(t, err)

	assert.Equal(t, &domain.Failure{Code: domain.FailureCodeStepFailed, Message: "totals service timed out"}, run.Failure)
	assert.Equal(t, &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{2 * time.Second}}, run.Retry)
	assert.Nil(t, run.Waiting)
	assert.Nil(t, run.Attempt)
	require.Len(t, run.Steps, 2)
	assert.Equal(t, []byte(`142`), run.Steps[0].Output)
	assert.Equal(t, &domain.Failure{Code: domain.FailureCodeError, Message: "timed out"}, run.Steps[1].Failure)
	assert.Equal(t, 3, run.Steps[1].TryCount)

	queued := base
	queued.QueueReadyAt = &now
	queued.QueueRetryCount = new(int16(0))
	run, err = runDetailFromRow(queued, nil)
	require.NoError(t, err)
	assert.Equal(t, &domain.RunWaiting{Reason: domain.RunWaitWorker, ReadyAt: now}, run.Waiting)

	delayed := base
	delayed.DelayedReadyAt = &now
	delayed.DelayedRetryStepID = &step
	delayed.DelayedRetryCount = &count
	run, err = runDetailFromRow(delayed, nil)
	require.NoError(t, err)
	assert.Equal(t, &domain.RunWaiting{Reason: domain.RunWaitRetry, ReadyAt: now, RetryStepID: &step, RetryCount: 2}, run.Waiting)

	leased := base
	leased.LeaseWorkerID = &workerID
	leased.LeaseStartedAt = &now
	run, err = runDetailFromRow(leased, nil)
	require.NoError(t, err)
	assert.Equal(t, &domain.RunAttempt{WorkerID: workerID, StartedAt: now}, run.Attempt)
	assert.Nil(t, run.Waiting)
}
