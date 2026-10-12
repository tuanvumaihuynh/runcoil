package engine

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
)

// errorInterceptor is the one place that translates errors for the client. A
// ConnectRPC error passes through. An apierr.Error becomes its code and detail.
// Any other error is logged, and the client gets an internal error with a fixed text.
type errorInterceptor struct {
	logger *slog.Logger
}

var _ connect.Interceptor = errorInterceptor{}

// WrapUnary translates the error of a unary call.
func (i errorInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		if err != nil {
			return nil, i.translate(ctx, err)
		}

		return resp, nil
	}
}

// WrapStreamingClient does nothing: the server does not call out as a client.
func (errorInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler translates the error that ends a stream.
func (i errorInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := next(ctx, conn); err != nil {
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
		return connect.NewError(connect.CodeInternal, errors.New(internalMessage))
	}

	i.logger.ErrorContext(ctx, "request failed", slog.Any("error", err))
	return connect.NewError(connect.CodeInternal, errors.New(internalMessage))
}

// authInterceptor requires the bearer token on every call, unary and streaming.
type authInterceptor struct {
	token string
}

var _ connect.Interceptor = authInterceptor{}

// WrapUnary checks the credential before the call.
func (i authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := i.authorize(req.Header()); err != nil {
			return nil, err
		}

		return next(ctx, req)
	}
}

// WrapStreamingClient does nothing: the server does not call out as a client.
func (authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler checks the credential before the stream starts.
func (i authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := i.authorize(conn.RequestHeader()); err != nil {
			return err
		}

		return next(ctx, conn)
	}
}

func (i authInterceptor) authorize(header http.Header) error {
	got, ok := strings.CutPrefix(header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(i.token)) != 1 {
		return apierr.Unauthenticated(apierr.CodeUnauthenticated, "missing or invalid bearer token")
	}

	return nil
}

// requestLogger writes one debug line per RPC, when the call ends: its procedure, its
// duration, its result code, and the Runcoil error code when there is one. It reads no
// request or response body, so no token or payload reaches the log. It sits inside the
// error translation, so it sees the Runcoil error before the client does, and outside
// the authentication, so a refused credential is logged too.
type requestLogger struct {
	logger *slog.Logger
}

var _ connect.Interceptor = requestLogger{}

// WrapUnary logs the call when it ends.
func (l requestLogger) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		l.finish(ctx, req.Spec().Procedure, start, err)
		return resp, err
	}
}

// WrapStreamingClient does nothing: the server does not call out as a client.
func (requestLogger) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler logs the stream when it ends, with how long it lasted.
func (l requestLogger) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()
		err := next(ctx, conn)
		l.finish(ctx, conn.Spec().Procedure, start, err)
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

var _ connect.Interceptor = nulInterceptor{}

// WrapUnary checks the request message before the call.
func (nulInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := checkNoNUL(req.Any()); err != nil {
			return nil, err
		}

		return next(ctx, req)
	}
}

// WrapStreamingClient does nothing: the server does not call out as a client.
func (nulInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler checks each message the handler receives on the stream.
func (nulInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, nulCheckedConn{StreamingHandlerConn: conn})
	}
}

// nulCheckedConn checks each message as the handler receives it, so a refused message
// never reaches the handler.
type nulCheckedConn struct {
	connect.StreamingHandlerConn
}

// Receive reads the next message, and returns an error instead when one of its strings
// has a NUL byte.
func (c nulCheckedConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
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
