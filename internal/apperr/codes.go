package apperr

// Code is the stable identifier of an application error, in lower snake case.
type Code string

// The codes of the application.
const (
	CodeInvalidRequest       Code = "invalid_request"
	CodeUnauthenticated      Code = "unauthenticated"
	CodeIncompatibleProtocol Code = "incompatible_protocol"
	CodeInvalidRegistration  Code = "invalid_registration"
	CodeTaskNotFound         Code = "task_not_found"
	CodeRunNotFound          Code = "run_not_found"
	CodeIdempotencyConflict  Code = "idempotency_conflict"
	CodeInputTooLarge        Code = "input_too_large"
	CodeOutputTooLarge       Code = "output_too_large"
	CodeNotRegistered        Code = "not_registered"
	CodeStaleLease           Code = "stale_lease"
	CodeRunNotFinished       Code = "run_not_finished"
	CodeStepNotFound         Code = "step_not_found"
	CodePermissionDenied     Code = "permission_denied"
	CodeNotFound             Code = "not_found"
	CodePayloadTooLarge      Code = "payload_too_large"
	CodeInternal             Code = "internal"
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
	CodeOutputTooLarge,
	CodeNotRegistered,
	CodeStaleLease,
	CodeRunNotFinished,
	CodeStepNotFound,
	CodePermissionDenied,
	CodeNotFound,
	CodePayloadTooLarge,
	CodeInternal,
}
