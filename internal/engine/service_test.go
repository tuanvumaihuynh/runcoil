package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1/runcoilv1connect"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// NewService builds the engine's in-process hub, and the dispatcher and the scheduler share it:
// a wake-up from the dispatcher reaches a subscriber that the engine hands out.
func TestServiceDefaultSignalsAreSharedByItsParts(t *testing.T) {
	store := newFakeStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewService(testConfig(), logger, store, store, store, store)

	_, ok := e.signals.(*inProcessSignals)
	require.True(t, ok, "Signals = %T, want the in-process hub", e.signals)
	require.Same(t, e.signals, e.dispatcher.signals, "the dispatcher and the scheduler must share the engine's Signals")
	require.Same(t, e.signals, e.scheduler.signals, "the dispatcher and the scheduler must share the engine's Signals")

	runs, unsubscribe := e.signals.Subscribe(TopicRuns)
	defer unsubscribe()
	e.dispatcher.wakeUp()

	select {
	case <-runs:
	case <-time.After(time.Second):
		require.Fail(t, "a wake-up from the dispatcher must reach a subscriber of the engine's Signals")
	}
}

// The four store arguments of newService have distinct types in this test, so an argument
// that reaches the wrong field is a type mismatch. Each wrapper embeds the fake store, so
// it satisfies every store interface; only its type says which argument it was.
type taskStoreArg struct{ *fakeStore }

type runStoreArg struct{ *fakeStore }

type queueArg struct{ *fakeStore }

type scheduleStoreArg struct{ *fakeStore }

// TestNewServiceWiresEachStoreToItsFields builds the engine through newService, and checks
// that the engine, the dispatcher, and the scheduler each hold the argument they use.
func TestNewServiceWiresEachStoreToItsFields(t *testing.T) {
	store := newFakeStore()
	tasks, runs, queue, schedules := taskStoreArg{store}, runStoreArg{store}, queueArg{store}, scheduleStoreArg{store}
	e := newService(
		testConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		tasks,
		runs,
		queue,
		schedules,
		newFakeSignals(),
		func() time.Time { return testNow },
		func() float64 { return 0.5 },
	)

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"engine.tasks", e.tasks, tasks},
		{"engine.runs", e.runs, runs},
		{"engine.queue", e.queue, queue},
		{"engine.schedules", e.schedules, schedules},
		{"dispatcher.tasks", e.dispatcher.tasks, tasks},
		{"dispatcher.runs", e.dispatcher.runs, runs},
		{"dispatcher.queue", e.dispatcher.queue, queue},
		{"scheduler.runs", e.scheduler.runs, runs},
		{"scheduler.queue", e.scheduler.queue, queue},
		{"scheduler.schedules", e.scheduler.schedules, schedules},
	}

	for _, c := range checks {
		// Identity, not deep equality: two empty fake stores would be deeply equal.
		assert.True(t, c.got == c.want, "%s = %T, want %T", c.field, c.got, c.want)
	}
}

// The HTTP server applies the token check to the calls it serves. This serves the handler
// that newHTTPServer builds, and calls ListTasks once with the configured token and once with another.
func TestHTTPServerAppliesTheTokenCheck(t *testing.T) {
	e := newTestService(t, newFakeStore())
	server := httptest.NewServer(e.newHTTPServer(":0").Handler)
	t.Cleanup(server.Close)

	client := runcoilv1connect.NewRunServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))
	listTasks := func(token string) error {
		_, err := client.ListTasks(bearerContext(token), &runcoilv1.ListTasksRequest{WorkerId: "demo"})
		return err
	}

	require.NoError(t, listTasks(e.cfg.Token), "a call with the configured token must succeed")

	err := listTasks("not-the-token")
	require.Error(t, err, "a call with another token must be refused")
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "connect code")
}

// serveForTest serves the handler that newHTTPServer builds, so that a test's call goes
// through the interceptor chain of the server.
func serveForTest(t *testing.T, e *Service) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(e.newHTTPServer(":0").Handler)
	t.Cleanup(server.Close)
	return server
}

// bearerContext returns the context of a call that sends token in its Authorization header.
func bearerContext(token string) context.Context {
	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Authorization", "Bearer "+token)
	return ctx
}

// A NUL byte in a run_id is refused at the edge: GetRun is invalid_argument with the field
// name and the error detail. This test fails if nulInterceptor is removed from the handler
// options, because the call would then reach the store and answer not found.
func TestHTTPServerRefusesANulByteInRunID(t *testing.T) {
	e := newTestService(t, newFakeStore())
	server := serveForTest(t, e)

	client := runcoilv1connect.NewRunServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))
	req := &runcoilv1.GetRunRequest{By: &runcoilv1.GetRunRequest_RunId{RunId: "a\u0000b"}}
	_, err := client.GetRun(bearerContext(e.cfg.Token), req)

	require.Error(t, err, "a run_id with a NUL byte must be refused")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "want a connect error")
	assert.Equal(t, "run_id must not contain a NUL byte", connectErr.Message(), "message")
	requireErrorDetailCode(t, err, runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST)
}

// A Listen call with a NUL byte in instance_id ends at once with invalid_argument, before
// the handler attaches a stream. It goes through the server's chain, as the unary calls do.
func TestHTTPServerRefusesANulByteInListen(t *testing.T) {
	e := newTestService(t, newFakeStore())
	server := serveForTest(t, e)
	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

	req := &runcoilv1.ListenRequest{InstanceId: "i-1\u0000"}
	stream, err := client.Listen(bearerContext(e.cfg.Token), req)
	require.NoError(t, err, "a server stream opens before its first message")
	msg, err := stream.Receive()
	require.Nil(t, msg, "a refused stream must yield no message")
	_ = stream.Close()

	require.Error(t, err, "a Listen request with a NUL byte must be refused")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "want a connect error")
	assert.Equal(t, "instance_id must not contain a NUL byte", connectErr.Message(), "message")
}

// A Heartbeat with a NUL byte in one attempt's run_id is refused before the store is
// called, so none of its attempts is renewed, the valid ones included. A clean heartbeat
// first shows that the store does count its renewals.
func TestHTTPServerRefusesAHeartbeatWithANulRunID(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	server := serveForTest(t, e)
	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))
	first := seedLeased(t, store, testRef, "tok-1", false, testNow.Add(time.Minute))
	second := seedLeased(t, store, testRef, "tok-2", false, testNow.Add(time.Minute))

	heartbeat := func(attempts ...*runcoilv1.AttemptRef) error {
		req := &runcoilv1.HeartbeatRequest{InstanceId: "i-1", Attempts: attempts}
		_, err := client.Heartbeat(bearerContext(e.cfg.Token), req)
		return err
	}

	require.NoError(t, heartbeat(&runcoilv1.AttemptRef{RunId: first, LeaseToken: "tok-1"}),
		"a heartbeat with valid attempts must succeed")
	require.Equal(t, 1, store.renewCount(), "the clean heartbeat must reach the store")

	err := heartbeat(
		&runcoilv1.AttemptRef{RunId: first, LeaseToken: "tok-1"},
		&runcoilv1.AttemptRef{RunId: "run\u0000-bad", LeaseToken: "tok-x"},
		&runcoilv1.AttemptRef{RunId: second, LeaseToken: "tok-2"},
	)

	require.Error(t, err, "a heartbeat with a NUL byte in one run_id must be refused")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "want a connect error")
	assert.Equal(t, "attempts[1].run_id must not contain a NUL byte", connectErr.Message(), "message")
	requireErrorDetailCode(t, err, runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST)
	assert.Equal(t, 1, store.renewCount(), "the store must not be called for a refused heartbeat")
}

// An identifier longer than 255 bytes is refused at the edge as invalid_argument, and the
// message names the field. Each call goes through the server's chain, and the store is not
// reached: a refused Register stores no Task, an Invoke does not get as far as the Task
// lookup, and a RecordStep stores no Step. A Register refusal carries INVALID_REGISTRATION,
// as the other refusals of a Task do; Invoke and RecordStep carry INVALID_REQUEST.
func TestHTTPServerRefusesAnOverlongIdentifier(t *testing.T) {
	long := strings.Repeat("x", 300)

	t.Run("register worker_id", func(t *testing.T) {
		store := newFakeStore()
		e := newTestService(t, store)
		server := serveForTest(t, e)
		client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

		req := validRegisterRequest()
		req.WorkerId = long
		err := registerOver(client, e, req)

		requireInvalidArgument(t, err, "worker_id is longer than 255 bytes", runcoilv1.ErrorCode_ERROR_CODE_INVALID_REGISTRATION)
		require.Empty(t, store.tasks, "a refused Register stores no Task")
	})

	t.Run("register instance_id", func(t *testing.T) {
		store := newFakeStore()
		e := newTestService(t, store)
		server := serveForTest(t, e)
		client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

		req := validRegisterRequest()
		req.InstanceId = long
		err := registerOver(client, e, req)

		requireInvalidArgument(t, err, "instance_id is longer than 255 bytes", runcoilv1.ErrorCode_ERROR_CODE_INVALID_REGISTRATION)
		require.Empty(t, store.tasks, "a refused Register stores no Task")
	})

	t.Run("register task_id", func(t *testing.T) {
		store := newFakeStore()
		e := newTestService(t, store)
		server := serveForTest(t, e)
		client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

		req := validRegisterRequest()
		req.Tasks[1].TaskId = long
		err := registerOver(client, e, req)

		requireInvalidArgument(t, err, "task_id is longer than 255 bytes", runcoilv1.ErrorCode_ERROR_CODE_INVALID_REGISTRATION)
		require.Empty(t, store.tasks, "a refused Register stores no Task")
	})

	t.Run("invoke task_id", func(t *testing.T) {
		store := newFakeStore()
		e := newTestService(t, store)
		server := serveForTest(t, e)
		client := runcoilv1connect.NewRunServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

		req := &runcoilv1.InvokeRequest{
			WorkerId:       "demo",
			TaskId:         long,
			IdempotencyKey: "k-1",
		}
		_, err := client.Invoke(bearerContext(e.cfg.Token), req)

		requireInvalidArgument(t, err, "task_id is longer than 255 bytes", runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST)
	})

	t.Run("record step step_id", func(t *testing.T) {
		store := newFakeStore()
		e := newTestService(t, store)
		server := serveForTest(t, e)
		client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))
		id := store.seedRunning(t, testRef, "tok-1")

		req := &runcoilv1.RecordStepRequest{
			RunId:       id,
			LeaseToken:  "tok-1",
			StepId:      long,
			StepAttempt: 1,
			Outcome:     &runcoilv1.RecordStepRequest_Succeeded{Succeeded: &runcoilv1.StepSucceeded{}},
		}
		_, err := client.RecordStep(bearerContext(e.cfg.Token), req)

		requireInvalidArgument(t, err, "step_id is longer than 255 bytes", runcoilv1.ErrorCode_ERROR_CODE_INVALID_REQUEST)
		require.Empty(t, store.steps[id], "a refused RecordStep stores no Step")
	})
}

// registerOver sends a Register request with the configured token, and returns the error.
func registerOver(client runcoilv1connect.WorkerServiceClient, e *Service, msg *runcoilv1.RegisterRequest) error {
	_, err := client.Register(bearerContext(e.cfg.Token), msg)
	return err
}

// requireInvalidArgument checks that err is an invalid_argument error with the given message
// and the given error detail code.
func requireInvalidArgument(t *testing.T, err error, message string, code runcoilv1.ErrorCode) {
	t.Helper()
	require.Error(t, err, "the call must be refused")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "connect code")
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr, "want a connect error")
	assert.Equal(t, message, connectErr.Message(), "message")
	requireErrorDetailCode(t, err, code)
}

// The HTTP server retires a connection after IdleTimeout without a request. The value is two
// minutes in production; this test shortens it so the timer fires during the test. A Listen
// stream that is open while the timer fires keeps delivering its messages, and a unary call
// on the same client still succeeds.
func TestListenStreamOutlivesTheIdleTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.HeartbeatInterval = 20 * time.Millisecond
	e := newFakeService(t, newFakeStore(), cfg, testNow)
	registerInstance(t, e, "i-1", testNow)

	srv := e.newHTTPServer("127.0.0.1:0")
	srv.IdleTimeout = 100 * time.Millisecond
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	// The client speaks HTTP/2 without TLS, as the worker does against the server.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	httpClient := &http.Client{Transport: &http.Transport{Protocols: protocols}}
	workers := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(httpClient, "http://"+listener.Addr().String(), connecthttp.WithReadMaxBytes(0))))

	stream, err := workers.Listen(bearerContext(e.cfg.Token), &runcoilv1.ListenRequest{InstanceId: "i-1"})
	require.NoError(t, err, "open the stream")
	t.Cleanup(func() { _ = stream.Close() })

	started := time.Now()
	received := 0
	for time.Since(started) < 500*time.Millisecond {
		_, err := stream.Receive()
		require.NoError(t, err, "the stream must keep delivering, but it ended")
		received++
	}
	require.Greater(t, received, 0, "the stream delivered no message")

	beat := &runcoilv1.HeartbeatRequest{InstanceId: "i-1"}
	_, err = workers.Heartbeat(bearerContext(e.cfg.Token), beat)
	require.NoError(t, err, "a unary call after the idle timer fired must still succeed")
}

// The HTTP server retires a connection that has no request for two minutes.
func TestHTTPServerRetiresIdleConnectionsAfterTwoMinutes(t *testing.T) {
	e := newTestService(t, newFakeStore())
	require.Equal(t, 2*time.Minute, e.newHTTPServer(":0").IdleTimeout, "IdleTimeout")
}

// freePort returns a TCP port that nothing listens on, as far as the test can tell.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err, "listen for a free port")
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// canDial reports whether something accepts connections on the address.
func canDial(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

// runInBackground runs the service with ctx, and returns the channel that receives its error.
func runInBackground(s *Service, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return done
}

// waitForRun waits for Run to return. The limit only bounds a broken test.
func waitForRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Run did not return")
		return nil
	}
}

// Run listens until Stop, then shuts the server down and returns nil. Stop is safe to call twice.
func TestRunServesUntilStopAndThenReturnsNil(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	s := newFakeService(t, newFakeStore(), cfg, testNow)

	done := runInBackground(s, context.Background())
	require.Eventually(t, func() bool { return canDial(addr) }, 5*time.Second, time.Millisecond,
		"the server must listen once Run starts")

	s.Stop()
	s.Stop()
	require.NoError(t, waitForRun(t, done), "a stop is a clean shutdown")
	require.False(t, canDial(addr), "the listener must be closed when Run returns")
}

// Run also ends when its context ends, without a call to Stop.
func TestRunReturnsWhenItsContextEnds(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	s := newFakeService(t, newFakeStore(), cfg, testNow)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runInBackground(s, ctx)
	require.Eventually(t, func() bool { return canDial(addr) }, 5*time.Second, time.Millisecond,
		"the server must listen once Run starts")

	cancel()
	require.NoError(t, waitForRun(t, done), "the end of the context is a clean shutdown")
	require.False(t, canDial(addr), "the listener must be closed when Run returns")
}

// A Stop before Run makes Run shut down at once, and it returns nil.
func TestStopBeforeRunMakesRunReturnNil(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	s := newFakeService(t, newFakeStore(), cfg, testNow)

	s.Stop()
	done := runInBackground(s, context.Background())
	require.NoError(t, waitForRun(t, done))
}

// A port that is in use fails the start with the listen error, and nothing is left running.
func TestRunFailsWhenTheAddressIsInUse(t *testing.T) {
	held, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })

	cfg := testConfig()
	cfg.Port = held.Addr().(*net.TCPAddr).Port
	s := newFakeService(t, newFakeStore(), cfg, testNow)

	err = s.Run(context.Background())
	require.ErrorContains(t, err, "listen: ", "the start must fail with the listen error")
}

// countingSchedules counts the calls that reach the schedule store, so that a test can see
// whether a start touched the schedules at all.
type countingSchedules struct {
	ScheduleStore
	calls int
}

func (c *countingSchedules) ListMissed(ctx context.Context, now time.Time, after domain.TaskRef, limit int) ([]domain.ScheduleEntry, error) {
	c.calls++
	return c.ScheduleStore.ListMissed(ctx, now, after, limit)
}

func (c *countingSchedules) Advance(ctx context.Context, ref domain.TaskRef, tick, next time.Time) (bool, error) {
	c.calls++
	return c.ScheduleStore.Advance(ctx, ref, tick, next)
}

// failingListMissed fails every ListMissed the way the store does when its tables are missing.
type failingListMissed struct {
	ScheduleStore
}

func (failingListMissed) ListMissed(context.Context, time.Time, domain.TaskRef, int) ([]domain.ScheduleEntry, error) {
	return nil, fmt.Errorf("list missed schedules: %w", errors.New(`ERROR: relation "schedules" does not exist`))
}

// overdueStore holds one schedule whose tick passed before testNow, so that the startup
// advance has something to move.
func overdueStore() *fakeStore {
	store := newFakeStore()
	store.schedules[testRef] = &fakeSchedule{
		sched: domain.Schedule{Kind: domain.ScheduleKindEvery, Every: time.Minute},
		next:  testNow.Add(-time.Minute),
	}
	return store
}

// With the port taken, the start fails at the listen before the missed ticks are read, so a
// second server changes no schedule row and logs no advance.
func TestRunDoesNotTouchSchedulesWhenTheAddressIsInUse(t *testing.T) {
	held, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })

	store := overdueStore()
	overdue := store.schedules[testRef].next
	schedules := &countingSchedules{ScheduleStore: store}

	cfg := testConfig()
	cfg.Port = held.Addr().(*net.TCPAddr).Port
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	s := newService(cfg, logger, store, store, store, schedules, newFakeSignals(),
		func() time.Time { return testNow }, func() float64 { return 0.5 })

	err = s.Run(context.Background())
	require.ErrorContains(t, err, "listen: ", "the start must fail with the listen error")
	require.Zero(t, schedules.calls, "the schedule store must not be read or written")
	require.Equal(t, overdue, store.schedules[testRef].next, "the overdue tick must not move")
	require.NotContains(t, logs.String(), "missed schedule ticks advanced", "no advance may be logged")
}

// A failure to advance the missed ticks closes the listener, so the port is free again and
// nothing is left serving.
func TestRunClosesTheListenerWhenTheAdvanceFails(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	store := newFakeStore()
	s := newService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, store, store,
		failingListMissed{store}, newFakeSignals(), func() time.Time { return testNow }, func() float64 { return 0.5 })

	err := s.Run(context.Background())
	require.EqualError(t, err, `advance missed schedule ticks: list missed schedules: ERROR: relation "schedules" does not exist`,
		"each layer adds its words once")
	require.False(t, canDial(addr), "the listener must be closed when the start fails")

	relisten, err := net.Listen("tcp", addr)
	require.NoError(t, err, "the port must be free again after the failed start")
	require.NoError(t, relisten.Close())
}

// A Register during the drain stores no Task and no schedule.
func TestRegisterDuringTheDrainStoresNothing(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	e.Stop()
	server := serveForTest(t, e)
	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

	_, err := client.Register(bearerContext(e.cfg.Token), validRegisterRequest())

	require.Error(t, err, "a Register during the drain must be refused")
	assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "connect code")
	require.Empty(t, store.tasks, "a refused Register stores no Task")
	require.Empty(t, store.schedules, "a refused Register stores no schedule")
}

// A worker still finishes the Run it holds during the drain.
func TestFinishRunDuringTheDrainIsAnswered(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	attachInstance(t, e, "i-1", testNow)
	id := store.seedRunning(t, testRef, "tok-1")
	e.Stop()
	e.instances.CloseAll()
	server := serveForTest(t, e)
	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithReadMaxBytes(0))))

	req := &runcoilv1.FinishRunRequest{
		RunId:      id,
		LeaseToken: "tok-1",
		Result:     &runcoilv1.FinishRunRequest_Succeeded{Succeeded: &runcoilv1.RunSucceeded{Output: []byte(`{"ok":true}`)}},
	}
	_, err := client.FinishRun(bearerContext(e.cfg.Token), req)

	require.NoError(t, err, "FinishRun during the drain must be answered")
	run, err := store.GetRun(context.Background(), id)
	require.NoError(t, err, "get run")
	require.Equal(t, domain.RunStatusSucceeded, run.Status, "the Run is finished")
}

// countingRuns is a RunStore whose CountRunning answers the counts in turn, and then the last one.
type countingRuns struct {
	RunStore
	counts []int
	calls  int
}

func (c *countingRuns) CountRunning(context.Context, string) (int, error) {
	n := c.counts[min(c.calls, len(c.counts)-1)]
	c.calls++
	return n, nil
}

// captureLogs sends the service's log lines to the returned buffer.
func captureLogs(s *Service) *bytes.Buffer {
	buf := &bytes.Buffer{}
	s.logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return buf
}

// The drain ends as soon as no Run is held.
func TestWaitForHeldRunsEndsOnceNoRunIsHeld(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	counts := &countingRuns{RunStore: store, counts: []int{2, 1, 0}}
	e.runs = counts
	logs := captureLogs(e)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.waitForHeldRuns(ctx, []string{"i-1"})

	require.NoError(t, ctx.Err(), "the drain must end before its deadline")
	require.Equal(t, 3, counts.calls, "one count per poll")
	require.Contains(t, logs.String(), "drain finished")
	require.NotContains(t, logs.String(), "drain timed out")
}

// The drain gives up at its deadline while a Run is still held, and logs the held count.
func TestWaitForHeldRunsGivesUpAtTheDeadline(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	e.runs = &countingRuns{RunStore: store, counts: []int{1}}
	logs := captureLogs(e)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	e.waitForHeldRuns(ctx, []string{"i-1"})

	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded, "the drain must end at its deadline")
	require.Less(t, time.Since(started), 2*time.Second, "the drain must not wait past its deadline")
	require.Contains(t, logs.String(), "drain timed out")
	require.Contains(t, logs.String(), "held_runs=1")
}

// drainService is a service on a free port, with one attached instance that holds one Run.
func drainService(t *testing.T, shutdownTimeout time.Duration) (*Service, *fakeStore, string) {
	t.Helper()
	cfg := testConfig()
	cfg.Port = freePort(t)
	cfg.ShutdownTimeout = shutdownTimeout
	store := newFakeStore()
	s := newFakeService(t, store, cfg, testNow)
	registerInstance(t, s, "i-1", testNow)
	attachInstance(t, s, "i-1", testNow)
	id := store.seedRunning(t, testRef, "tok-1")
	return s, store, id
}

// getHealth returns the status and the body of a GET of the URL.
func getHealth(url string) (int, string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, "", err
	}

	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

// waitReady waits until the readiness endpoint of the service answers 200.
func waitReady(t *testing.T, s *Service) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/readyz", s.cfg.Port)
	require.Eventually(t, func() bool {
		status, _, err := getHealth(url)
		return err == nil && status == http.StatusOK
	}, 5*time.Second, time.Millisecond, "the readiness endpoint must answer 200 once Run listens")
}

// A stop returns once the attached worker finishes its Run, before the deadline.
func TestRunStopEndsWhenTheHeldRunFinishes(t *testing.T) {
	s, store, id := drainService(t, 10*time.Second)
	logs := captureLogs(s)
	done := runInBackground(s, context.Background())
	waitReady(t, s)

	s.Stop()
	started := time.Now()
	_, err := s.finishRun(context.Background(), id, domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded, Output: []byte(`{}`)})
	require.NoError(t, err, "finish the held run")

	require.NoError(t, waitForRun(t, done), "a stop is a clean shutdown")
	require.Less(t, time.Since(started), 5*time.Second, "the stop must not wait for the deadline")
	require.Contains(t, logs.String(), "drain finished")
	run, err := store.GetRun(context.Background(), id)
	require.NoError(t, err, "get run")
	require.Equal(t, domain.RunStatusSucceeded, run.Status, "the Run is finished")
}

// A Run still held at the deadline does not hold the stop.
func TestRunStopTimesOutWhileARunIsHeld(t *testing.T) {
	s, _, _ := drainService(t, 300*time.Millisecond)
	logs := captureLogs(s)
	done := runInBackground(s, context.Background())
	waitReady(t, s)

	s.Stop()
	started := time.Now()
	require.NoError(t, waitForRun(t, done), "the stop ends at its deadline")
	require.Less(t, time.Since(started), 3*time.Second, "the stop must end near the shutdown timeout")
	require.Contains(t, logs.String(), "drain timed out")
	require.Contains(t, logs.String(), "held_runs=1")
}

// A Listen that attaches once the service is stopping is refused and leaves no stream.
func TestAttachStreamRefusesWhileStopping(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	registerInstance(t, e, "i-1", testNow)
	e.Stop()

	_, err := e.attachStream(context.Background(), "i-1")
	var apiErr *apierr.Error
	require.ErrorAs(t, err, &apiErr, "a stopping service refuses the attach")
	assert.Equal(t, apierr.KindUnavailable, apiErr.Kind, "kind")
	assert.Nil(t, e.instances.StreamOf("i-1"), "the refused stream must not stay attached")
	assert.Empty(t, e.instances.Attached(), "the refused instance must not be listed as attached")
}

// waitDone reports whether done closed before ctx ended.
func TestWaitDoneReportsWhetherItClosedFirst(t *testing.T) {
	done := make(chan struct{})
	close(done)
	require.True(t, waitDone(context.Background(), done), "a closed channel is done")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, waitDone(ctx, make(chan struct{})), "a channel that stays open is not done when ctx ends")
}

// /livez answers 200 in every state; /readyz answers 503 with the reason when not ready.
func TestHealthRoutesReportEachState(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	server := serveForTest(t, e)

	expect := func(path string, status int, body string) {
		t.Helper()
		got, text, err := getHealth(server.URL + path)
		require.NoError(t, err, "GET %s", path)
		require.Equal(t, status, got, "GET %s status", path)
		require.Equal(t, body, text, "GET %s body", path)
	}

	expect("/livez", http.StatusOK, "ok")
	expect("/readyz", http.StatusServiceUnavailable, "starting")

	e.ready.Store(true)
	expect("/readyz", http.StatusOK, "ok")

	e.Stop()
	expect("/readyz", http.StatusServiceUnavailable, "shutting down")
	expect("/livez", http.StatusOK, "ok")
}

// A stop waits for a Run held by a worker whose Listen stream is open as the stop begins.
func TestStopWaitsForARunHeldOnAnOpenListenStream(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	cfg.ShutdownTimeout = 10 * time.Second
	// The client's Listen call returns when the first message arrives, which is a keepalive.
	cfg.HeartbeatInterval = 20 * time.Millisecond
	store := newFakeStore()
	s := newFakeService(t, store, cfg, testNow)
	registerInstance(t, s, "i-1", testNow)
	done := runInBackground(s, context.Background())
	waitReady(t, s)

	// The client speaks HTTP/2 without TLS, as the worker does against the server.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	httpClient := &http.Client{Transport: &http.Transport{Protocols: protocols}}
	url := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	workers := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(httpClient, url, connecthttp.WithReadMaxBytes(0))))
	stream, err := workers.Listen(bearerContext(cfg.Token), &runcoilv1.ListenRequest{InstanceId: "i-1"})
	require.NoError(t, err, "open the stream")
	t.Cleanup(func() { _ = stream.Close() })
	require.Eventually(t, func() bool {
		return slices.Contains(s.instances.Attached(), "i-1")
	}, 5*time.Second, time.Millisecond, "the Listen stream must attach")

	held := store.seedRunning(t, testRef, "tok-1")
	s.Stop()

	select {
	case err := <-done:
		require.FailNow(t, "Run returned while a worker held a Run", "Run returned with %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	_, err = s.finishRun(context.Background(), held, domain.FinishInput{Token: "tok-1", Kind: domain.FinishSucceeded, Output: []byte(`{}`)})
	require.NoError(t, err, "finish the held run")
	require.NoError(t, waitForRun(t, done), "the stop ends once the held Run is finished")
}
