package domain

import (
	"errors"
	"fmt"
	"strings"
	"uuid"
)

// MaxSlots is the most Runs one worker process may run at once.
const MaxSlots = 100

// MaxTasks is the most Tasks one registration may declare.
const MaxTasks = 1000

// Registration is what one worker process declares at Register.
type Registration struct {
	App   string
	Slots int
	Tasks []TaskDefinition
}

// RegistrationError is a registration the server refuses. TaskName is set when one Task
// is at fault.
type RegistrationError struct {
	Message  string
	TaskName string
}

func (e *RegistrationError) Error() string {
	return e.Message
}

// Validate checks what the request rules do not: the slots, the number of Tasks, that no
// name has a NUL byte, that each Task is declared once, and the limits of each Task's
// retry policy and start deadline.
func (r Registration) Validate() error {
	if r.Slots < 1 || r.Slots > MaxSlots {
		return &RegistrationError{Message: fmt.Sprintf("slots must be between 1 and %d", MaxSlots)}
	}

	if strings.ContainsRune(r.App, 0) {
		return &RegistrationError{Message: "app name must not contain a NUL byte"}
	}

	if len(r.Tasks) == 0 {
		return &RegistrationError{Message: "a registration needs at least one task"}
	}

	if len(r.Tasks) > MaxTasks {
		return &RegistrationError{Message: fmt.Sprintf("a registration declares at most %d tasks", MaxTasks)}
	}

	seen := make(map[string]struct{}, len(r.Tasks))
	for _, def := range r.Tasks {
		if strings.ContainsRune(def.Name, 0) {
			return &RegistrationError{Message: "task name must not contain a NUL byte", TaskName: def.Name}
		}

		if _, ok := seen[def.Name]; ok {
			return &RegistrationError{Message: "task is declared twice", TaskName: def.Name}
		}

		seen[def.Name] = struct{}{}

		if def.Retry != nil {
			if err := def.Retry.Validate(); err != nil {
				return &RegistrationError{Message: "retry policy: " + err.Error(), TaskName: def.Name}
			}
		}

		if def.StartDeadline != nil && *def.StartDeadline <= 0 {
			return &RegistrationError{Message: "start deadline must be greater than 0", TaskName: def.Name}
		}

		if def.Schedule != nil {
			if err := def.Schedule.Validate(); err != nil {
				return &RegistrationError{Message: "schedule: " + err.Error(), TaskName: def.Name}
			}
		}
	}

	return nil
}

// AsRegistrationError returns the RegistrationError in err's chain, if any.
func AsRegistrationError(err error) (*RegistrationError, bool) {
	var regErr *RegistrationError
	ok := errors.As(err, &regErr)

	return regErr, ok
}

// WorkerSession is a worker session to store at Register. TaskIDs holds a new ID per
// Task, used only for a Task stored for the first time.
type WorkerSession struct {
	ID           uuid.UUID
	TokenHash    []byte
	Registration Registration
	TaskIDs      []uuid.UUID
}

// NewWorkerSession checks the registration and gives the session and each Task a new ID.
func NewWorkerSession(reg Registration, tokenHash []byte) (WorkerSession, error) {
	if err := reg.Validate(); err != nil {
		return WorkerSession{}, fmt.Errorf("validate registration: %w", err)
	}

	taskIDs := make([]uuid.UUID, 0, len(reg.Tasks))
	for range reg.Tasks {
		taskIDs = append(taskIDs, uuid.NewV7())
	}

	return WorkerSession{ID: uuid.NewV7(), TokenHash: tokenHash, Registration: reg, TaskIDs: taskIDs}, nil
}

// Worker is a live worker session.
type Worker struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	App       string
	TaskIDs   []uuid.UUID
	Slots     int
}

// AttachStreamParams moves a worker to a new stream. Running is what the worker reports
// it is running. The leases it does not report count as lost, and a Run lost more than
// MaxLost times fails. Attached gets the reported attempts the worker no longer holds
// while the worker row is still locked, before the commit.
type AttachStreamParams struct {
	WorkerID     uuid.UUID
	DispatcherID uuid.UUID
	StreamID     uuid.UUID
	Running      []Lease
	MaxLost      int
	Attached     func(notHeld []Lease)
}

// AttachStreamResult is what a new stream changed. NotHeld are the reported attempts the
// worker no longer holds. Requeued and Failed count the leases it did not report: back in
// the queue, or failed with FailureCodeAttemptLost.
type AttachStreamResult struct {
	NotHeld  []Lease
	Requeued int
	Failed   int
}

// ClaimRunsParams claims up to Slots queued Runs of TaskIDs for the worker on the stream.
type ClaimRunsParams struct {
	WorkerID     uuid.UUID
	DispatcherID uuid.UUID
	StreamID     uuid.UUID
	TaskIDs      []uuid.UUID
	Slots        int
}

// DeadWorkers is what one sweep of the reaper did: the sessions it ended, and what became
// of the attempts they held.
type DeadWorkers struct {
	Ended    int
	Requeued int
	// Failed counts the Runs lost more often than the cap, which failed with
	// FailureCodeAttemptLost.
	Failed int
}
