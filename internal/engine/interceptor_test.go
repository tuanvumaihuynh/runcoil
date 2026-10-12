package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
)

func TestTranslate(t *testing.T) {
	i := errorInterceptor{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx := context.Background()

	t.Run("api error wrapped by the core keeps its code", func(t *testing.T) {
		wrapped := fmt.Errorf("get run: %w", apierr.FailedPrecondition(apierr.CodeStaleLease, "stale"))
		got := asConnectError(t, i.translate(ctx, wrapped))
		require.Equal(t, connect.CodeFailedPrecondition, got.Code())
	})

	t.Run("connect error passes through", func(t *testing.T) {
		unimpl := connect.NewError(connect.CodeUnimplemented, errors.New("not yet"))
		got := asConnectError(t, i.translate(ctx, unimpl))
		require.Equal(t, connect.CodeUnimplemented, got.Code())
	})

	t.Run("other error becomes internal with a fixed message", func(t *testing.T) {
		secret := errors.New("pq: password authentication failed for user runcoil")
		got := asConnectError(t, i.translate(ctx, fmt.Errorf("claim run: %w", secret)))
		require.Equal(t, connect.CodeInternal, got.Code())
		require.Equal(t, internalMessage, got.Message())
	})
}

// The request logger's codes: ok, the connect code of an apierr, a connect error, and internal.
func TestResultOfNamesTheCode(t *testing.T) {
	cases := []struct {
		err       error
		code      string
		errorCode string
	}{
		{nil, "ok", ""},
		{apierr.NotFound(apierr.CodeRunNotFound, "x"), "not_found", "RUN_NOT_FOUND"},
		{apierr.FailedPrecondition(apierr.CodeStaleLease, "x"), "failed_precondition", "STALE_LEASE"},
		{connect.NewError(connect.CodeResourceExhausted, errors.New("big")), "resource_exhausted", ""},
		{errors.New("boom"), "internal", ""},
	}

	for _, c := range cases {
		code, errorCode := resultOf(c.err)
		require.Equal(t, c.code, code, "code of resultOf(%v)", c.err)
		require.Equal(t, c.errorCode, errorCode, "error code of resultOf(%v)", c.err)
	}
}

// Expected client errors never reach a level above debug: a refused call logs its code at debug only.
func TestExpectedClientErrorsStayAtDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	l := requestLogger{logger: logger}
	l.finish(context.Background(), "/runcoil.v1.RunService/GetRun", time.Now(),
		apierr.NotFound(apierr.CodeRunNotFound, "run not found"))

	require.Empty(t, buf.String(), "a not-found call must not reach the info level")
}

// bearerTestToken is the token the auth tests configure.
const bearerTestToken = "s3cret-token"

// bearerCases are the Authorization headers the auth tests send. Only the configured
// token under the Bearer scheme is accepted.
var bearerCases = []struct {
	name     string
	header   http.Header
	accepted bool
}{
	{"right token with the Bearer scheme", http.Header{"Authorization": {"Bearer s3cret-token"}}, true},
	{"no Authorization header", http.Header{}, false},
	{"empty Authorization value", http.Header{"Authorization": {""}}, false},
	{"wrong token", http.Header{"Authorization": {"Bearer wrong-token"}}, false},
	{"right token without the scheme", http.Header{"Authorization": {"s3cret-token"}}, false},
	{"right token under the Basic scheme", http.Header{"Authorization": {"Basic s3cret-token"}}, false},
	{"prefix of the right token", http.Header{"Authorization": {"Bearer s3cret"}}, false},
	{"right token with a suffix", http.Header{"Authorization": {"Bearer s3cret-token-extra"}}, false},
}

// A refused credential is the Unauthenticated error that authorize returns.
func TestAuthorizeAcceptsOnlyTheBearerToken(t *testing.T) {
	auth := authInterceptor{token: bearerTestToken}
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			err := auth.authorize(c.header)
			if c.accepted {
				require.NoError(t, err, "authorize(%v): the configured token must be accepted", c.header)
				return
			}

			var apiErr *apierr.Error
			require.ErrorAs(t, err, &apiErr, "authorize(%v): want an Unauthenticated error", c.header)
			assert.Equal(t, apierr.KindUnauthenticated, apiErr.Kind, "kind")
			assert.Equal(t, apierr.CodeUnauthenticated, apiErr.Code, "code")
			assert.Equal(t, "missing or invalid bearer token", apiErr.Message, "message")
		})
	}
}

// guardedUnary wraps next in the server's order, without the request logger: error
// translation outside the auth check, and the NUL check inside it.
func guardedUnary(next connect.UnaryFunc) connect.UnaryFunc {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return errorInterceptor{logger: logger}.WrapUnary(
		authInterceptor{token: bearerTestToken}.WrapUnary(nulInterceptor{}.WrapUnary(next)),
	)
}

// A unary call with a credential other than the token never reaches the handler.
func TestUnaryCallNeedsTheBearerToken(t *testing.T) {
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
				reached = true
				return connect.NewResponse(new(runcoilv1.GetRunResponse)), nil
			}

			req := connect.NewRequest(new(runcoilv1.GetRunRequest))
			maps.Copy(req.Header(), c.header)
			_, err := guardedUnary(next)(context.Background(), req)

			if c.accepted {
				require.NoError(t, err, "the configured token must reach the handler")
				require.True(t, reached, "the configured token must reach the handler")
				return
			}

			require.Error(t, err, "a refused call must return an error")
			assert.False(t, reached, "a refused call must not reach the handler")
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "connect code")
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr, "want a connect error")
			assert.Equal(t, "missing or invalid bearer token", connectErr.Message(), "message")
		})
	}
}

// The NUL check names the field of the first string that has a NUL byte, by its path.
// Bytes fields are payloads, so a NUL byte in one is accepted.
func TestCheckNoNULNamesTheFieldByPath(t *testing.T) {
	cases := []struct {
		name string
		msg  proto.Message
		want string // the error text; empty when the message is accepted
	}{
		{
			name: "top-level field",
			msg:  &runcoilv1.ListTasksRequest{WorkerId: "demo\x00"},
			want: "worker_id must not contain a NUL byte",
		},
		{
			name: "field of a nested message",
			msg:  &runcoilv1.InvokeRequest{Task: &runcoilv1.TaskRef{WorkerId: "demo", TaskId: "hello\x00"}},
			want: "task.task_id must not contain a NUL byte",
		},
		{
			name: "element of a repeated message, as in a heartbeat",
			msg: &runcoilv1.HeartbeatRequest{InstanceId: "i-1", Attempts: []*runcoilv1.AttemptRef{
				{RunId: "run-1", LeaseToken: "tok-1"},
				{RunId: "run\x00-2", LeaseToken: "tok-2"},
			}},
			want: "attempts[1].run_id must not contain a NUL byte",
		},
		{
			name: "map value",
			msg: &structpb.Struct{Fields: map[string]*structpb.Value{
				"plain": structpb.NewStringValue("ok"),
				"key":   structpb.NewStringValue("a\x00b"),
			}},
			want: `fields["key"].string_value must not contain a NUL byte`,
		},
		{
			name: "bytes field holding a NUL byte",
			msg:  &runcoilv1.InvokeRequest{Task: &runcoilv1.TaskRef{WorkerId: "demo", TaskId: "hello"}, Input: []byte("a\x00b")},
		},
		{
			name: "clean message",
			msg: &runcoilv1.HeartbeatRequest{InstanceId: "i-1", Attempts: []*runcoilv1.AttemptRef{
				{RunId: "run-1", LeaseToken: "tok-1", StepId: "charge#2", StepAttempt: 1},
			}},
		},
		{
			name: "empty message",
			msg:  new(runcoilv1.HeartbeatRequest),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkNoNUL(c.msg)
			if c.want == "" {
				require.NoError(t, err, "a message with no NUL byte in a string must be accepted")
				return
			}

			var apiErr *apierr.Error
			require.ErrorAs(t, err, &apiErr, "want an invalid-request error, got %v", err)
			assert.Equal(t, apierr.KindInvalid, apiErr.Kind, "kind")
			assert.Equal(t, apierr.CodeInvalidRequest, apiErr.Code, "code")
			assert.Equal(t, c.want, apiErr.Message, "message")
		})
	}
}

// BenchmarkCheckNoNUL measures the check on clean messages, which is what almost every
// request is. A heartbeat grows with its attempts. An Invoke carries a 1 MiB input, which
// the check does not read, since bytes are payloads.
func BenchmarkCheckNoNUL(b *testing.B) {
	for _, n := range []int{1, 100, 1000} {
		req := &runcoilv1.HeartbeatRequest{InstanceId: "instance-1"}
		for i := range n {
			req.Attempts = append(req.Attempts, &runcoilv1.AttemptRef{
				RunId:       fmt.Sprintf("run-%d", i),
				LeaseToken:  "lease-token",
				StepId:      "charge",
				StepAttempt: 1,
			})
		}

		b.Run(fmt.Sprintf("heartbeat_attempts_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				require.NoError(b, checkNoNUL(req))
			}
		})
	}

	invoke := &runcoilv1.InvokeRequest{
		Task:  &runcoilv1.TaskRef{WorkerId: "demo", TaskId: "hello"},
		Input: bytes.Repeat([]byte("a"), 1<<20),
	}
	b.Run("invoke_input_1MiB", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			require.NoError(b, checkNoNUL(invoke))
		}
	})
}

// requireErrorDetailCode checks that a connect error carries exactly one ErrorDetail, with the given code.
func requireErrorDetailCode(t *testing.T, err error, want runcoilv1.ErrorCode) {
	t.Helper()
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "want a connect error")
	details := connectErr.Details()
	require.Len(t, details, 1, "the error must carry the server's ErrorDetail")

	msg, valueErr := details[0].Value()
	require.NoError(t, valueErr, "decode the error detail")
	detail, ok := msg.(*runcoilv1.ErrorDetail)
	require.True(t, ok, "the error detail is %T, want *ErrorDetail", msg)
	assert.Equal(t, want, detail.GetCode(), "error detail code")
}

// A unary call whose request has a NUL byte never reaches the handler. The caller gets
// invalid_argument, the field path, and the Runcoil error detail of a validation error.
func TestUnaryCallRefusesANulByteBeforeTheHandler(t *testing.T) {
	cases := []struct {
		name     string
		workerID string
		refused  bool
	}{
		{"clean worker_id reaches the handler", "demo", false},
		{"NUL in worker_id is refused", "demo\x00", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
				reached = true
				return connect.NewResponse(new(runcoilv1.ListTasksResponse)), nil
			}

			req := connect.NewRequest(&runcoilv1.ListTasksRequest{WorkerId: c.workerID})
			req.Header().Set("Authorization", "Bearer "+bearerTestToken)
			_, err := guardedUnary(next)(context.Background(), req)

			if !c.refused {
				require.NoError(t, err, "a clean request must reach the handler")
				require.True(t, reached, "a clean request must reach the handler")
				return
			}

			require.Error(t, err, "a request with a NUL byte must return an error")
			assert.False(t, reached, "a refused call must not reach the handler")
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr, "want a connect error")
			assert.Equal(t, "worker_id must not contain a NUL byte", connectErr.Message(), "message")
			requireErrorDetailCode(t, err, runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST)
		})
	}
}

// requestConn is a stream that holds one request message. Receive hands that message
// over, as the transport does. RequestHeader is what the auth check reads.
type requestConn struct {
	connect.StreamingHandlerConn
	header http.Header
	msg    proto.Message
}

func (c requestConn) RequestHeader() http.Header { return c.header }

func (c requestConn) Receive(dst any) error {
	proto.Merge(dst.(proto.Message), c.msg)
	return nil
}

// A Listen request with a NUL byte never reaches the handler. The message is checked as
// the handler receives it, so the handler's Receive fails with invalid_argument and the field path.
func TestListenRefusesANulByteBeforeTheHandler(t *testing.T) {
	cases := []struct {
		name       string
		instanceID string
		refused    bool
	}{
		{"clean instance_id reaches the handler", "i-1", false},
		{"NUL in instance_id is refused", "i-\x001", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(_ context.Context, conn connect.StreamingHandlerConn) error {
				var req runcoilv1.ListenRequest
				if err := conn.Receive(&req); err != nil {
					return err
				}

				reached = true
				return nil
			}

			conn := requestConn{
				header: http.Header{"Authorization": {"Bearer " + bearerTestToken}},
				msg:    &runcoilv1.ListenRequest{InstanceId: c.instanceID},
			}
			err := guardedStream(next)(context.Background(), conn)

			if !c.refused {
				require.NoError(t, err, "a clean request must reach the handler")
				require.True(t, reached, "a clean request must reach the handler")
				return
			}

			require.Error(t, err, "a request with a NUL byte must end the stream with an error")
			assert.False(t, reached, "a refused stream must not reach the handler")
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr, "want a connect error")
			assert.Equal(t, "instance_id must not contain a NUL byte", connectErr.Message(), "message")
		})
	}
}

// headerConn is a stream that answers only RequestHeader. The auth check reads no other part of it.
type headerConn struct {
	connect.StreamingHandlerConn
	header http.Header
}

func (c headerConn) RequestHeader() http.Header { return c.header }

// guardedStream wraps next in the server's order, without the request logger: error
// translation outside the auth check, and the NUL check inside it.
func guardedStream(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return errorInterceptor{logger: logger}.WrapStreamingHandler(
		authInterceptor{token: bearerTestToken}.WrapStreamingHandler(nulInterceptor{}.WrapStreamingHandler(next)),
	)
}

// A Listen stream with a credential other than the token never reaches the handler.
func TestStreamingCallNeedsTheBearerToken(t *testing.T) {
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(context.Context, connect.StreamingHandlerConn) error {
				reached = true
				return nil
			}

			err := guardedStream(next)(context.Background(), headerConn{header: c.header})

			if c.accepted {
				require.NoError(t, err, "the configured token must reach the handler")
				require.True(t, reached, "the configured token must reach the handler")
				return
			}

			require.Error(t, err, "a refused stream must return an error")
			assert.False(t, reached, "a refused stream must not reach the handler")
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "connect code")
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr, "want a connect error")
			assert.Equal(t, "missing or invalid bearer token", connectErr.Message(), "message")
		})
	}
}
