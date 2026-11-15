package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunResultEnd(t *testing.T) {
	assert.Equal(t, RunEnd{Status: RunStatusSucceeded, Output: []byte(`1`)}, RunResult{Output: []byte(`1`)}.End())

	panicked := RunResult{Failure: &Failure{Code: FailureCodePanic, Message: "boom"}}
	assert.Equal(t, RunEnd{Status: RunStatusFailed, Failure: &Failure{Code: FailureCodePanic, Message: "boom"}}, panicked.End())

	stepFailed := RunResult{Failure: &Failure{Code: FailureCodeError, Message: "smtp down", StepID: "email"}}
	assert.Equal(t, RunEnd{
		Status:  RunStatusFailed,
		Failure: &Failure{Code: FailureCodeStepFailed, Message: "smtp down", StepID: "email"},
	}, stepFailed.End())
}

func TestRunStatusEnded(t *testing.T) {
	assert.False(t, RunStatusQueued.Ended())
	assert.False(t, RunStatusRunning.Ended())
	assert.True(t, RunStatusSucceeded.Ended())
	assert.True(t, RunStatusFailed.Ended())
}

func TestFailureCodeRetriable(t *testing.T) {
	assert.True(t, FailureCodeError.Retriable())
	assert.False(t, FailureCodePanic.Retriable())
	assert.False(t, FailureCodeOutputNotEncodable.Retriable())
	assert.False(t, FailureCodeOutputTooLarge.Retriable())
}
