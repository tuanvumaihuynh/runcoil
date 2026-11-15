package dispatcher

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/store"
	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/proto/gen/go/runcoil/v1/runcoilv1connect"
)

var validRetry = RetryConfig{MaxAttempts: 4, Delays: []time.Duration{time.Second}, Jitter: 0.2}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "defaults", cfg: Config{Port: 7070, ClaimInterval: time.Second, HeartbeatInterval: 10 * time.Second, WorkerTTL: 30 * time.Second, Retry: validRetry}},
		{name: "port out of range", cfg: Config{Port: 70000, ClaimInterval: time.Second, HeartbeatInterval: time.Second, WorkerTTL: 2 * time.Second}, want: "port must be between 1 and 65535"},
		{name: "no claim interval", cfg: Config{Port: 7070, HeartbeatInterval: time.Second, WorkerTTL: 2 * time.Second}, want: "claim interval must be greater than 0"},
		{name: "no heartbeat interval", cfg: Config{Port: 7070, ClaimInterval: time.Second, WorkerTTL: time.Second}, want: "heartbeat interval must be greater than 0"},
		{name: "ttl too short", cfg: Config{Port: 7070, ClaimInterval: time.Second, HeartbeatInterval: time.Second, WorkerTTL: time.Second}, want: "worker ttl must be longer than the heartbeat interval"},
		{name: "retry attempts", cfg: Config{Port: 7070, ClaimInterval: time.Second, HeartbeatInterval: time.Second, WorkerTTL: 2 * time.Second, Retry: RetryConfig{MaxAttempts: 31, Delays: []time.Duration{time.Second}}}, want: "retry: policy: max attempts must be between 0 and 30"},
		{name: "retry delays", cfg: Config{Port: 7070, ClaimInterval: time.Second, HeartbeatInterval: time.Second, WorkerTTL: 2 * time.Second, Retry: RetryConfig{MaxAttempts: 4}}, want: "retry: delays must list at least one duration"},
		{name: "retry jitter", cfg: Config{Port: 7070, ClaimInterval: time.Second, HeartbeatInterval: time.Second, WorkerTTL: 2 * time.Second, Retry: RetryConfig{MaxAttempts: 4, Delays: []time.Duration{time.Second}, Jitter: 2}}, want: "retry: jitter must be between 0 and 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == "" {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, tt.want)
		})
	}
}

type fakeWorkerStore struct {
	store.WorkerStore
}

func (fakeWorkerStore) BeatDispatcher(context.Context, uuid.UUID) error { return nil }

func newTestService(t *testing.T, port int) *Service {
	t.Helper()

	cfg := Config{Port: port, ClaimInterval: time.Hour, HeartbeatInterval: time.Hour, WorkerTTL: 2 * time.Hour}
	return New(cfg, slog.New(slog.DiscardHandler), fakeWorkerStore{}, nil)
}

// start serves s on a listener of its own and returns its base URL and the result of serve.
func start(t *testing.T, s *Service) (string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- s.serve(context.Background(), listener) }()

	require.Eventually(t, func() bool { return s.Ready(context.Background()) == nil }, 5*time.Second, 10*time.Millisecond)

	return "http://" + listener.Addr().String(), done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "serve did not return")
		return nil
	}
}

func TestReady(t *testing.T) {
	s := newTestService(t, 7070)
	assert.EqualError(t, s.Ready(context.Background()), "not serving")

	_, done := start(t, s)
	assert.NoError(t, s.Ready(context.Background()))

	s.Stop()
	assert.EqualError(t, s.Ready(context.Background()), "shutting down")
	assert.NoError(t, wait(t, done))
}

func TestStopShutsTheServerDown(t *testing.T) {
	s := newTestService(t, 7070)
	base, done := start(t, s)

	s.Stop()
	require.NoError(t, wait(t, done))

	_, err := h2Client().Get(base + "/")
	assert.Error(t, err)
}

func h2Client() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Client{Transport: &http.Transport{Protocols: protocols}}
}

func TestHTTP1IsRefused(t *testing.T) {
	s := newTestService(t, 7070)
	base, done := start(t, s)
	defer func() {
		s.Stop()
		assert.NoError(t, wait(t, done))
	}()

	resp, err := http.Get(base + "/")
	if err == nil {
		resp.Body.Close()
	}

	assert.Error(t, err)
}

func TestUnencryptedHTTP2(t *testing.T) {
	s := newTestService(t, 7070)
	base, done := start(t, s)
	defer func() {
		s.Stop()
		assert.NoError(t, wait(t, done))
	}()

	resp, err := h2Client().Get(base + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 2, resp.ProtoMajor)
}

func TestRunListenError(t *testing.T) {
	s := newTestService(t, 70000)

	assert.ErrorContains(t, s.Run(context.Background()), "listen")
}

func TestAMessageOverTheReadLimitIsRefused(t *testing.T) {
	s := newTestService(t, 7070)
	base, done := start(t, s)
	defer func() {
		s.Stop()
		assert.NoError(t, wait(t, done))
	}()

	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(h2Client(), base)))
	_, err := client.CompleteStep(context.Background(), &runcoilv1.CompleteStepRequest{
		Lease:   &runcoilv1.Lease{RunId: uuid.New().String(), Epoch: 1},
		StepId:  "big",
		Outcome: &runcoilv1.CompleteStepRequest_Output{Output: make([]byte, readMaxBytes+1)},
	})

	assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
}

func TestRequestRulesRefuseALongStepID(t *testing.T) {
	s := newTestService(t, 7070)
	base, done := start(t, s)
	defer func() {
		s.Stop()
		assert.NoError(t, wait(t, done))
	}()

	client := runcoilv1connect.NewWorkerServiceClient(connect.NewClient(connecthttp.NewTransport(h2Client(), base)))
	step := func(id string) error {
		_, err := client.CompleteStep(context.Background(), &runcoilv1.CompleteStepRequest{
			Lease:   &runcoilv1.Lease{RunId: uuid.New().String(), Epoch: 1},
			StepId:  id,
			Outcome: &runcoilv1.CompleteStepRequest_Output{Output: []byte(`1`)},
		})

		return err
	}

	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(step(strings.Repeat("x", 256))))
	assert.NotEqual(t, connect.CodeInvalidArgument, connect.CodeOf(step(strings.Repeat("x", 255))), "a step id of 255 bytes passes the rules")
}
