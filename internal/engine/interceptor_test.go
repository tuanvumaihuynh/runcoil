package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
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
		unimpl := connect.NewError(connect.CodeUnimplemented, "not yet")
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
		{connect.NewError(connect.CodeResourceExhausted, "big"), "resource_exhausted", ""},
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

// callInfoWith returns the CallInfo of a call whose request header is header.
func callInfoWith(header http.Header) *connect.CallInfo {
	info := new(connect.CallInfo)
	for key, values := range header {
		info.RequestHeader().SetValues(key, values)
	}

	return info
}

// A refused credential is the Unauthenticated error that authorize returns.
func TestAuthorizeAcceptsOnlyTheBearerToken(t *testing.T) {
	auth := authInterceptor{token: bearerTestToken}
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			err := auth.authorize(callInfoWith(c.header))
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

// A call with no CallInfo, which Server.Call never passes, is refused.
func TestAuthorizeRefusesACallWithoutCallInfo(t *testing.T) {
	auth := authInterceptor{token: bearerTestToken}
	var apiErr *apierr.Error
	require.ErrorAs(t, auth.authorize(nil), &apiErr, "want an Unauthenticated error")
	assert.Equal(t, apierr.CodeUnauthenticated, apiErr.Code, "code")
}

// testProcedure is the procedure the guarded tests call.
const testProcedure = "/runcoil.v1.RunService/ListTasks"

// guardedServer registers next on a server with the server's interceptors, in the server's
// order, without the request logger: error translation outside the auth check, and the NUL
// check inside it.
func guardedServer(next connect.ServerFunc) *connect.Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := connect.NewServer(
		errorInterceptor{logger: logger}.wrap,
		authInterceptor{token: bearerTestToken}.wrap,
		nulInterceptor{}.wrap,
	)
	server.Register(connect.Method{Spec: connect.Spec{Procedure: testProcedure}, Handler: next})
	return server
}

// serveCall runs one call on the server, as a transport does: the CallInfo carries header,
// and the stream holds the request.
func serveCall(server *connect.Server, header http.Header, stream connect.ServerStream) error {
	return server.Call(context.Background(), testProcedure, callInfoWith(header), stream)
}

// requestStream is a server stream that holds one request message. Receive hands that
// message over, as the transport does. Send discards the response.
type requestStream struct {
	msg proto.Message
}

func (s requestStream) Receive(dst any) error {
	proto.Merge(dst.(proto.Message), s.msg)
	return nil
}

func (requestStream) SendHeaders() error { return nil }

func (requestStream) Send(any) error { return nil }

// A unary call with a credential other than the token never reaches the handler.
func TestUnaryCallNeedsTheBearerToken(t *testing.T) {
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
				var req runcoilv1.GetRunRequest
				if err := stream.Receive(&req); err != nil {
					return err
				}

				reached = true
				return stream.Send(new(runcoilv1.GetRunResponse))
			}

			err := serveCall(guardedServer(next), c.header, requestStream{msg: new(runcoilv1.GetRunRequest)})

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
			msg: &runcoilv1.RecordStepRequest{RunId: "run-1", Outcome: &runcoilv1.RecordStepRequest_Failed{
				Failed: &runcoilv1.AttemptError{Message: "boom\x00"},
			}},
			want: "failed.message must not contain a NUL byte",
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
			msg:  &runcoilv1.InvokeRequest{WorkerId: "demo", TaskId: "hello", Input: []byte("a\x00b")},
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
		WorkerId: "demo",
		TaskId:   "hello",
		Input:    bytes.Repeat([]byte("a"), 1<<20),
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

	msg, valueErr := connectproto.UnmarshalErrorDetail(details[0])
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
			next := func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
				var req runcoilv1.ListTasksRequest
				if err := stream.Receive(&req); err != nil {
					return err
				}

				reached = true
				return stream.Send(new(runcoilv1.ListTasksResponse))
			}

			req := &runcoilv1.ListTasksRequest{WorkerId: c.workerID}
			header := http.Header{"Authorization": {"Bearer " + bearerTestToken}}
			err := serveCall(guardedServer(next), header, requestStream{msg: req})

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
			next := func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
				var req runcoilv1.ListenRequest
				if err := stream.Receive(&req); err != nil {
					return err
				}

				reached = true
				return nil
			}

			header := http.Header{"Authorization": {"Bearer " + bearerTestToken}}
			msg := &runcoilv1.ListenRequest{InstanceId: c.instanceID}
			err := serveCall(guardedServer(next), header, requestStream{msg: msg})

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

// A Listen stream with a credential other than the token never reaches the handler.
func TestStreamingCallNeedsTheBearerToken(t *testing.T) {
	for _, c := range bearerCases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := func(context.Context, connect.Spec, connect.ServerStream) error {
				reached = true
				return nil
			}

			err := serveCall(guardedServer(next), c.header, requestStream{msg: new(runcoilv1.ListenRequest)})

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

// drainTestCalls lists the calls the drain refuses, then the reports it lets pass.
var drainTestCalls = []struct {
	procedure string
	refused   bool
}{
	{runcoilv1connect.RunServiceGetRunProcedure, true},
	{runcoilv1connect.RunServiceInvokeProcedure, true},
	{runcoilv1connect.WorkerServiceRegisterProcedure, true},
	{runcoilv1connect.WorkerServiceListenProcedure, true},
	{runcoilv1connect.WorkerServiceHeartbeatProcedure, false},
	{runcoilv1connect.WorkerServiceAckDispatchProcedure, false},
	{runcoilv1connect.WorkerServiceRecordStepProcedure, false},
	{runcoilv1connect.WorkerServiceFinishRunProcedure, false},
}

// drainingServer serves the drain test calls behind the server's interceptors.
func drainingServer(stopped <-chan struct{}, reached map[string]bool) *connect.Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := connect.NewServer(
		errorInterceptor{logger: logger}.wrap,
		requestLogger{logger: logger}.wrap,
		authInterceptor{token: bearerTestToken}.wrap,
		drainInterceptor{stopped: stopped}.wrap,
		nulInterceptor{}.wrap,
	)
	for _, c := range drainTestCalls {
		procedure := c.procedure
		server.Register(connect.Method{
			Spec: connect.Spec{Procedure: procedure},
			Handler: func(context.Context, connect.Spec, connect.ServerStream) error {
				reached[procedure] = true
				return nil
			},
		})
	}

	return server
}

// callWith runs one call of the procedure on the server, with the header.
func callWith(server *connect.Server, procedure string, header http.Header) error {
	stream := requestStream{msg: new(runcoilv1.GetRunRequest)}
	return server.Call(context.Background(), procedure, callInfoWith(header), stream)
}

// Once the stop has begun, the calls that start new work are refused before their handler.
func TestDrainRefusesNewWorkOnlyOnceStopping(t *testing.T) {
	stopped := make(chan struct{})
	reached := make(map[string]bool)
	server := drainingServer(stopped, reached)
	header := http.Header{"Authorization": {"Bearer " + bearerTestToken}}

	for _, c := range drainTestCalls {
		require.NoError(t, callWith(server, c.procedure, header), "before the stop, %s must pass", c.procedure)
		require.True(t, reached[c.procedure], "before the stop, %s must reach its handler", c.procedure)
	}

	close(stopped)
	for _, c := range drainTestCalls {
		delete(reached, c.procedure)
		err := callWith(server, c.procedure, header)
		if !c.refused {
			require.NoError(t, err, "during the drain, %s must pass", c.procedure)
			require.True(t, reached[c.procedure], "during the drain, %s must reach its handler", c.procedure)
			continue
		}

		require.Error(t, err, "during the drain, %s must be refused", c.procedure)
		assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "%s: connect code", c.procedure)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr, "%s: want a connect error", c.procedure)
		assert.Equal(t, "server is shutting down", connectErr.Message(), "%s: message", c.procedure)
		assert.False(t, reached[c.procedure], "during the drain, %s must not reach its handler", c.procedure)
	}
}

// A caller without the token is still refused as unauthenticated during the drain.
func TestDrainKeepsTheTokenCheck(t *testing.T) {
	stopped := make(chan struct{})
	close(stopped)
	for _, c := range bearerCases {
		if c.accepted {
			continue
		}

		t.Run(c.name, func(t *testing.T) {
			reached := make(map[string]bool)
			server := drainingServer(stopped, reached)
			err := callWith(server, runcoilv1connect.RunServiceGetRunProcedure, c.header)

			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "connect code")
			assert.Empty(t, reached, "a refused caller must not reach any handler")
		})
	}
}
