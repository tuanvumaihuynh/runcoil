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

// RunFilter picks the Runs a list holds. An empty Statuses and a nil TaskID match every
// Run.
type RunFilter struct {
	Statuses []RunStatus
	TaskID   *uuid.UUID
}

// RunSummary is a Run as a list shows it, with its Task. Failure is set when it failed.
type RunSummary struct {
	ID        uuid.UUID
	Status    RunStatus
	TaskID    uuid.UUID
	TaskApp   string
	TaskName  string
	Failure   *Failure
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
}

// RunDetail is a Run with its data, its settings, where it is now, and its Steps in the
// order they were recorded. Retry is the Task's retry policy when the Run was created,
// nil for the server default. Waiting is set while the Run waits in the queue or for a
// retry, Attempt while a worker holds it.
type RunDetail struct {
	RunSummary
	Input   []byte
	Output  []byte
	Retry   *RetryPolicy
	Waiting *RunWaiting
	Attempt *RunAttempt
	Steps   []StepDetail
}

// RunWaitReason is what a Run that is not running waits for.
type RunWaitReason string

// The reasons a Run waits.
const (
	// RunWaitWorker: the Run is ready and waits for a worker to claim it.
	RunWaitWorker RunWaitReason = "worker"
	// RunWaitRetry: the Run waits until it may try a failed Step again.
	RunWaitRetry RunWaitReason = "retry"
)

// RunWaiting is what a Run waits for. ReadyAt is when it became ready, or for
// RunWaitRetry, when it becomes ready. RetryStepID is the Step it tries again, and
// RetryCount the failed tries of that Step so far.
type RunWaiting struct {
	Reason          RunWaitReason
	ReadyAt         time.Time
	RetryStepID     *string
	RetryCount      int
	StartDeadlineAt *time.Time
}

// RunAttempt is the attempt a worker runs now, since StartedAt.
type RunAttempt struct {
	WorkerID  uuid.UUID
	StartedAt time.Time
}
