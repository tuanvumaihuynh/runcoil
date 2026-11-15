package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"
)

// StepKind is what a Step record records.
type StepKind string

// The kinds of Step.
const (
	StepKindDo StepKind = "do"
)

// StepStatus says whether a Step succeeded or failed.
type StepStatus string

// The statuses of a Step record.
const (
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
)

// StepRecord is the stored result of one Step. Failure is set when the Step failed for good.
type StepRecord struct {
	StepID  string
	Kind    StepKind
	Output  []byte
	Failure *Failure
}

// StepOutcome is one try of a Step as the worker reports it: an output, or a failure.
type StepOutcome struct {
	Output  []byte
	Failure *Failure
}

// Status is the status the outcome is stored with. A failed try is final until Steps
// are retried.
func (o StepOutcome) Status() StepStatus {
	if o.Failure != nil {
		return StepStatusFailed
	}

	return StepStatusSucceeded
}

// CompleteStepParams stores the outcome of one try of a Do Step for the attempt that
// holds Lease. Retry is what the worker says about retrying a failed try.
type CompleteStepParams struct {
	WorkerID uuid.UUID
	Lease    Lease
	StepID   string
	Outcome  StepOutcome
	Retry    StepRetry
}

// Validate checks what the request rules do not: that the Step ID has no NUL byte, and
// the retry.
func (p CompleteStepParams) Validate() error {
	if strings.ContainsRune(p.StepID, 0) {
		return errors.New("step id must not contain a NUL byte")
	}

	if err := p.Retry.Validate(); err != nil {
		return fmt.Errorf("retry: %w", err)
	}

	return nil
}

// StepRetry is what a worker says about retrying a failed try of a Step.
type StepRetry struct {
	// Policy is the Step's own policy. Nil: the Run's policy.
	Policy *RetryPolicy
	// NonRetriable says the Step must not be tried again.
	NonRetriable bool
	// After replaces the policy's delay before the next try.
	After *time.Duration
}

// Validate checks the Step's own policy and that After is not negative.
func (r StepRetry) Validate() error {
	if r.Policy != nil {
		if err := r.Policy.Validate(); err != nil {
			return fmt.Errorf("retry policy: %w", err)
		}
	}

	if r.After != nil && *r.After < 0 {
		return errors.New("retry after must not be negative")
	}

	return nil
}

// RetryDecider decides whether a failed try of a Step is tried again. tries counts the
// tries of the Step, this one included, and runPolicy is the Run's policy, nil when the
// Run has none. It returns the wait before the next try, and false when the failure is
// final.
type RetryDecider func(tries int, runPolicy *RetryPolicy) (time.Duration, bool)

// ScheduledRetry is the next try of a Step whose try failed.
type ScheduledRetry struct {
	NextAttemptAt time.Time
	// TryCount is how many tries of the Step failed so far.
	TryCount int
}

// StepResult is what one try of a Step leads to: its stored record, or a retry.
type StepResult struct {
	Record StepRecord
	Retry  *ScheduledRetry
}
