package engine

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
)

// errorInterceptor is the one place that translates errors for the client. A
// ConnectRPC error passes through. An apierr.Error becomes its code and detail.
// Any other error is logged, and the client gets an internal error with a fixed text.
type errorInterceptor struct {
	logger *slog.Logger
}

// wrap translates the error that ends the call.
func (i errorInterceptor) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		if err := next(ctx, spec, stream); err != nil {
			return i.translate(ctx, err)
		}

		return nil
	}
}

func (i errorInterceptor) translate(ctx context.Context, err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		converted, convErr := toConnectError(apiErr)
		if convErr == nil {
			return converted
		}

		i.logger.ErrorContext(ctx, "convert api error", slog.Any("error", convErr))
		return connect.NewError(connect.CodeInternal, internalMessage)
	}

	i.logger.ErrorContext(ctx, "request failed", slog.Any("error", err))
	return connect.NewError(connect.CodeInternal, internalMessage)
}

// authInterceptor requires the bearer token on every call, unary and streaming.
type authInterceptor struct {
	token string
}

// wrap checks the credential before the call, and before any message is read.
func (i authInterceptor) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		// Server.Call attaches the CallInfo before the interceptors run.
		info, _ := connect.CallInfoForServerContext(ctx)
		if err := i.authorize(info); err != nil {
			return err
		}

		return next(ctx, spec, stream)
	}
}

// authorize returns the Unauthenticated error unless the request header carries the token
// under the Bearer scheme. A call with no CallInfo is refused.
func (i authInterceptor) authorize(info *connect.CallInfo) error {
	var authorization string
	if info != nil {
		authorization = info.RequestHeader().Get("Authorization")
	}

	got, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(i.token)) != 1 {
		return apierr.Unauthenticated(apierr.CodeUnauthenticated, "missing or invalid bearer token")
	}

	return nil
}

// errServerShuttingDown returns the error of every call that a stop refuses. It is the same
// error WaitRun returns when the stop ends its wait. Each call gets a new error, because
// an apierr.Error carries details that a caller may add to.
func errServerShuttingDown() *apierr.Error {
	return apierr.Unavailable("", "server is shutting down")
}

// drainInterceptor refuses the calls that start new work once the service is stopping:
// every RunService call, and the WorkerService Register and Listen calls, which register
// an instance and attach its stream. The refusal happens before the handler runs. The
// calls that a worker makes on the Runs it holds (Heartbeat, AckDispatch, RecordStep, and
// FinishRun) pass, so the worker can finish them during the drain. It sits after the token
// check, so a caller without the token is refused as before.
type drainInterceptor struct {
	stopped <-chan struct{}
}

// wrap refuses the call when the service is stopping and the procedure starts new work.
func (i drainInterceptor) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		if isStopping(i.stopped) && startsNewWork(spec.Procedure) {
			return errServerShuttingDown()
		}

		return next(ctx, spec, stream)
	}
}

// isStopping reports whether stopped is closed.
func isStopping(stopped <-chan struct{}) bool {
	select {
	case <-stopped:
		return true
	default:
		return false
	}
}

// startsNewWork reports whether the procedure is one that the drain refuses.
func startsNewWork(procedure string) bool {
	if strings.HasPrefix(procedure, "/"+runcoilv1connect.RunServiceName+"/") {
		return true
	}

	return procedure == runcoilv1connect.WorkerServiceRegisterProcedure ||
		procedure == runcoilv1connect.WorkerServiceListenProcedure
}

// requestLogger writes one debug line per RPC, when the call ends: its procedure, its
// duration, its result code, and the Runcoil error code when there is one. It reads no
// request or response body, so no token or payload reaches the log. It sits inside the
// error translation, so it sees the Runcoil error before the client does, and outside
// the authentication, so a refused credential is logged too.
type requestLogger struct {
	logger *slog.Logger
}

// wrap logs the call when it ends, with how long it lasted.
func (l requestLogger) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		start := time.Now()
		err := next(ctx, spec, stream)
		l.finish(ctx, spec.Procedure, start, err)
		return err
	}
}

// finish writes the line of one call.
func (l requestLogger) finish(ctx context.Context, procedure string, start time.Time, err error) {
	code, errorCode := resultOf(err)
	if ctx.Err() != nil && err != nil {
		// The client went away or gave up: the call was cancelled, whatever error it returned.
		code = connect.CodeCanceled.String()
	}

	attrs := []any{
		slog.String("procedure", procedure),
		slog.Duration("duration", time.Since(start)),
		slog.String("code", code),
	}
	if errorCode != "" {
		attrs = append(attrs, slog.String("error_code", errorCode))
	}

	l.logger.DebugContext(ctx, "rpc finished", attrs...)
}

// resultOf names the result of a call: "ok", or the connect code the error maps to. It
// also returns the Runcoil error code when the error carries one.
func resultOf(err error) (code, errorCode string) {
	if err == nil {
		return "ok", ""
	}

	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return connectCodeOf(apiErr.Kind).String(), string(apiErr.Code)
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code().String(), ""
	}

	return connect.CodeInternal.String(), ""
}

// nulInterceptor refuses a request that has a NUL byte in any of its strings. The store
// keeps request text as text, and text cannot hold a NUL byte, so the check runs here,
// before any handler. It checks the message of a unary call and each message a stream
// receives. Bytes fields are payloads stored as binary, so they are not checked. It sits
// inside the authentication, so only a caller with the token reaches it.
type nulInterceptor struct{}

// wrap checks each message the handler receives, so a refused message never reaches the
// handler. Generated code reads the request of a unary call with Receive too, so the check
// covers unary and streaming calls alike.
func (nulInterceptor) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		return next(ctx, spec, nulCheckedStream{ServerStream: stream})
	}
}

// nulCheckedStream checks each message as the handler receives it.
type nulCheckedStream struct {
	connect.ServerStream
}

// Receive reads the next message, and returns an error instead when one of its strings
// has a NUL byte.
func (s nulCheckedStream) Receive(msg any) error {
	if err := s.ServerStream.Receive(msg); err != nil {
		return err
	}

	return checkNoNUL(msg)
}

// checkNoNUL returns an invalid-request error for the first string of msg that has a NUL
// byte. The error names the field by its path, such as attempts[1].run_id. Every message
// of this API is a protobuf message, so a value of any other type is not checked.
func checkNoNUL(msg any) error {
	m, ok := msg.(proto.Message)
	if !ok {
		return nil
	}

	path, found := nulPathInMessage(m.ProtoReflect())
	if !found {
		return nil
	}

	return apierr.Invalid(apierr.CodeInvalidRequest, path+" must not contain a NUL byte")
}

// nulPathInMessage walks the populated fields of m in declaration order, and returns the
// path of the first string that has a NUL byte, relative to m. The walk builds no string:
// a path is built only on the way out, from the fields that the walk found.
func nulPathInMessage(m protoreflect.Message) (string, bool) {
	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if !m.Has(fd) {
			continue
		}

		if path, found := nulPathInField(fd, m.Get(fd)); found {
			return path, true
		}
	}

	return "", false
}

// nulPathInField checks one populated field: each element of a list, each key and value of
// a map, or the value of a singular field. The path it returns starts with the field's name.
func nulPathInField(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, bool) {
	switch {
	case fd.IsMap():
		var path string
		found := false
		v.Map().Range(func(key protoreflect.MapKey, val protoreflect.Value) bool {
			suffix, bad := nulSuffixOf(fd.MapKey(), key.Value())
			if !bad {
				suffix, bad = nulSuffixOf(fd.MapValue(), val)
			}

			if bad {
				path = fmt.Sprintf("%s[%s]%s", fd.Name(), strconv.Quote(key.String()), suffix)
				found = true
			}

			return !bad
		})

		return path, found
	case fd.IsList():
		list := v.List()
		for i := range list.Len() {
			if suffix, bad := nulSuffixOf(fd, list.Get(i)); bad {
				return fmt.Sprintf("%s[%d]%s", fd.Name(), i, suffix), true
			}
		}

		return "", false
	default:
		if suffix, bad := nulSuffixOf(fd, v); bad {
			return string(fd.Name()) + suffix, true
		}

		return "", false
	}
}

// nulSuffixOf checks one value of a field: a string is refused when it has a NUL byte, and
// a message is checked field by field. It returns the path from the value to the offending
// string: empty for a string, and ".field..." inside a message.
func nulSuffixOf(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, bool) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		if strings.ContainsRune(v.String(), 0) {
			return "", true
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if path, found := nulPathInMessage(v.Message()); found {
			return "." + path, true
		}
	}

	return "", false
}
