package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"connectrpc.com/validate"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/proto/gen/go/runcoil/v1/runcoilv1connect"
)

// readMaxBytes is the largest message a worker may send: an output at its limit, with
// room for the rest of the message. A larger one is refused with resource_exhausted.
const readMaxBytes = domain.MaxOutputBytes + 1<<20 // 2 MiB

// newHTTPServer returns the server of the worker protocol. The dispatcher holds the
// workers' streams, so it serves their calls too.
func (s *Service) newHTTPServer() *http.Server {
	server := connect.NewServer(
		errorInterceptor{logger: s.logger}.wrap,
		validate.NewServerInterceptor(),
	)
	runcoilv1connect.RegisterWorkerServiceHandler(server, workerHandler{s: s})

	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithReadMaxBytes(readMaxBytes))

	// HTTP/2 only: without TLS locally, and over TLS behind a terminating proxy. HTTP/1.1
	// comes back with the serverless workers.
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Handler:           mux,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// internalMessage is the only text a client sees for an unexpected failure.
const internalMessage = "internal error"

// errorCodes maps the codes the worker protocol returns to the proto ErrorCode.
var errorCodes = map[apperr.Code]runcoilv1.ErrorCode{
	apperr.CodeInvalidRequest:      runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST,
	apperr.CodeUnauthenticated:     runcoilv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED,
	apperr.CodeInvalidRegistration: runcoilv1.ErrorCode_ERROR_CODE_INVALID_REGISTRATION,
	apperr.CodeStaleLease:          runcoilv1.ErrorCode_ERROR_CODE_STALE_LEASE,
	apperr.CodeOutputTooLarge:      runcoilv1.ErrorCode_ERROR_CODE_OUTPUT_TOO_LARGE,
}

// errorInterceptor is the one place that translates errors for a Connect client. A
// Connect error passes through. An apperr.Error becomes its code and detail. Any other
// error is logged, and the client gets an internal error with a fixed text.
type errorInterceptor struct {
	logger *slog.Logger
}

func (i errorInterceptor) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		if err := next(ctx, spec, stream); err != nil {
			return i.translate(ctx, spec, err)
		}

		return nil
	}
}

func (i errorInterceptor) translate(ctx context.Context, spec connect.Spec, err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		converted, convErr := toConnectError(appErr)
		if convErr != nil {
			i.logger.ErrorContext(ctx, "convert app error failed", slog.Any("error", convErr))
			return connect.NewError(connect.CodeInternal, internalMessage)
		}

		i.logger.WarnContext(ctx, "request refused",
			slog.String("procedure", spec.Procedure),
			slog.String("code", string(appErr.Code)),
			slog.String("message", appErr.Message),
		)

		return converted
	}

	i.logger.ErrorContext(ctx, "request failed",
		slog.String("procedure", spec.Procedure),
		slog.Any("error", err),
	)

	return connect.NewError(connect.CodeInternal, internalMessage)
}

func toConnectError(e *apperr.Error) (*connect.Error, error) {
	connectErr := connect.NewError(connectCodeOf(e.Kind), e.Message)
	if e.Code == "" {
		return connectErr, nil
	}

	detail, err := connectproto.NewErrorDetail(&runcoilv1.ErrorDetail{
		Code:     errorCodes[e.Code],
		TaskName: e.Details["task_name"],
	})
	if err != nil {
		return nil, fmt.Errorf("new error detail: %w", err)
	}

	return connectErr.WithDetail(detail), nil
}

// connectCodeOf maps a Kind to a Connect code.
func connectCodeOf(kind apperr.Kind) connect.Code {
	switch kind {
	case apperr.KindInvalid:
		return connect.CodeInvalidArgument
	case apperr.KindUnauthenticated:
		return connect.CodeUnauthenticated
	case apperr.KindPermissionDenied:
		return connect.CodePermissionDenied
	case apperr.KindNotFound:
		return connect.CodeNotFound
	case apperr.KindConflict:
		return connect.CodeAlreadyExists
	case apperr.KindFailedPrecondition:
		return connect.CodeFailedPrecondition
	case apperr.KindUnavailable:
		return connect.CodeUnavailable
	case apperr.KindCanceled:
		return connect.CodeCanceled
	case apperr.KindDeadlineExceeded:
		return connect.CodeDeadlineExceeded
	default:
		return connect.CodeInternal
	}
}

var errMissingBearer = apperr.Unauthenticated(apperr.CodeUnauthenticated, "missing bearer token")

// bearerToken returns the token of the request's Authorization header.
func bearerToken(ctx context.Context) (string, error) {
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return "", errMissingBearer
	}

	token, ok := strings.CutPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "", errMissingBearer
	}

	return token, nil
}
