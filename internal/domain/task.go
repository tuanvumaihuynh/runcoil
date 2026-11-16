package domain

import (
	"errors"
	"fmt"
	"time"
	"uuid"
)

// MaxRetryAttempts is the most attempts a retry policy may ask for, the first one
// included.
const MaxRetryAttempts = 30

// MaxRetryDelays is the most delays a retry policy may list.
const MaxRetryDelays = 32

// TaskRef names a Task.
type TaskRef struct {
	ID   uuid.UUID
	Name string
}

// TaskDefinition is one Task a worker declares. A nil field is unset.
type TaskDefinition struct {
	Name          string
	Retry         *RetryPolicy
	StartDeadline *time.Duration
	// Schedule makes a Run at each tick of its cron. Nil: the Task has no schedule.
	Schedule *Schedule
}

// RetryPolicy says how often and how late a failed Step is tried again.
type RetryPolicy struct {
	// MaxAttempts counts the first attempt. Zero is the server default.
	MaxAttempts int
	// Delays between tries; the last one repeats.
	Delays []time.Duration
}

// Validate checks the limits of a policy: at most MaxRetryAttempts attempts and
// MaxRetryDelays delays, none negative.
func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 0 || p.MaxAttempts > MaxRetryAttempts {
		return fmt.Errorf("max attempts must be between 0 and %d", MaxRetryAttempts)
	}

	if len(p.Delays) > MaxRetryDelays {
		return fmt.Errorf("at most %d delays", MaxRetryDelays)
	}

	for _, d := range p.Delays {
		if d < 0 {
			return errors.New("delays must not be negative")
		}
	}

	return nil
}

// Or returns p with each unset field taken from fallback: a zero MaxAttempts and empty
// Delays are unset.
func (p RetryPolicy) Or(fallback RetryPolicy) RetryPolicy {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = fallback.MaxAttempts
	}

	if len(p.Delays) == 0 {
		p.Delays = fallback.Delays
	}

	return p
}

// Delay returns the wait after the try number tries failed, the first try being 1, and
// false when that try was the last one. The last delay repeats; no delay means none.
func (p RetryPolicy) Delay(tries int) (time.Duration, bool) {
	if tries >= p.MaxAttempts {
		return 0, false
	}

	if len(p.Delays) == 0 {
		return 0, true
	}

	return p.Delays[min(tries, len(p.Delays))-1], true
}

// TaskSchedule is a Task's schedule as the dashboard shows it. PausedAt is set while it
// is paused.
type TaskSchedule struct {
	Cron       string
	Overlap    OverlapPolicy
	NextTickAt time.Time
	PausedAt   *time.Time
}

// LastRun is the newest Run of a Task.
type LastRun struct {
	ID        uuid.UUID
	Status    RunStatus
	CreatedAt time.Time
}

// TaskSummary is a Task as a list shows it. Schedule and LastRun are nil when it has
// none.
type TaskSummary struct {
	ID               uuid.UUID
	App              string
	Name             string
	Schedule         *TaskSchedule
	LastRun          *LastRun
	LiveWorkerCount  int
	LastRegisteredAt time.Time
}

// TaskDetail is a Task with its settings and the live workers that serve it, newest
// first. Retry and StartDeadline are nil when the Task does not set them.
type TaskDetail struct {
	TaskSummary
	Retry         *RetryPolicy
	StartDeadline *time.Duration
	Workers       []TaskWorker
	CreatedAt     time.Time
}

// TaskWorker is a live worker that serves a Task.
type TaskWorker struct {
	ID          uuid.UUID
	Slots       int
	ConnectedAt time.Time
	HeartbeatAt time.Time
}
