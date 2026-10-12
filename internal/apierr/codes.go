package apierr

// Code is the stable identifier of an API error. The engine maps each Code to
// the schema's ErrorCode, which carries it to the client in the error detail.
type Code string

// The codes of the API. Every one has a matching ErrorCode in the schema.
const (
	CodeInvalidRequest       Code = "INVALID_REQUEST"
	CodeUnauthenticated      Code = "UNAUTHENTICATED"
	CodeIncompatibleProtocol Code = "INCOMPATIBLE_PROTOCOL"
	CodeInvalidRegistration  Code = "INVALID_REGISTRATION"
	CodeTaskNotFound         Code = "TASK_NOT_FOUND"
	CodeRunNotFound          Code = "RUN_NOT_FOUND"
	CodeIdempotencyConflict  Code = "IDEMPOTENCY_CONFLICT"
	CodeInputTooLarge        Code = "INPUT_TOO_LARGE"
	CodeNotRegistered        Code = "NOT_REGISTERED"
	CodeStaleLease           Code = "STALE_LEASE"
	CodeRunNotFinished       Code = "RUN_NOT_FINISHED"
	CodeStepNotFound         Code = "STEP_NOT_FOUND"
)

// Codes lists every Code, so that a test can check each one against the schema.
var Codes = []Code{
	CodeInvalidRequest,
	CodeUnauthenticated,
	CodeIncompatibleProtocol,
	CodeInvalidRegistration,
	CodeTaskNotFound,
	CodeRunNotFound,
	CodeIdempotencyConflict,
	CodeInputTooLarge,
	CodeNotRegistered,
	CodeStaleLease,
	CodeRunNotFinished,
	CodeStepNotFound,
}
