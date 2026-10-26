package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres/sqlc"
)

func TestRetryPolicyIsStoredWithTheseFields(t *testing.T) {
	policy := domain.RetryPolicy{
		MaxAttempts: 3,
		Delays:      []time.Duration{time.Second, 2 * time.Second},
	}

	raw, err := retryRawParams(&policy)
	require.NoError(t, err)
	require.NotNil(t, raw)

	assert.JSONEq(t, `{"max_attempts":3,"delays_ns":[1000000000,2000000000]}`, string(*raw))
}

func TestRetryPolicyReadsStoredRow(t *testing.T) {
	// The stored shape of a retry policy: delays in nanoseconds, in the order they were tried.
	raw := json.RawMessage(`{"max_attempts":3,"delays_ns":[1000000000,2000000000]}`)

	got, err := retryFromRaw(raw)
	require.NoError(t, err)

	want := domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second, 2 * time.Second}}
	assert.Equal(t, want, got)
}

func TestNilRetryPolicyIsNull(t *testing.T) {
	raw, err := retryRawParams(nil)
	require.NoError(t, err)
	assert.Nil(t, raw, "a Task without a retry policy is stored as NULL")
}

func TestRetryPolicyKeepsDelaysInOrder(t *testing.T) {
	policy := domain.RetryPolicy{
		MaxAttempts: 4,
		Delays:      []time.Duration{5 * time.Second, time.Second, 3 * time.Second},
	}

	raw, err := retryRawParams(&policy)
	require.NoError(t, err)
	require.NotNil(t, raw)

	got, err := retryFromRaw(*raw)
	require.NoError(t, err)

	assert.Equal(t, policy, got)
}

func TestInvalidRetryPolicyJSONIsAnError(t *testing.T) {
	raw := json.RawMessage(`{"max_attempts":`)

	_, err := retryFromRaw(raw)
	assert.ErrorContains(t, err, "unmarshal retry policy")
}

func TestTaskSurvivesWriteAndRead(t *testing.T) {
	task := domain.Task{
		WorkerID:      "worker-a",
		TaskID:        "task-b",
		Retry:         &domain.RetryPolicy{MaxAttempts: 2, Delays: []time.Duration{time.Second}},
		StartDeadline: 90 * time.Second,
		RegisteredAt:  testTime,
	}

	params, err := taskUpsertParams(task)
	require.NoError(t, err)

	rowID, err := uuid.Parse(params.ID)
	require.NoError(t, err, "the row ID is a UUID string")
	assert.Equal(t, uuid.Version(7), rowID.Version(), "the row ID is a UUIDv7")

	row := sqlc.Task{
		ID:              params.ID,
		WorkerID:        params.WorkerID,
		TaskID:          params.TaskID,
		RetryPolicy:     params.RetryPolicy,
		StartDeadlineNs: params.StartDeadlineNs,
		RegisteredAt:    testTime,
	}

	got, err := taskFromRow(row)
	require.NoError(t, err)

	assert.Equal(t, task, got)
}

func TestTaskWithoutRetryOrDeadlineMapsNullsBothWays(t *testing.T) {
	params, err := taskUpsertParams(domain.Task{WorkerID: "worker-a", TaskID: "task-b"})
	require.NoError(t, err)
	assert.Nil(t, params.RetryPolicy, "no retry policy is stored as NULL")
	assert.Nil(t, params.StartDeadlineNs, "no start deadline is stored as NULL")

	got, err := taskFromRow(sqlc.Task{WorkerID: "worker-a", TaskID: "task-b", RegisteredAt: testTime})
	require.NoError(t, err)

	want := domain.Task{WorkerID: "worker-a", TaskID: "task-b", RegisteredAt: testTime}
	assert.Equal(t, want, got)
}
