package domain

import (
	"slices"
	"time"
)

// RetryConfig is the server's default retry policy and the jitter applied to
// its delays. Delays from a policy also vary by the same jitter.
type RetryConfig struct {
	MaxAttempts int
	Delays      []time.Duration
	// Jitter is the fraction by which a delay may vary either way, from 0 to below 1.
	Jitter float64
}

// ResolveRetry picks the policy for one failure, field by field. The attempt limit
// is the Step's if it is above zero, else the Task's if above zero, else the server
// default. The delays are the Step's if it has any, else the Task's if it has any,
// else the server default. A failure outside any Step passes a nil Step policy.
func ResolveRetry(stepPolicy, taskPolicy *RetryPolicy, cfg RetryConfig) RetryPolicy {
	out := RetryPolicy{MaxAttempts: cfg.MaxAttempts, Delays: slices.Clone(cfg.Delays)}
	for _, p := range []*RetryPolicy{taskPolicy, stepPolicy} {
		if p == nil {
			continue
		}

		if p.MaxAttempts > 0 {
			out.MaxAttempts = p.MaxAttempts
		}

		if len(p.Delays) > 0 {
			out.Delays = slices.Clone(p.Delays)
		}
	}

	return out
}

// Exhausted reports whether a try that failed as attempt number attempt was the last one.
func (p RetryPolicy) Exhausted(attempt int) bool {
	return attempt >= p.MaxAttempts
}

// NeverRetried reports whether a failure code ends the Step or the Run at once.
func NeverRetried(code FailureCode) bool {
	return code == FailureCodePanic || code == FailureCodeOutputNotEncodable
}

// NormalizeCode returns the code a worker may report. Only ERROR, PANIC, and
// OUTPUT_NOT_ENCODABLE are accepted. Any other code is ERROR.
func NormalizeCode(code FailureCode) FailureCode {
	switch code {
	case FailureCodePanic, FailureCodeOutputNotEncodable:
		return code
	default:
		return FailureCodeError
	}
}

// DelayAfter returns how long to wait before the try that follows attempt number
// attempt. A retryAfter that is set is used exactly. Otherwise the policy's delay
// for that attempt is used: delays[min(attempt-1, len-1)], varied by jitter. unit
// is a number in [0, 1) that picks where in the jitter range the delay falls.
func DelayAfter(p RetryPolicy, attempt int, retryAfter *time.Duration, jitter, unit float64) time.Duration {
	if retryAfter != nil {
		return *retryAfter
	}

	if len(p.Delays) == 0 {
		return 0
	}

	i := min(max(attempt-1, 0), len(p.Delays)-1)
	factor := 1 - jitter + 2*jitter*unit
	return time.Duration(float64(p.Delays[i]) * factor)
}
