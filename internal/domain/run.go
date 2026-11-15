package domain

import (
	"time"
	"uuid"
)

// MaxOutputBytes is the largest output a Step or a Run may store.
const MaxOutputBytes = 1 << 20 // 1 MiB

// RunStatus is where a Run is in its life.
type RunStatus string

// The statuses of a Run.
const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

// Ended reports whether the Run has ended.
func (s RunStatus) Ended() bool {
	return s == RunStatusSucceeded || s == RunStatusFailed
}

// FailureCode says why a Step or a Run failed.
type FailureCode string

// The failure codes.
const (
	FailureCodeError              FailureCode = "error"
	FailureCodePanic              FailureCode = "panic"
	FailureCodeStepFailed         FailureCode = "step_failed"
	FailureCodeAttemptLost        FailureCode = "attempt_lost"
	FailureCodeStartDeadline      FailureCode = "start_deadline"
	FailureCodeOutputNotEncodable FailureCode = "output_not_encodable"
	FailureCodeOutputTooLarge     FailureCode = "output_too_large"
)

// Retriable reports whether a failure of this code may be tried again. A panic and an
// output that cannot be encoded or is too large fail the same way every time.
func (c FailureCode) Retriable() bool {
	return c == FailureCodeError
}

// Failure is why a Step or a Run failed. StepID is set on a Run that failed with a
// Step's final error.
type Failure struct {
	Code    FailureCode
	Message string
	StepID  string
}

// Lease names one attempt of one Run.
type Lease struct {
	RunID uuid.UUID
	Epoch int64
}

// Assign is an attempt given to a worker, with what replay needs.
type Assign struct {
	Lease     Lease
	TaskName  string
	Input     []byte
	Steps     []StepRecord
	CreatedAt time.Time
}

// RunResult is how the handler of an attempt ended, as the worker reports it: an output,
// or a failure.
type RunResult struct {
	Output  []byte
	Failure *Failure
}

// End is how the result ends the Run. A failure that carries a Step's ID is that Step's
// final error, so the Run fails with step_failed.
func (r RunResult) End() RunEnd {
	if r.Failure == nil {
		return RunEnd{Status: RunStatusSucceeded, Output: r.Output}
	}

	failure := *r.Failure
	if failure.StepID != "" {
		failure.Code = FailureCodeStepFailed
	}

	return RunEnd{Status: RunStatusFailed, Failure: &failure}
}

// RunEnd is how an attempt ends a Run.
type RunEnd struct {
	Status  RunStatus
	Output  []byte
	Failure *Failure
}
