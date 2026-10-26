package domain

import "time"

// StepStatus is the state of a Step record. Its values are stored as text.
type StepStatus string

const (
	// StepStatusPending is a Step with failed tries and no final record.
	StepStatusPending   StepStatus = "pending"
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
)

// StepRecord is the stored outcome of one Step of one Run.
type StepRecord struct {
	StepID string
	// Position is the order in which the Step was first reported, from 1.
	Position     int
	Status       StepStatus
	AttemptsUsed int
	Output       []byte
	Failure      *Failure
	// RetryNotBefore is the earliest start of the next try, while the Step is pending.
	RetryNotBefore *time.Time
}

// Finished reports whether the Step has a final record.
func (s StepRecord) Finished() bool {
	return s.Status == StepStatusSucceeded || s.Status == StepStatusFailed
}

// AttemptError is one failed try reported by a worker.
type AttemptError struct {
	Message string
	// Permanent asks for no further try.
	Permanent bool
	// RetryAfter replaces the policy's delay for the next try, when set.
	RetryAfter *time.Duration
	Code       FailureCode
}

// StepReport is one try of one Step as a worker reports it.
type StepReport struct {
	StepID string
	// Attempt is the number of this try, from 1.
	Attempt   int
	Succeeded bool
	Output    []byte
	Error     AttemptError
	// StepPolicy is the Step's own policy, or nil for the Task's.
	StepPolicy *RetryPolicy
}
