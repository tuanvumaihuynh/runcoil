// Package domain holds the vocabulary of Runcoil: Tasks, Runs, their statuses,
// and their IDs. It imports no infrastructure.
package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"
)

// RunStatus is the state of a Run. Its values are stored as text.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

// QueuedReason says why a queued Run is waiting. It is empty when the Run is not queued.
type QueuedReason string

const (
	QueuedReasonNone               QueuedReason = ""
	QueuedReasonWaitingForWorker   QueuedReason = "waiting_for_worker"
	QueuedReasonWaitingForCapacity QueuedReason = "waiting_for_capacity"
	QueuedReasonRetryBackoff       QueuedReason = "retry_backoff"
)

// RunSource says what created a Run.
type RunSource string

const (
	RunSourceInvoke   RunSource = "invoke"
	RunSourceSchedule RunSource = "schedule"
	RunSourceRerun    RunSource = "rerun"
)

// FailureCode says why a Run or a Step failed for good.
type FailureCode string

const (
	FailureCodeError              FailureCode = "ERROR"
	FailureCodePanic              FailureCode = "PANIC"
	FailureCodeStepFailed         FailureCode = "STEP_FAILED"
	FailureCodeAttemptLost        FailureCode = "ATTEMPT_LOST"
	FailureCodeOutputNotEncodable FailureCode = "OUTPUT_NOT_ENCODABLE"
	FailureCodeOutputTooLarge     FailureCode = "OUTPUT_TOO_LARGE"
	FailureCodeRunDataLimit       FailureCode = "RUN_DATA_LIMIT"
	FailureCodeWorkerWaitLimit    FailureCode = "WORKER_WAIT_LIMIT"
	FailureCodeStartDeadline      FailureCode = "START_DEADLINE"
)

// Failure is the final error of a Run.
type Failure struct {
	Code     FailureCode
	Message  string
	Attempts int
	StepID   string
}

// Run is one execution of a Task. A zero time or ID means the field is not set.
type Run struct {
	ID             string
	Task           TaskRef
	Status         RunStatus
	QueuedReason   QueuedReason
	Source         RunSource
	Attempts       int
	Input          []byte
	Output         []byte
	Failure        *Failure
	LeaseToken     string
	LeaseExpiresAt time.Time
	InstanceID     string
	AckedAt        time.Time
	DueAt          time.Time
	CreatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
	RerunOf        string
	// NextAttemptAt is the earliest start of the next attempt of a queued Run that was
	// retried or released. Zero when the Run is not waiting for a retry.
	NextAttemptAt time.Time
	// HandlerAttemptsUsed counts failed tries of handler code outside any Step.
	HandlerAttemptsUsed int
	// EndedToken is the lease token of the attempt that last ended with a retry or a release,
	// EndedKind is how it ended, and EndedAt is the retry time that was answered. They let a
	// repeated report get the same answer after the Run has been dispatched again.
	EndedToken string
	EndedKind  EndKind
	EndedAt    time.Time
	// RunningStepID and RunningStepAttempt name the Step that the current attempt is running,
	// as its last heartbeat said. Empty when the heartbeat named none.
	RunningStepID      string
	RunningStepAttempt int
	// RunningStepPolicy is the running Step's own retry policy, as its last heartbeat
	// named it. Nil when the Step has none, or the heartbeat named no Step.
	RunningStepPolicy *RetryPolicy
	// WorkerWaitSince is when the Run started waiting for a worker: the start of the current
	// spell of WAITING_FOR_WORKER. It is zero when the Run is not waiting for a worker.
	WorkerWaitSince time.Time
}

// EndKind is how an attempt ended with an answer that a repeated report must get back.
type EndKind string

const (
	// EndKindNone is no ended attempt.
	EndKindNone EndKind = ""
	// EndKindReleased is an attempt the worker gave back.
	EndKindReleased EndKind = "released"
	// EndKindFailed is an attempt whose handler error was retried.
	EndKindFailed EndKind = "failed"
)

// RunRef identifies a Run and its Task, for a log line about a Run a sweep changed.
type RunRef struct {
	ID   string
	Task TaskRef
}

// Claim is what the dispatcher gives a queued Run when it starts an attempt.
type Claim struct {
	InstanceID     string
	LeaseToken     string
	LeaseExpiresAt time.Time
	Now            time.Time
}

// StartDeadlineMessage is the text of a START_DEADLINE failure. The store writes it,
// in the same statement that holds the start-deadline rule.
const StartDeadlineMessage = "run did not start within its start deadline"

// WorkerWaitLimitMessage is the text of a WORKER_WAIT_LIMIT failure. The store writes it,
// in the same statement that holds the worker-wait rule.
const WorkerWaitLimitMessage = "no worker was ready within the worker wait limit"

// RunFilter selects Runs for a listing, newest first. An empty field does not filter.
// The creation window is half-open: created at or after CreatedAfter, and before
// CreatedBefore. The page starts after the position AfterCreatedAt and AfterID, which
// is the creation time and ID of the last Run of the previous page. Both zero means
// the first page. Limit is the number of Runs to return.
type RunFilter struct {
	WorkerID      string
	TaskID        string
	Statuses      []RunStatus
	CreatedAfter  time.Time
	CreatedBefore time.Time
	// AfterCreatedAt is the creation time of the position the page starts after.
	AfterCreatedAt time.Time
	// AfterID is the ID of the position the page starts after. Runs created at the same
	// time are ordered by ID.
	AfterID string
	Limit   int
}

// ErrRunNotFinished reports that a rerun names a Run that has not finished.
var ErrRunNotFinished = errors.New("run is not finished")

// ErrStepNotFound reports that a rerun names a Step that has no record in the Run.
var ErrStepNotFound = errors.New("step has no record")

// RerunPlan is what a rerun copies from the original Run.
type RerunPlan struct {
	// Copied are the finished Step records recorded before the selected Step, in
	// order, renumbered from 1 for the new Run. Empty for a rerun from the start.
	Copied []StepRecord
}

// MaxIdempotencyKeyLength is the longest idempotency key, in characters.
const MaxIdempotencyKeyLength = 255

// KeyRecord is an idempotency key, stored with the Run it created in one transaction.
type KeyRecord struct {
	Key         string
	Fingerprint string
	RunID       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// KeyHolder is the key that already exists: the Run it created and the fingerprint of its request.
type KeyHolder struct {
	RunID       string
	Fingerprint string
}

// KeyAction is the answer to a request that carries a key.
type KeyAction int

const (
	// KeyCreate means the key is new, so the request creates its Run.
	KeyCreate KeyAction = iota + 1
	// KeyDeduplicated means the key holds the same request, so the first Run is returned.
	KeyDeduplicated
	// KeyConflict means the key holds a different request.
	KeyConflict
)

// Limits are the size limits on user data, in bytes. A zero limit is not checked.
type Limits struct {
	// Input is the largest Run input.
	Input int
	// StepOutput is the largest output of one Step.
	StepOutput int
	// RunOutput is the largest output of a Run.
	RunOutput int
	// RunTotal is the largest total of a Run's input and recorded Step outputs.
	RunTotal int
}

// ErrNotFound reports that a stored record does not exist. Stores return it;
// the engine turns it into the API's not-found error.
var ErrNotFound = errors.New("not found")

// ErrStaleLease reports that a report names a lease that is no longer the Run's current one.
var ErrStaleLease = errors.New("lease is no longer current")

// ErrStepOrder reports a Step report that is out of order: the attempt number is
// not the next one, or the Step is already final.
var ErrStepOrder = errors.New("step report is out of order")

// Terminal reports whether the status will not change.
func (s RunStatus) Terminal() bool {
	return s == RunStatusSucceeded || s == RunStatusFailed
}

// PlanRerun decides a rerun of a finished Run. Only a finished Run can be rerun,
// succeeded or failed. Without fromStepID, nothing is copied. With it, the Step must
// have a final record, matched by its exact ID (a repeated Step carries its numbered
// form). A pending Step, one with failed tries and no final record, cannot be selected.
// The records copied are the finished ones whose position is lower than the selected
// Step's: a Step the Run never finished has no outcome to keep.
func PlanRerun(original Run, steps []StepRecord, fromStepID string) (RerunPlan, error) {
	if !original.Status.Terminal() {
		return RerunPlan{}, ErrRunNotFinished
	}

	if fromStepID == "" {
		return RerunPlan{}, nil
	}

	selected := FindStep(steps, fromStepID)
	if selected == nil || !selected.Finished() {
		return RerunPlan{}, ErrStepNotFound
	}

	var copied []StepRecord
	for _, step := range steps {
		if step.Position >= selected.Position || !step.Finished() {
			continue
		}

		step.Position = len(copied) + 1
		copied = append(copied, step)
	}

	return RerunPlan{Copied: copied}, nil
}

// Fingerprint identifies a request by its parts. Each part is length-prefixed, so
// that different splits of the same bytes give different fingerprints.
func Fingerprint(parts ...string) string {
	h := sha256.New()
	var n [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(n[:], uint64(len(part))) //nolint:gosec // A length is never negative.
		h.Write(n[:])
		h.Write([]byte(part))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// DecideKey decides a request from the key that is held for it. holder is nil when
// no unexpired key exists: an expired key counts as absent. The same key with the
// same request is deduplicated, whatever state its Run is in; the same key with a
// different request is a conflict that names the Run that holds the key.
func DecideKey(holder *KeyHolder, fingerprint string) KeyAction {
	switch {
	case holder == nil:
		return KeyCreate
	case holder.Fingerprint == fingerprint:
		return KeyDeduplicated
	default:
		return KeyConflict
	}
}

// InputTooLarge reports whether a Run input is over its limit.
func InputTooLarge(input []byte, l Limits) bool {
	return l.Input > 0 && len(input) > l.Input
}

// RunOutputTooLarge reports whether a Run output is over its limit.
func RunOutputTooLarge(output []byte, l Limits) bool {
	return l.RunOutput > 0 && len(output) > l.RunOutput
}

// CheckStepOutput returns the Failure to record instead of a succeeded Step output
// that is over a limit, or nil when the output may be stored. An output over its own
// limit fails with OUTPUT_TOO_LARGE. An output that would take the Run's total over
// its limit fails with RUN_DATA_LIMIT. The total counts the Run input and the outputs
// of the Steps that succeeded before, not the Run output.
func CheckStepOutput(run Run, steps []StepRecord, stepID string, attempt int, output []byte, l Limits) *Failure {
	if l.StepOutput > 0 && len(output) > l.StepOutput {
		return &Failure{
			Code:     FailureCodeOutputTooLarge,
			Message:  "step output is over its size limit",
			Attempts: attempt,
			StepID:   stepID,
		}
	}

	if l.RunTotal > 0 && storedBytes(run, steps)+len(output) > l.RunTotal {
		return &Failure{
			Code:     FailureCodeRunDataLimit,
			Message:  "run data is over its total size limit",
			Attempts: attempt,
			StepID:   stepID,
		}
	}

	return nil
}

// storedBytes is the size of the data that travels with each dispatch: the Run
// input, and every succeeded Step output.
func storedBytes(run Run, steps []StepRecord) int {
	total := len(run.Input)
	for _, step := range steps {
		if step.Status == StepStatusSucceeded {
			total += len(step.Output)
		}
	}

	return total
}
