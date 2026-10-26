package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecideKeyCreatesDeduplicatesAndConflicts(t *testing.T) {
	fp := Fingerprint("invoke", "demo", "hello", `{"n":1}`)
	require.Equal(t, KeyCreate, DecideKey(nil, fp), "no key held")

	holder := &KeyHolder{Fingerprint: fp}
	require.Equal(t, KeyDeduplicated, DecideKey(holder, fp), "same key, same request")

	other := Fingerprint("invoke", "demo", "hello", `{"n":2}`)
	require.Equal(t, KeyConflict, DecideKey(holder, other), "same key, different input")
}

func TestFingerprintSeparatesParts(t *testing.T) {
	// Without length prefixes these two would have the same bytes.
	require.NotEqual(t, Fingerprint("ab", "c"), Fingerprint("a", "bc"),
		"different splits of the same text must not share a fingerprint")
	require.NotEqual(t, Fingerprint("demo", "hello", ""), Fingerprint("demo", "hello"),
		"an empty part is still a part")
	require.Equal(t, Fingerprint("invoke", "demo", "x"), Fingerprint("invoke", "demo", "x"),
		"a fingerprint is deterministic")
}

func TestInputAndRunOutputLimits(t *testing.T) {
	l := Limits{Input: 4, RunOutput: 4}
	assert.False(t, InputTooLarge([]byte("abcd"), l), "input at the limit is allowed")
	assert.True(t, InputTooLarge([]byte("abcde"), l), "input one over the limit is not")
	assert.False(t, RunOutputTooLarge([]byte("abcd"), l), "run output at the limit is allowed")
	assert.True(t, RunOutputTooLarge([]byte("abcde"), l), "run output one over the limit is not")
	assert.False(t, InputTooLarge(make([]byte, 1<<20), Limits{}), "a zero limit is not checked")
}

func TestCheckStepOutputTable(t *testing.T) {
	run := Run{Input: []byte("1234")}
	done := func(id string, n int) StepRecord {
		return StepRecord{StepID: id, Status: StepStatusSucceeded, Output: make([]byte, n)}
	}

	tests := []struct {
		name  string
		run   Run
		steps []StepRecord
		out   int
		l     Limits
		want  FailureCode // "" means allowed
	}{
		{"within both limits", run, nil, 10, Limits{StepOutput: 10, RunTotal: 100}, ""},
		{"step output over its own limit", run, nil, 11, Limits{StepOutput: 10, RunTotal: 100}, FailureCodeOutputTooLarge},
		{"total over the run limit with input and steps", run, []StepRecord{done("a", 20)}, 10,
			Limits{StepOutput: 100, RunTotal: 30}, FailureCodeRunDataLimit},
		{"total exactly at the run limit is allowed", run, []StepRecord{done("a", 20)}, 6,
			Limits{StepOutput: 100, RunTotal: 30}, ""},
		{"failed steps hold no data", run, []StepRecord{{StepID: "a", Status: StepStatusFailed}}, 10,
			Limits{StepOutput: 100, RunTotal: 14}, ""},
		{"zero limits are not checked", run, []StepRecord{done("a", 1000)}, 1000, Limits{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := CheckStepOutput(tt.run, tt.steps, "b", 2, make([]byte, tt.out), tt.l)
			if tt.want == "" {
				require.Nil(t, f)

				return
			}
			require.NotNil(t, f)
			require.Equal(t, tt.want, f.Code, "failure code")
			require.Equal(t, "b", f.StepID, "failed step")
			require.Equal(t, 2, f.Attempts, "attempts")
		})
	}
}

func finishedRun(status RunStatus) Run {
	return Run{ID: "run-1", WorkerID: "demo", TaskID: "hello", Status: status}
}

func TestPlanRerunNeedsAFinishedRun(t *testing.T) {
	for _, status := range []RunStatus{RunStatusQueued, RunStatusRunning} {
		_, err := PlanRerun(finishedRun(status), nil, "")
		require.ErrorIs(t, err, ErrRunNotFinished, "status %s", status)
	}

	for _, status := range []RunStatus{RunStatusSucceeded, RunStatusFailed} {
		_, err := PlanRerun(finishedRun(status), nil, "")
		require.NoError(t, err, "status %s", status)
	}
}

func TestPlanRerunFromTheStartCopiesNothing(t *testing.T) {
	steps := []StepRecord{{StepID: "a", Position: 1, Status: StepStatusSucceeded}}
	plan, err := PlanRerun(finishedRun(RunStatusSucceeded), steps, "")
	require.NoError(t, err)
	require.Empty(t, plan.Copied, "nothing is copied")
}

func TestPlanRerunFromAStepCopiesTheEarlierFinishedRecords(t *testing.T) {
	steps := []StepRecord{
		{StepID: "reserve", Position: 1, Status: StepStatusSucceeded, Output: []byte(`"r"`)},
		{StepID: "charge#1", Position: 2, Status: StepStatusFailed},
		{StepID: "charge#2", Position: 3, Status: StepStatusSucceeded},
		{StepID: "ship", Position: 4, Status: StepStatusSucceeded},
	}

	plan, err := PlanRerun(finishedRun(RunStatusFailed), steps, "charge#2")
	require.NoError(t, err)
	require.Len(t, plan.Copied, 2, "reserve and charge#1 are copied")
	require.Equal(t, "reserve", plan.Copied[0].StepID)
	require.Equal(t, 1, plan.Copied[0].Position)
	require.Equal(t, "charge#1", plan.Copied[1].StepID)
	require.Equal(t, 2, plan.Copied[1].Position)
	require.Equal(t, StepStatusFailed, plan.Copied[1].Status)
}

func TestPlanRerunRejectsAnUnknownStep(t *testing.T) {
	steps := []StepRecord{{StepID: "reserve", Position: 1, Status: StepStatusSucceeded}}
	_, err := PlanRerun(finishedRun(RunStatusSucceeded), steps, "charge")
	require.ErrorIs(t, err, ErrStepNotFound)
}

// A pending Step has no final record, so it cannot be the selected Step of a rerun.
func TestPlanRerunRefusesAPendingStep(t *testing.T) {
	steps := []StepRecord{{StepID: "pending", Position: 1, Status: StepStatusPending, RetryNotBefore: time.Now()}}
	_, err := PlanRerun(finishedRun(RunStatusFailed), steps, "pending")
	require.ErrorIs(t, err, ErrStepNotFound, "a pending Step")
}

func TestPlanRerunLeavesOutAPendingRecord(t *testing.T) {
	steps := []StepRecord{
		{StepID: "pending", Position: 1, Status: StepStatusPending, RetryNotBefore: time.Now()},
		{StepID: "done", Position: 2, Status: StepStatusSucceeded},
	}

	plan, err := PlanRerun(finishedRun(RunStatusFailed), steps, "done")
	require.NoError(t, err)
	require.Empty(t, plan.Copied, "a pending record is not an outcome to keep")
}
