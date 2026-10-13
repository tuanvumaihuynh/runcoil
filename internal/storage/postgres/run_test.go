package postgres

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres/sqlc"
)

// testTime is a fixed UTC time for the rows and domain values below.
var testTime = time.Date(2026, time.October, 12, 9, 30, 0, 0, time.UTC)

func ptr[T any](v T) *T {
	return &v
}

func TestFailureIsStoredWithTheseFields(t *testing.T) {
	tests := []struct {
		name    string
		failure domain.Failure
		want    string
	}{
		{
			name:    "a step failure keeps its step ID",
			failure: domain.Failure{Code: domain.FailureCodeStepFailed, Message: "boom", Attempts: 3, StepID: "fetch"},
			want:    `{"code":"STEP_FAILED","message":"boom","attempts":3,"step_id":"fetch"}`,
		},
		{
			name:    "a run failure without a step leaves step_id out",
			failure: domain.Failure{Code: domain.FailureCodeError, Message: "boom", Attempts: 1},
			want:    `{"code":"ERROR","message":"boom","attempts":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := failureParams(&tt.failure)
			require.NoError(t, err)
			require.NotNil(t, raw)

			assert.JSONEq(t, tt.want, string(*raw))
		})
	}
}

func TestFailureReadsStoredRow(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want domain.Failure
	}{
		{
			name: "a row written with a step",
			raw:  `{"code":"STEP_FAILED","message":"boom","attempts":3,"step_id":"fetch"}`,
			want: domain.Failure{Code: domain.FailureCodeStepFailed, Message: "boom", Attempts: 3, StepID: "fetch"},
		},
		{
			name: "a row written without a step",
			raw:  `{"code":"ERROR","message":"boom","attempts":1}`,
			want: domain.Failure{Code: domain.FailureCodeError, Message: "boom", Attempts: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)

			got, err := failureFromRaw(&raw)
			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Equal(t, tt.want, *got)
		})
	}
}

func TestNilFailureIsNullInBothDirections(t *testing.T) {
	raw, err := failureParams(nil)
	require.NoError(t, err)
	assert.Nil(t, raw, "no failure is stored as NULL")

	failure, err := failureFromRaw(nil)
	require.NoError(t, err)
	assert.Nil(t, failure, "a NULL column reads as no failure")
}

func TestInvalidFailureJSONIsAnError(t *testing.T) {
	raw := json.RawMessage(`{"code":`)

	_, err := failureFromRaw(&raw)
	assert.ErrorContains(t, err, "unmarshal failure")
}

func TestRunFromRowReadsEveryColumn(t *testing.T) {
	row := sqlc.Run{
		ID:                  "run-1",
		WorkerID:            "worker-a",
		TaskID:              "task-b",
		Status:              "failed",
		QueuedReason:        "waiting_for_worker",
		Source:              "rerun",
		Attempts:            2,
		Input:               []byte(`{"n":1}`),
		Output:              []byte(`{"ok":true}`),
		Failure:             ptr(json.RawMessage(`{"code":"STEP_FAILED","message":"boom","attempts":2,"step_id":"fetch"}`)),
		LeaseToken:          "lease-1",
		LeaseExpiresAt:      ptr(testTime.Add(time.Minute)),
		InstanceID:          "instance-1",
		AckedAt:             ptr(testTime.Add(2 * time.Second)),
		DueAt:               testTime,
		CreatedAt:           testTime.Add(-time.Hour),
		StartedAt:           ptr(testTime.Add(time.Second)),
		FinishedAt:          ptr(testTime.Add(2 * time.Minute)),
		RerunOf:             ptr("run-0"),
		HandlerAttemptsUsed: 1,
		EndedToken:          "lease-0",
		EndedKind:           "released",
		NextAttemptAt:       ptr(testTime.Add(time.Hour)),
		RunningStepID:       "fetch",
		RunningStepAttempt:  2,
		WorkerWaitSince:     ptr(testTime.Add(-time.Second)),
		EndedAt:             ptr(testTime.Add(3 * time.Minute)),
		RunningStepPolicy:   ptr(json.RawMessage(`{"max_attempts":3,"delays_ns":[1000000000]}`)),
	}

	want := domain.Run{
		ID:                  "run-1",
		Task:                domain.TaskRef{WorkerID: "worker-a", TaskID: "task-b"},
		Status:              domain.RunStatusFailed,
		QueuedReason:        domain.QueuedReasonWaitingForWorker,
		Source:              domain.RunSourceRerun,
		Attempts:            2,
		Input:               []byte(`{"n":1}`),
		Output:              []byte(`{"ok":true}`),
		Failure:             &domain.Failure{Code: domain.FailureCodeStepFailed, Message: "boom", Attempts: 2, StepID: "fetch"},
		LeaseToken:          "lease-1",
		LeaseExpiresAt:      testTime.Add(time.Minute),
		InstanceID:          "instance-1",
		AckedAt:             testTime.Add(2 * time.Second),
		DueAt:               testTime,
		CreatedAt:           testTime.Add(-time.Hour),
		StartedAt:           testTime.Add(time.Second),
		FinishedAt:          testTime.Add(2 * time.Minute),
		RerunOf:             "run-0",
		HandlerAttemptsUsed: 1,
		EndedToken:          "lease-0",
		EndedKind:           domain.EndKindReleased,
		NextAttemptAt:       testTime.Add(time.Hour),
		EndedAt:             testTime.Add(3 * time.Minute),
		RunningStepID:       "fetch",
		RunningStepAttempt:  2,
		RunningStepPolicy:   &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second}},
		WorkerWaitSince:     testTime.Add(-time.Second),
	}

	got, err := runFromRow(row)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestRunFromRowMapsNullColumnsToZeroValues(t *testing.T) {
	row := sqlc.Run{
		ID:        "run-1",
		WorkerID:  "worker-a",
		TaskID:    "task-b",
		Status:    "queued",
		Source:    "invoke",
		DueAt:     testTime,
		CreatedAt: testTime,
	}

	want := domain.Run{
		ID:        "run-1",
		Task:      domain.TaskRef{WorkerID: "worker-a", TaskID: "task-b"},
		Status:    domain.RunStatusQueued,
		Source:    domain.RunSourceInvoke,
		DueAt:     testTime,
		CreatedAt: testTime,
	}

	got, err := runFromRow(row)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestRunListingRowMapsColumnsWithoutPayloads(t *testing.T) {
	tests := []struct {
		name string
		row  sqlc.RunListPageRow
		want domain.Run
	}{
		{
			name: "a failed rerun keeps its failure, times, and source run",
			row: sqlc.RunListPageRow{
				ID:            "run-2",
				WorkerID:      "worker-a",
				TaskID:        "task-b",
				Status:        "failed",
				Source:        "rerun",
				Attempts:      2,
				Failure:       ptr(json.RawMessage(`{"code":"ERROR","message":"boom","attempts":2}`)),
				InstanceID:    "instance-1",
				DueAt:         testTime,
				CreatedAt:     testTime.Add(-time.Minute),
				StartedAt:     ptr(testTime.Add(time.Second)),
				FinishedAt:    ptr(testTime.Add(time.Minute)),
				NextAttemptAt: ptr(testTime.Add(time.Hour)),
				RerunOf:       ptr("run-1"),
			},
			want: domain.Run{
				ID:            "run-2",
				Task:          domain.TaskRef{WorkerID: "worker-a", TaskID: "task-b"},
				Status:        domain.RunStatusFailed,
				Source:        domain.RunSourceRerun,
				Attempts:      2,
				Failure:       &domain.Failure{Code: domain.FailureCodeError, Message: "boom", Attempts: 2},
				InstanceID:    "instance-1",
				DueAt:         testTime,
				CreatedAt:     testTime.Add(-time.Minute),
				StartedAt:     testTime.Add(time.Second),
				FinishedAt:    testTime.Add(time.Minute),
				NextAttemptAt: testTime.Add(time.Hour),
				RerunOf:       "run-1",
			},
		},
		{
			name: "NULL columns read as zero values",
			row: sqlc.RunListPageRow{
				ID:        "run-3",
				WorkerID:  "worker-a",
				TaskID:    "task-b",
				Status:    "queued",
				Source:    "invoke",
				DueAt:     testTime,
				CreatedAt: testTime,
			},
			want: domain.Run{
				ID:        "run-3",
				Task:      domain.TaskRef{WorkerID: "worker-a", TaskID: "task-b"},
				Status:    domain.RunStatusQueued,
				Source:    domain.RunSourceInvoke,
				DueAt:     testTime,
				CreatedAt: testTime,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := summaryFromRow(tt.row)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunParamsWriteUnsetFieldsAsNull(t *testing.T) {
	createParams, err := runCreateParams(domain.Run{ID: "run-1", Task: domain.TaskRef{WorkerID: "w", TaskID: "t"}})
	require.NoError(t, err)
	assert.Nil(t, createParams.RerunOf, "a Run that is not a rerun has NULL rerun_of")

	createParams, err = runCreateParams(domain.Run{ID: "run-2", RerunOf: "run-1"})
	require.NoError(t, err)
	assert.Equal(t, ptr("run-1"), createParams.RerunOf)

	updateParams, err := runUpdateParams(domain.Run{ID: "run-1", Status: domain.RunStatusQueued, FinishedAt: testTime})
	require.NoError(t, err)
	assert.Nil(t, updateParams.Failure, "no failure is stored as NULL")
	assert.Nil(t, updateParams.RunningStepPolicy, "no step policy is stored as NULL")
	assert.Nil(t, updateParams.LeaseExpiresAt, "a zero lease time is stored as NULL")
	assert.Nil(t, updateParams.AckedAt, "a zero ack time is stored as NULL")
	assert.Nil(t, updateParams.NextAttemptAt, "a zero retry time is stored as NULL")
	assert.Nil(t, updateParams.EndedAt, "a zero end time is stored as NULL")
	assert.Nil(t, updateParams.WorkerWaitSince, "a zero wait time is stored as NULL")
	assert.Nil(t, updateParams.StartedAt, "a zero start time is stored as NULL")
	assert.Equal(t, ptr(testTime), updateParams.FinishedAt, "a set time is stored as it is")
}

func TestRunSurvivesWriteAndRead(t *testing.T) {
	run := domain.Run{
		ID:                  "run-1",
		Task:                domain.TaskRef{WorkerID: "worker-a", TaskID: "task-b"},
		Status:              domain.RunStatusRunning,
		QueuedReason:        domain.QueuedReasonNone,
		Source:              domain.RunSourceInvoke,
		Attempts:            2,
		Input:               []byte(`{"n":1}`),
		Output:              []byte(`{"ok":true}`),
		Failure:             &domain.Failure{Code: domain.FailureCodeStepFailed, Message: "boom", Attempts: 2, StepID: "fetch"},
		LeaseToken:          "lease-1",
		LeaseExpiresAt:      testTime.Add(time.Minute),
		InstanceID:          "instance-1",
		AckedAt:             testTime.Add(time.Second),
		DueAt:               testTime,
		CreatedAt:           testTime.Add(-time.Hour),
		StartedAt:           testTime.Add(2 * time.Second),
		FinishedAt:          testTime.Add(3 * time.Second),
		RerunOf:             "run-0",
		HandlerAttemptsUsed: 1,
		EndedToken:          "lease-0",
		EndedKind:           domain.EndKindFailed,
		EndedAt:             testTime.Add(4 * time.Second),
		NextAttemptAt:       testTime.Add(time.Hour),
		RunningStepID:       "fetch",
		RunningStepAttempt:  2,
		RunningStepPolicy:   &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{time.Second, 2 * time.Second}},
		WorkerWaitSince:     testTime.Add(-time.Second),
	}

	params, err := runUpdateParams(run)
	require.NoError(t, err)

	// The update writes the mutable columns. The rest of the row keeps the values that
	// the create wrote, so they are copied from the Run here.
	row := sqlc.Run{
		ID:                  params.ID,
		WorkerID:            run.Task.WorkerID,
		TaskID:              run.Task.TaskID,
		Status:              params.Status,
		QueuedReason:        params.QueuedReason,
		Source:              string(run.Source),
		Attempts:            params.Attempts,
		Input:               run.Input,
		Output:              params.Output,
		Failure:             params.Failure,
		LeaseToken:          params.LeaseToken,
		LeaseExpiresAt:      params.LeaseExpiresAt,
		InstanceID:          params.InstanceID,
		AckedAt:             params.AckedAt,
		DueAt:               run.DueAt,
		CreatedAt:           run.CreatedAt,
		StartedAt:           params.StartedAt,
		FinishedAt:          params.FinishedAt,
		RerunOf:             ptr(run.RerunOf),
		HandlerAttemptsUsed: params.HandlerAttemptsUsed,
		EndedToken:          params.EndedToken,
		EndedKind:           params.EndedKind,
		NextAttemptAt:       params.NextAttemptAt,
		RunningStepID:       params.RunningStepID,
		RunningStepAttempt:  params.RunningStepAttempt,
		WorkerWaitSince:     params.WorkerWaitSince,
		EndedAt:             params.EndedAt,
		RunningStepPolicy:   params.RunningStepPolicy,
	}

	got, err := runFromRow(row)
	require.NoError(t, err)

	assert.Equal(t, run, got)
}

func TestStepSurvivesWriteAndRead(t *testing.T) {
	step := domain.StepRecord{
		StepID:         "fetch",
		Position:       2,
		Status:         domain.StepStatusFailed,
		AttemptsUsed:   3,
		Output:         []byte(`{"partial":true}`),
		Failure:        &domain.Failure{Code: domain.FailureCodeStepFailed, Message: "boom", Attempts: 3, StepID: "fetch"},
		RetryNotBefore: testTime,
	}

	params, err := stepUpsertParams("run-1", step)
	require.NoError(t, err)

	row := sqlc.Step{
		StepID:         params.StepID,
		Position:       params.Position,
		Status:         params.Status,
		AttemptsUsed:   params.AttemptsUsed,
		Output:         params.Output,
		Failure:        params.Failure,
		RetryNotBefore: params.RetryNotBefore,
	}

	got, err := stepFromRow(row)
	require.NoError(t, err)

	assert.Equal(t, step, got)
}

func TestStepRowMapsNullColumnsToZeroValues(t *testing.T) {
	row := sqlc.Step{StepID: "fetch", Position: 1, Status: "pending"}

	got, err := stepFromRow(row)
	require.NoError(t, err)
	assert.Equal(t, domain.StepRecord{StepID: "fetch", Position: 1, Status: domain.StepStatusPending}, got)

	params, err := stepUpsertParams("run-1", domain.StepRecord{StepID: "fetch", Position: 1, Status: domain.StepStatusPending})
	require.NoError(t, err)
	assert.Nil(t, params.Failure, "no failure is stored as NULL")
	assert.Nil(t, params.RetryNotBefore, "a zero retry time is stored as NULL")
}

func TestClampInt32AtBothBounds(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int32
	}{
		{"zero is kept", 0, 0},
		{"a negative value is kept", -1, -1},
		{"the largest int32 is kept", math.MaxInt32, math.MaxInt32},
		{"one past the largest int32 is clamped", math.MaxInt32 + 1, math.MaxInt32},
		{"the smallest int32 is kept", math.MinInt32, math.MinInt32},
		{"one past the smallest int32 is clamped", math.MinInt32 - 1, math.MinInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampInt32(tt.in))
		})
	}
}

func TestNullableTimesConvertBothWays(t *testing.T) {
	assert.Nil(t, nullTime(time.Time{}), "the zero time is stored as NULL")
	assert.Equal(t, ptr(testTime), nullTime(testTime), "a set time is stored as it is")

	assert.Equal(t, time.Time{}, timeValue(nil), "NULL reads as the zero time")
	assert.Equal(t, testTime, timeValue(ptr(testTime)), "a stored time reads as it is")
}

func TestOnlyUnfinishedIsSetOnlyForUnfinishedStatuses(t *testing.T) {
	tests := []struct {
		name     string
		statuses []domain.RunStatus
		want     bool
	}{
		{name: "no statuses", statuses: nil, want: false},
		{name: "queued", statuses: []domain.RunStatus{domain.RunStatusQueued}, want: true},
		{name: "running", statuses: []domain.RunStatus{domain.RunStatusRunning}, want: true},
		{name: "queued and running", statuses: []domain.RunStatus{domain.RunStatusQueued, domain.RunStatusRunning}, want: true},
		{name: "queued and failed", statuses: []domain.RunStatus{domain.RunStatusQueued, domain.RunStatusFailed}, want: false},
		{name: "failed", statuses: []domain.RunStatus{domain.RunStatusFailed}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, onlyUnfinished(tt.statuses))
		})
	}
}
