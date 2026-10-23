package domain

import "time"

// Task is a Task as a worker registered it.
type Task struct {
	WorkerID      string
	TaskID        string
	Schedule      *Schedule
	Retry         *RetryPolicy
	StartDeadline time.Duration
	// RegisteredAt is when the Task was last registered.
	RegisteredAt time.Time
	// NextRunAt is the next tick of the Task's schedule. Zero when it has none.
	NextRunAt time.Time
}

// ScheduleKind says which field of a Schedule is set.
type ScheduleKind string

const (
	ScheduleKindCron  ScheduleKind = "cron"
	ScheduleKindEvery ScheduleKind = "every"
	ScheduleKindDaily ScheduleKind = "daily"
)

// Schedule says when a Task runs by itself. Only the field named by Kind is set.
type Schedule struct {
	Kind        ScheduleKind
	Cron        string
	Every       time.Duration
	Daily       []TimeOfDay
	SkipOverlap bool
}

// TimeOfDay is a clock time in UTC.
type TimeOfDay struct {
	Hour   int
	Minute int
}

// RetryPolicy says how many attempts a Task gets and the delay before each retry.
type RetryPolicy struct {
	MaxAttempts int
	Delays      []time.Duration
}

// Registration is a worker registration that has passed validation.
type Registration struct {
	WorkerID   string
	InstanceID string
	Slots      int
	Tasks      []Task
	// SDKName and SDKVersion say which SDK the worker runs. They are for the logs only.
	SDKName    string
	SDKVersion string
}
