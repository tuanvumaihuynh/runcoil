package step

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/sdks/go/internal/runstate"
	"github.com/runcoil/runcoil/sdks/go/pkg/retry"
)

// fakeRun is a run state whose reports are stored as the server would store them.
type fakeRun struct {
	st      *runstate.State
	reports []runstate.StepReport
	open    bool
	refuse  bool
}

func newFakeRun(recorded map[string]runstate.Record) (*fakeRun, context.Context) {
	f := &fakeRun{st: runstate.NewState(recorded), open: true}
	f.st.RunID = "run-1"
	f.st.Logger = slog.New(slog.DiscardHandler)
	f.st.Open = func() bool { return f.open }
	f.st.Report = func(_ context.Context, r runstate.StepReport) (runstate.Record, bool) {
		f.reports = append(f.reports, r)
		if f.refuse {
			f.open = false
			return runstate.Record{}, false
		}

		if r.Failure != nil {
			return runstate.Record{Kind: r.Failure.Kind, Message: r.Failure.Message}, true
		}

		return runstate.Record{Succeeded: true, Output: r.Output}, true
	}

	return f, runstate.With(context.Background(), f.st)
}

// unwound reports whether fn ended the attempt by unwinding.
func unwound(fn func()) (ok bool) {
	defer func() {
		ok = runstate.IsUnwind(recover())
	}()

	fn()

	return false
}

func TestDoOutsideARunCallsFn(t *testing.T) {
	want := errors.New("plain")

	got, err := Do(context.Background(), "charge", func(context.Context) (int, error) { return 7, want })

	assert.Equal(t, 7, got)
	assert.Same(t, want, err)
}

func TestDoReportsAndReturnsTheStoredValue(t *testing.T) {
	f, ctx := newFakeRun(nil)

	got, err := Do(ctx, "charge", func(context.Context) (map[string]int, error) { return map[string]int{"amount": 3}, nil })
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"amount": 3}, got)
	require.Len(t, f.reports, 1)
	assert.Equal(t, "charge", f.reports[0].StepID)
	assert.JSONEq(t, `{"amount":3}`, string(f.reports[0].Output))
	assert.False(t, f.reports[0].StartedAt.IsZero())
}

func TestDoReplaysARecord(t *testing.T) {
	f, ctx := newFakeRun(map[string]runstate.Record{
		"charge":   {Succeeded: true, Output: []byte(`"stored"`)},
		"charge#2": {Kind: runstate.KindError, Message: "declined"},
	})

	got, err := Do(ctx, "charge", func(context.Context) (string, error) { return "new", nil })
	require.NoError(t, err)
	assert.Equal(t, "stored", got)

	_, err = Do(ctx, "charge", func(context.Context) (string, error) { return "new", nil })

	var stepErr *Error
	require.ErrorAs(t, err, &stepErr)
	assert.Equal(t, &Error{StepID: "charge#2", Kind: runstate.KindError, Message: "declined"}, stepErr)
	assert.Empty(t, f.reports, "a stored Step runs no function and sends no report")
}

func TestDoReportsAFailure(t *testing.T) {
	tests := []struct {
		name    string
		fn      func(context.Context) (func(), error)
		kind    string
		message string
	}{
		{
			name:    "an error",
			fn:      func(context.Context) (func(), error) { return nil, errors.New("declined") },
			kind:    runstate.KindError,
			message: "declined",
		},
		{
			name:    "a panic",
			fn:      func(context.Context) (func(), error) { panic("boom") },
			kind:    runstate.KindPanic,
			message: "step panicked: boom",
		},
		{
			name:    "an output that is not JSON",
			fn:      func(context.Context) (func(), error) { return func() {}, nil },
			kind:    runstate.KindOutputNotEncodable,
			message: "encode output: json: unsupported type: func()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ctx := newFakeRun(nil)

			_, err := Do(ctx, "charge", tt.fn)

			var stepErr *Error
			require.ErrorAs(t, err, &stepErr)
			assert.Equal(t, tt.kind, stepErr.Kind)
			assert.Equal(t, tt.message, stepErr.Message)

			require.Len(t, f.reports, 1)
			assert.Equal(t, &runstate.Failure{Kind: tt.kind, Message: tt.message}, f.reports[0].Failure)
		})
	}
}

func TestDoUnwindsWhenTheAttemptEnded(t *testing.T) {
	t.Run("before the Step", func(t *testing.T) {
		f, ctx := newFakeRun(nil)
		f.open = false

		called := false
		assert.True(t, unwound(func() {
			_, _ = Do(ctx, "charge", func(context.Context) (int, error) { called = true; return 1, nil })
		}))
		assert.False(t, called)
	})

	t.Run("while the Step ran", func(t *testing.T) {
		f, ctx := newFakeRun(nil)

		assert.True(t, unwound(func() {
			_, _ = Do(ctx, "charge", func(context.Context) (int, error) { f.open = false; return 1, nil })
		}))
		assert.Empty(t, f.reports)
	})

	t.Run("when the report is refused", func(t *testing.T) {
		f, ctx := newFakeRun(nil)
		f.refuse = true

		assert.True(t, unwound(func() {
			_, _ = Do(ctx, "charge", func(context.Context) (int, error) { return 1, nil })
		}))
	})
}

func TestDoNeedsAnID(t *testing.T) {
	_, ctx := newFakeRun(nil)

	_, err := Do(ctx, "", func(context.Context) (int, error) { return 1, nil })

	assert.EqualError(t, err, "step: id is required")
}

func TestDoUnwindsWhenTheAttemptEndsWhileItReports(t *testing.T) {
	f, ctx := newFakeRun(nil)
	report := f.st.Report
	f.st.Report = func(ctx context.Context, r runstate.StepReport) (runstate.Record, bool) {
		rec, ok := report(ctx, r)
		f.open = false

		return rec, ok
	}

	assert.True(t, unwound(func() {
		_, _ = Do(ctx, "charge", func(context.Context) (int, error) { return 1, nil })
	}), "code after the Step does not run once the attempt ended")
}

func TestDoMarksTheRetryOfAFailure(t *testing.T) {
	base := errors.New("rate limited")

	tests := []struct {
		name         string
		err          error
		nonRetriable bool
		retryAfter   *time.Duration
	}{
		{name: "a plain error", err: base},
		{name: "non retriable", err: retry.NonRetriable(base), nonRetriable: true},
		{name: "retry after", err: retry.After(base, time.Minute), retryAfter: ptr(time.Minute)},
		{name: "wrapped retry after", err: fmt.Errorf("charge: %w", retry.After(base, time.Second)), retryAfter: ptr(time.Second)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ctx := newFakeRun(nil)

			_, err := Do(ctx, "charge", func(context.Context) (int, error) { return 0, tt.err })
			require.Error(t, err)

			require.Len(t, f.reports, 1)
			failure := f.reports[0].Failure
			assert.Equal(t, tt.err.Error(), failure.Message)
			assert.Equal(t, tt.nonRetriable, failure.NonRetriable)
			assert.Equal(t, tt.retryAfter, failure.RetryAfter)
		})
	}
}

func TestDoSendsTheStepRetryPolicy(t *testing.T) {
	f, ctx := newFakeRun(nil)

	_, err := Do(ctx, "charge", func(context.Context) (int, error) { return 1, nil },
		Retry(retry.Policy{MaxAttempts: 2, Delays: []time.Duration{time.Second}}))
	require.NoError(t, err)

	require.Len(t, f.reports, 1)
	assert.Equal(t, &runstate.RetryPolicy{MaxAttempts: 2, Delays: []time.Duration{time.Second}}, f.reports[0].Retry)
}

func TestDoRefusesAnInvalidRetryPolicy(t *testing.T) {
	f, ctx := newFakeRun(nil)

	called := false
	_, err := Do(ctx, "charge", func(context.Context) (int, error) { called = true; return 1, nil },
		Retry(retry.Policy{MaxAttempts: 31}))

	assert.EqualError(t, err, `step "charge": retry policy: max attempts must be between 0 and 30, got 31`)
	assert.False(t, called)
	assert.Empty(t, f.reports)
}

func ptr[T any](v T) *T { return &v }

func TestDoRefusesAnIDTheServerCannotStore(t *testing.T) {
	f, ctx := newFakeRun(nil)
	calls := 0
	fn := func(context.Context) (int, error) {
		calls++
		return 1, nil
	}

	_, err := Do(ctx, "bad\x00id", fn)
	assert.EqualError(t, err, "step: id must not contain a NUL byte")

	_, err = Do(ctx, "bad\xffid", fn)
	assert.EqualError(t, err, "step: id must be valid UTF-8")

	_, err = Do(ctx, strings.Repeat("x", maxIDBytes+1), fn)
	assert.EqualError(t, err, "step: id must be at most 255 bytes, counter included, not 256")

	assert.Zero(t, calls, "fn is not called")
	assert.Empty(t, f.reports, "nothing is reported")
}

func TestDoCountsTheCounterInTheIDLimit(t *testing.T) {
	f, ctx := newFakeRun(nil)
	id := strings.Repeat("x", maxIDBytes)
	fn := func(context.Context) (int, error) { return 1, nil }

	_, err := Do(ctx, id, fn)
	require.NoError(t, err)

	_, err = Do(ctx, id, fn)
	assert.EqualError(t, err, "step: id must be at most 255 bytes, counter included, not 257")
	assert.Len(t, f.reports, 1)
}
