package runcoil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"

	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
)

const (
	retryStart = 250 * time.Millisecond
	retryCap   = 5 * time.Second
)

// reportBudget bounds the tries of one report: as long as the server keeps a silent
// worker's session.
func (w *Worker) reportBudget() time.Duration {
	return silentBeats * w.heartbeatInterval()
}

// retryCall runs op, and runs it again on a retryable failure: a transport failure, a
// server outage, or an internal server error. It stops once the next wait would pass
// budget, and as soon as parent ends. Each try has its own deadline. A failure that
// carries a Runcoil error code, such as STALE_LEASE, is never retried.
func (w *Worker) retryCall(parent context.Context, budget time.Duration, op func(ctx context.Context) error) error {
	deadline := time.Now().Add(budget)
	backoff := retryStart
	for try := 1; ; try++ {
		ctx, cancel := context.WithTimeout(parent, w.callTimeout())
		err := op(ctx)
		cancel()

		if err == nil {
			return nil
		}

		if !retryable(err) || parent.Err() != nil {
			return fmt.Errorf("try %d: %w", try, err)
		}

		if time.Now().Add(backoff).After(deadline) {
			return fmt.Errorf("gave up after %d tries: %w", try, err)
		}

		w.logger.DebugContext(parent, "call failed; retrying",
			slog.Int("try", try),
			slog.Duration("next_delay", backoff),
			slog.Any("error", err),
		)

		if !sleep(parent, backoff) {
			return fmt.Errorf("gave up after %d tries: %w", try, err)
		}

		backoff = min(2*backoff, retryCap)
	}
}

// retryable reports whether a failed call may succeed when it is sent again.
func retryable(err error) bool {
	if errorCode(err) != runcoilv1.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return false
	}

	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeUnknown, connect.CodeInternal:
		return true
	default:
		return false
	}
}

// refused reports whether the server refused a report for good: the same report would be
// refused again. A stale lease and an ended session are not refusals; they end the
// attempt.
func refused(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument, connect.CodeResourceExhausted:
		return true
	default:
		return false
	}
}

// outputTooLarge reports whether the server refused a report for the size of its output:
// over the server's limit, or over the largest message it reads.
func outputTooLarge(err error) bool {
	return errorCode(err) == runcoilv1.ErrorCode_ERROR_CODE_OUTPUT_TOO_LARGE ||
		connect.CodeOf(err) == connect.CodeResourceExhausted
}

// tooLargeMessage is the failure text of an output the server refused for its size. The
// server's own text already names the size and the limit; a refusal for the message size
// does not.
func tooLargeMessage(of string, size int, err error) string {
	if errorCode(err) == runcoilv1.ErrorCode_ERROR_CODE_OUTPUT_TOO_LARGE {
		return refusal(err)
	}

	return fmt.Sprintf("%s output of %d bytes is too large: %s", of, size, refusal(err))
}

// refusal returns the server's text of a refused call, without the SDK's wrapping.
func refusal(err error) string {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Message()
	}

	return err.Error()
}

// errorCode returns the Runcoil error code in the details of err, or
// ERROR_CODE_UNSPECIFIED when there is none.
func errorCode(err error) runcoilv1.ErrorCode {
	return errorDetail(err).GetCode()
}

// errorDetail returns the server's ErrorDetail carried by err, or nil.
func errorDetail(err error) *runcoilv1.ErrorDetail {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return nil
	}

	for _, d := range ce.Details() {
		msg, err := connectproto.UnmarshalErrorDetail(d)
		if err != nil {
			continue
		}

		if detail, ok := msg.(*runcoilv1.ErrorDetail); ok {
			return detail
		}
	}

	return nil
}

// sleep waits for d and reports false when ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
