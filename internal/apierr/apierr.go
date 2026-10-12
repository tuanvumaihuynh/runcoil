// Package apierr is the error vocabulary of the server. The core returns these
// errors, possibly wrapped with fmt.Errorf("...: %w", err). The transport layer
// is the only place that translates them. The package imports nothing, so any
// layer may depend on it.
package apierr

// Kind is a semantic failure category. It says what went wrong, not how the
// transport reports it. The zero value is KindInternal, so an Error built
// without a Kind is an internal failure.
type Kind int

const (
	// KindInternal is an unexpected failure. It is the zero value.
	KindInternal Kind = iota
	// KindInvalid is a malformed or rejected request.
	KindInvalid
	// KindUnauthenticated is a missing or wrong credential.
	KindUnauthenticated
	// KindNotFound is a missing resource.
	KindNotFound
	// KindConflict is a clash with existing state.
	KindConflict
	// KindFailedPrecondition is a request that the current state does not allow.
	KindFailedPrecondition
	// KindUnavailable is a transient failure that the caller may retry.
	KindUnavailable
	// KindCanceled is a call that the client gave up on.
	KindCanceled
	// KindDeadlineExceeded is a call whose client deadline passed before it finished.
	KindDeadlineExceeded
)

// Error is the structured error the core returns. Code identifies the failure
// for the client; it is empty for failures that have no schema code. Message is
// the text for people. Details carry identifiers such as "run_id" and "task_id".
type Error struct {
	Kind    Kind
	Code    Code
	Message string
	Details map[string]string
}

// Error returns the message. A nil Error has an empty message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	return e.Message
}

// WithDetail adds one detail and returns the same error, so calls can be chained.
func (e *Error) WithDetail(key, value string) *Error {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}

	e.Details[key] = value
	return e
}

func newError(kind Kind, code Code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Invalid is a malformed or rejected request.
func Invalid(code Code, message string) *Error {
	return newError(KindInvalid, code, message)
}

// Unauthenticated is a missing or wrong credential.
func Unauthenticated(code Code, message string) *Error {
	return newError(KindUnauthenticated, code, message)
}

// NotFound is a missing resource.
func NotFound(code Code, message string) *Error {
	return newError(KindNotFound, code, message)
}

// Conflict is a clash with existing state.
func Conflict(code Code, message string) *Error {
	return newError(KindConflict, code, message)
}

// FailedPrecondition is a request that the current state does not allow.
func FailedPrecondition(code Code, message string) *Error {
	return newError(KindFailedPrecondition, code, message)
}

// Unavailable is a transient failure that the caller may retry.
func Unavailable(code Code, message string) *Error {
	return newError(KindUnavailable, code, message)
}

// Canceled is a call that the client gave up on. It has no schema code.
func Canceled(message string) *Error {
	return newError(KindCanceled, "", message)
}

// DeadlineExceeded is a call whose client deadline passed before it finished. It has no schema code.
func DeadlineExceeded(message string) *Error {
	return newError(KindDeadlineExceeded, "", message)
}
