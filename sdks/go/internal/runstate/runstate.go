package runstate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// The kinds of a failure, as the server names them.
const (
	KindError              = "error"
	KindPanic              = "panic"
	KindOutputNotEncodable = "output_not_encodable"
	KindOutputTooLarge     = "output_too_large"
)

// State describes one attempt of one Run. The worker builds it from the Assign it
// received and puts it in the context it passes to the handler.
type State struct {
	RunID    string
	App      string
	TaskName string
	Logger   *slog.Logger

	// Open reports whether the attempt still belongs to this worker. It turns false once
	// the server has taken the attempt back, or the worker has given it up.
	Open func() bool

	// Report sends the outcome of one Step to the server and waits for its answer. It
	// returns the stored record and true. It returns false when the attempt ended, could
	// not report, or the server scheduled a retry of the Step: the worker has then ended
	// the attempt, and the caller must Unwind.
	Report func(ctx context.Context, r StepReport) (Record, bool)

	mu       sync.Mutex
	recorded map[string]Record
	used     map[string]struct{}
}

// NewState returns the state of an attempt whose Assign carried recorded.
func NewState(recorded map[string]Record) *State {
	if recorded == nil {
		recorded = map[string]Record{}
	}

	return &State{recorded: recorded, used: map[string]struct{}{}}
}

// Record is the stored outcome of one Step: an output, or a failure.
type Record struct {
	Succeeded bool
	// Output is JSON, when Succeeded.
	Output  []byte
	Kind    string
	Message string
}

// StepReport is the outcome of one Step, as the SDK sends it.
type StepReport struct {
	StepID  string
	Output  []byte
	Failure *Failure
	// Retry is the Step's own retry policy. Nil: the Task's.
	Retry     *RetryPolicy
	StartedAt time.Time
	EndedAt   time.Time
}

// Failure is why a Step or a handler failed: its kind and its text.
type Failure struct {
	Kind    string
	Message string
	// NonRetriable asks the server not to try the Step again.
	NonRetriable bool
	// RetryAfter replaces the policy's delay before the next try.
	RetryAfter *time.Duration
}

// RetryPolicy says how often and how late a failed Step is tried again. A zero field is
// the server's default.
type RetryPolicy struct {
	MaxAttempts int
	Delays      []time.Duration
}

// NonRetriableError is an error the application marked with retry.NonRetriable.
type NonRetriableError struct {
	Err error
}

func (e *NonRetriableError) Error() string { return e.Err.Error() }
func (e *NonRetriableError) Unwrap() error { return e.Err }

// RetryAfterError is an error the application marked with retry.After.
type RetryAfterError struct {
	Err   error
	After time.Duration
}

func (e *RetryAfterError) Error() string { return e.Err.Error() }
func (e *RetryAfterError) Unwrap() error { return e.Err }

// MarkRetry adds to f what the marks on err say about retrying.
func (f *Failure) MarkRetry(err error) {
	var nr *NonRetriableError
	f.NonRetriable = errors.As(err, &nr)

	var ra *RetryAfterError
	if errors.As(err, &ra) {
		after := ra.After
		f.RetryAfter = &after
	}
}

// StepID returns the id a Step named name is stored under in this attempt: the name the
// first time, then the name with a counter, adding counters until the id is unused. So
// two Steps never share an id, and a replay that calls the Steps in the same order makes
// the same ids again.
func (s *State) StepID(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := name
	for n := 2; ; n++ {
		if _, used := s.used[id]; !used {
			break
		}

		id = fmt.Sprintf("%s#%d", name, n)
	}

	s.used[id] = struct{}{}

	return id
}

// Lookup returns the record of a Step stored by an earlier attempt or by this one.
func (s *State) Lookup(stepID string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.recorded[stepID]

	return r, ok
}

// Store keeps the record of a Step that finished in this attempt.
func (s *State) Store(stepID string, r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recorded[stepID] = r
}

// StepError is the final error of a Step that failed. It is rebuilt from the stored text,
// so it never matches the application's own error values.
type StepError struct {
	StepID  string
	Kind    string
	Message string
}

// Error returns the recorded message of the Step's error.
func (e *StepError) Error() string { return e.Message }

// unwind is the panic value that stops a handler whose attempt has ended.
type unwind struct{}

// Unwind stops the handler of an attempt that has ended. Only the SDK panics with it.
// Code that recovers panics must re-panic it, as IsUnwind reports.
func Unwind() {
	panic(unwind{})
}

// IsUnwind reports whether a recovered panic value is the one Unwind raises.
func IsUnwind(v any) bool {
	_, ok := v.(unwind)

	return ok
}

type key struct{}

// With returns a copy of ctx that carries s.
func With(ctx context.Context, s *State) context.Context {
	return context.WithValue(ctx, key{}, s)
}

// From returns the state carried by ctx, and false when ctx did not come from a worker's
// Assign.
func From(ctx context.Context) (*State, bool) {
	s, ok := ctx.Value(key{}).(*State)

	return s, ok && s != nil
}
