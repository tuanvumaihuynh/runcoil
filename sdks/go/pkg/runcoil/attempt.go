package runcoil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/sdks/go/internal/runstate"
)

// errAttemptEnded is what callHandler returns when the attempt ended while the handler
// ran. Nothing is reported for such an attempt.
var errAttemptEnded = errors.New("attempt ended")

// attempt is one Run attempt that this worker holds, from its Assign until its result is
// reported or it ends.
type attempt struct {
	runID string
	epoch int64

	// logger carries the run_id, task_name and epoch of the attempt.
	logger *slog.Logger

	// ctx is the handler's context. It is cancelled when the attempt ends.
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.Mutex
	// claimed is set by the one caller that sends the result or ends the attempt.
	claimed bool
	// ended is set once the attempt is no longer this worker's.
	ended bool
}

// claim returns true for the first caller only. That caller sends the attempt's result;
// every later caller must send nothing.
func (a *attempt) claim() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.claimed {
		return false
	}

	a.claimed = true

	return true
}

// open reports whether the attempt is still this worker's. An attempt whose result is
// being sent is open: Listen still reports it, so the server keeps its lease.
func (a *attempt) open() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return !a.ended
}

// end makes the attempt no longer this worker's. Its result would be refused, so none is
// sent, and its handler's context is cancelled. It reports whether this call ended an
// attempt whose result was not claimed yet.
func (a *attempt) end() bool {
	a.mu.Lock()
	unclaimed := !a.claimed
	a.claimed = true
	a.ended = true
	a.mu.Unlock()

	a.cancel()

	return unclaimed
}

func (a *attempt) lease() *runcoilv1.Lease {
	return &runcoilv1.Lease{RunId: a.runID, Epoch: a.epoch}
}

// panicError is a panic that the worker recovered from a handler.
type panicError struct {
	value any
	stack []byte
}

func (e *panicError) Error() string { return fmt.Sprintf("handler panicked: %v", e.value) }

// execute runs the handler of an attempt and reports its result. It runs in its own
// goroutine, one per attempt.
func (w *Worker) execute(a *attempt, t *task, assign *runcoilv1.Assign) {
	defer w.running.Done()
	defer w.forget(a)
	defer a.cancel()

	if t == nil {
		a.logger.WarnContext(a.ctx, "task not registered by this worker; the run fails")
		w.report(a, runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR,
			fmt.Sprintf("task %q is not registered by this worker", assign.GetTaskName()), ""))

		return
	}

	if !w.takeSlot(a) {
		return
	}
	defer func() { <-w.slots }()

	// The slot and the end of the attempt may be ready together, and select picks either.
	if !a.open() {
		return
	}

	a.logger.DebugContext(a.ctx, "run started", slog.Int("replayed_steps", len(assign.GetSteps())))
	startedAt := time.Now()

	out, err := callHandler(a.ctx, t.handler, assign.GetInput())
	if errors.Is(err, errAttemptEnded) {
		a.logger.DebugContext(a.ctx, "attempt ended while its handler ran; no report")
		return
	}

	var pe *panicError
	if errors.As(err, &pe) {
		a.logger.ErrorContext(a.ctx, "handler panicked",
			slog.Any("panic", pe.value),
			slog.String("stack", string(pe.stack)),
		)
	}

	req := runResultToProto(out, err)
	if !w.report(a, req) {
		return
	}

	attrs := []slog.Attr{
		slog.String("status", "succeeded"),
		slog.Duration("duration", time.Since(startedAt)),
	}
	if runErr := req.GetError(); runErr != nil {
		attrs[0] = slog.String("status", "failed")
		attrs = append(attrs, slog.String("error_kind", errorKindFromProto(runErr.GetKind())))
	}

	a.logger.LogAttrs(a.ctx, slog.LevelDebug, "run finished", attrs...)
}

// takeSlot waits for one of the worker's handler slots. It returns false when the attempt
// ends first.
func (w *Worker) takeSlot(a *attempt) bool {
	select {
	case w.slots <- struct{}{}:
		return true
	default:
	}

	a.logger.DebugContext(a.ctx, "run waiting for a slot")

	select {
	case w.slots <- struct{}{}:
		return true
	case <-a.ctx.Done():
		return false
	}
}

// report sends the result of an attempt unless the attempt has ended, and reports
// whether the server took it. A result the server refuses for good, such as an output
// over its limit, is replaced by the failure of the Run, so the Run ends and does not
// run again.
func (w *Worker) report(a *attempt, req *runcoilv1.CompleteRunRequest) bool {
	if !a.claim() {
		a.logger.InfoContext(a.ctx, "attempt ended before its result was reported")
		return false
	}

	req.Lease = a.lease()

	// The attempt is claimed, so its context no longer ends it: the report runs until it
	// is sent, or until Shutdown cuts it short.
	ctx := w.reportCtx
	session := w.sessionToken()
	err := w.sendResult(ctx, a, req)
	if err == nil {
		return true
	}

	switch {
	case errorCode(err) == runcoilv1.ErrorCode_ERROR_CODE_STALE_LEASE:
		a.logger.InfoContext(ctx, "run result refused: lease is stale")
	case w.endSession(ctx, session, err):
		a.logger.WarnContext(ctx, "run result refused: worker session ended")
	case !a.open():
		a.logger.InfoContext(ctx, "run result refused after the attempt ended", slog.Any("error", err))
	default:
		a.logger.ErrorContext(ctx, "report run result failed", slog.Any("error", err))
	}

	return false
}

// completeRun sends the result of an attempt, and sends it again while the failure is
// retryable.
func (w *Worker) completeRun(ctx context.Context, req *runcoilv1.CompleteRunRequest) error {
	return w.retryCall(ctx, w.reportBudget(), func(ctx context.Context) error {
		if _, err := w.rpc.CompleteRun(ctx, req); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}

		return nil
	})
}

// sendResult sends the result of the attempt. completeRun already sends it again after a
// transient error. A result the server refuses for good would be refused on every try and
// leave the Run running, so the Run's failure is sent in its place, once.
func (w *Worker) sendResult(ctx context.Context, a *attempt, req *runcoilv1.CompleteRunRequest) error {
	err := w.completeRun(ctx, req)
	if err == nil || !refused(err) || !a.open() {
		return err
	}

	a.logger.WarnContext(ctx, "run result refused; reporting the failure instead", slog.Any("error", err))

	return w.completeRun(ctx, refusedRunResult(req, err))
}

// refusedRunResult is the failure that replaces a Run result the server refused: an
// output over the limit fails the Run with OUTPUT_TOO_LARGE, anything else with an error.
func refusedRunResult(req *runcoilv1.CompleteRunRequest, err error) *runcoilv1.CompleteRunRequest {
	failure := runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR, "the server refused the run result: "+refusal(err), "")
	if out := req.GetOutput(); out != nil && outputTooLarge(err) {
		failure = runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE, tooLargeMessage("run", len(out), err), "")
	}

	failure.Lease = req.GetLease()

	return failure
}

// forget drops the attempt from the held set, so Listen stops reporting it.
func (w *Worker) forget(a *attempt) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.attempts[a.runID] == a {
		delete(w.attempts, a.runID)
	}
}

// callHandler runs the handler and turns a panic into an error. The panic that ends an
// attempt becomes errAttemptEnded.
func callHandler(ctx context.Context, h Handler, input []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			if runstate.IsUnwind(r) {
				out, err = nil, errAttemptEnded
				return
			}

			out, err = nil, &panicError{value: r, stack: debug.Stack()}
		}
	}()

	return h.call(ctx, input)
}

// recordStep sends the outcome of one Step of the attempt and returns the stored record.
// An output the server refuses as too large is sent again as the Step's failure. It
// returns false when the attempt ended, or when the report could not be made: the
// attempt is then ended here, and the handler must stop. A report the server refuses for
// good fails the Run first, so the Run does not run again.
func (w *Worker) recordStep(a *attempt, r runstate.StepReport) (runstate.Record, bool) {
	logger := a.logger.With(slog.String("step_id", r.StepID))

	session := w.sessionToken()
	resp, err := w.completeStep(a, r)
	if err != nil && r.Failure == nil && outputTooLarge(err) {
		logger.WarnContext(a.ctx, "step output refused as too large; the step fails",
			slog.Int("bytes", len(r.Output)),
			slog.Any("error", err),
		)

		r.Failure = &runstate.Failure{
			Kind:         runstate.KindOutputTooLarge,
			Message:      tooLargeMessage("step", len(r.Output), err),
			NonRetriable: true,
		}
		r.Output = nil
		resp, err = w.completeStep(a, r)
	}

	if err != nil {
		switch {
		case errorCode(err) == runcoilv1.ErrorCode_ERROR_CODE_STALE_LEASE:
			logger.InfoContext(a.ctx, "step report refused: lease is stale")
		case w.endSession(a.ctx, session, err):
			logger.WarnContext(a.ctx, "step report refused: worker session ended; attempt ended")
		case a.ctx.Err() != nil:
			// The attempt ended while the report was sent; there is nothing to fail.
		case refused(err):
			logger.ErrorContext(a.ctx, "step report refused; the run fails", slog.Any("error", err))
			w.report(a, runFailureToProto(runcoilv1.ErrorKind_ERROR_KIND_ERROR,
				fmt.Sprintf("the server refused the report of step %q: %s", r.StepID, refusal(err)), ""))
		default:
			logger.ErrorContext(a.ctx, "step report failed; attempt ended", slog.Any("error", err))
		}

		a.end()

		return runstate.Record{}, false
	}

	if retry := resp.GetRetryScheduled(); retry != nil {
		message := ""
		if r.Failure != nil {
			message = r.Failure.Message
		}

		logger.InfoContext(a.ctx, "step failed; retry scheduled",
			slog.String("error", message),
			slog.Int("try_count", int(retry.GetTryCount())),
			slog.Time("next_attempt_at", retry.GetNextAttemptAt().AsTime()),
		)
		a.end()

		return runstate.Record{}, false
	}

	rec := resp.GetRecord()
	if rec == nil {
		logger.ErrorContext(a.ctx, "step report answered without a record; attempt ended")
		a.end()

		return runstate.Record{}, false
	}

	return stepRecordFromProto(rec), true
}

// completeStep sends the outcome of one Step, and sends it again while the failure is
// retryable.
func (w *Worker) completeStep(a *attempt, r runstate.StepReport) (*runcoilv1.CompleteStepResponse, error) {
	req := stepReportToProto(a.lease(), r)

	var resp *runcoilv1.CompleteStepResponse
	err := w.retryCall(a.ctx, w.reportBudget(), func(ctx context.Context) error {
		var err error
		if resp, err = w.rpc.CompleteStep(ctx, req); err != nil {
			return fmt.Errorf("complete step: %w", err)
		}

		return nil
	})

	return resp, err
}
