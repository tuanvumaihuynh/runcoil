package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStepRetryValidate(t *testing.T) {
	second, negative := time.Second, -time.Second

	assert.NoError(t, StepRetry{}.Validate())
	assert.NoError(t, StepRetry{Policy: &RetryPolicy{MaxAttempts: 3}, After: &second}.Validate())
	assert.EqualError(t, StepRetry{Policy: &RetryPolicy{MaxAttempts: 31}}.Validate(), "retry policy: max attempts must be between 0 and 30")
	assert.EqualError(t, StepRetry{After: &negative}.Validate(), "retry after must not be negative")
}

func TestCompleteStepParamsValidate(t *testing.T) {
	assert.NoError(t, CompleteStepParams{StepID: "charge"}.Validate())
	assert.EqualError(t, CompleteStepParams{StepID: "bad\x00id"}.Validate(), "step id must not contain a NUL byte")
	assert.EqualError(t,
		CompleteStepParams{StepID: "charge", Retry: StepRetry{Policy: &RetryPolicy{MaxAttempts: 31}}}.Validate(),
		"retry: retry policy: max attempts must be between 0 and 30")
}
