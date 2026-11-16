package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/pkg/configutil"
	"github.com/runcoil/runcoil/pkg/healthutil"
)

type fakePinger struct {
	err   error
	calls atomic.Int32
}

func (p *fakePinger) Ping(ctx context.Context) error {
	p.calls.Add(1)

	if _, ok := ctx.Deadline(); !ok {
		return errors.New("ping without a deadline")
	}

	return p.err
}

func newTestService(t *testing.T, db *fakePinger) *Service {
	t.Helper()

	checks := healthutil.New()
	checks.Add("postgres", db.Ping)

	s, err := New(Config{Port: 8080}, slog.New(slog.DiscardHandler), checks, nil, nil, nil)
	require.NoError(t, err)

	return s
}

func get(s *Service, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.newHTTPServer().Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec
}

func TestConfig(t *testing.T) {
	var cfg Config
	require.NoError(t, configutil.Load("test", &cfg, nil, func(string) (string, bool) { return "", false }))

	assert.Equal(t, 8080, cfg.Port)
	assert.NoError(t, cfg.Validate())

	for _, port := range []int{0, -1, 70000} {
		assert.EqualError(t, (&Config{Port: port}).Validate(), "port must be between 1 and 65535", port)
	}
}

func TestLivez(t *testing.T) {
	db := &fakePinger{err: errors.New("down")}

	rec := get(newTestService(t, db), "/livez")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, db.calls.Load())
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		stopping bool
		want     int
	}{
		{name: "healthy", want: http.StatusOK},
		{name: "a check fails", err: errors.New("down"), want: http.StatusServiceUnavailable},
		{name: "stopping", stopping: true, want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestService(t, &fakePinger{err: tt.err})
			if tt.stopping {
				s.Stop()
			}

			rec := get(s, "/readyz")

			assert.Equal(t, tt.want, rec.Code)
		})
	}
}

// start serves s on a listener of its own and returns its base URL and the result of serve.
func start(t *testing.T, ctx context.Context, s *Service) (string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, listener) }()

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

func fetch(url string) (int, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

func TestStopShutsTheServerDown(t *testing.T) {
	s := newTestService(t, &fakePinger{})
	base, done := start(t, context.Background(), s)

	code, err := fetch(base + "/readyz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, code)

	s.Stop()

	require.NoError(t, wait(t, done))

	_, err = fetch(base + "/livez")
	assert.Error(t, err)
}

func TestContextEndShutsTheServerDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, done := start(t, ctx, newTestService(t, &fakePinger{}))

	cancel()

	require.NoError(t, wait(t, done))
}

func TestStopBeforeRun(t *testing.T) {
	s := newTestService(t, &fakePinger{})
	s.Stop()

	_, done := start(t, context.Background(), s)

	assert.NoError(t, wait(t, done))
}

func TestRunListenError(t *testing.T) {
	s, err := New(Config{Port: 70000}, slog.New(slog.DiscardHandler), healthutil.New(), nil, nil, nil)
	require.NoError(t, err)

	assert.ErrorContains(t, s.Run(context.Background()), "listen")
}

func TestSwagger(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		h := newAPIServerWithConfig(t, Config{Port: 8080, Swagger: enabled}, &fakeAccountService{user: testUser})

		for _, path := range []string{"/docs", "/docs/openapi.yaml"} {
			rec := do(t, h, call{method: http.MethodGet, path: path})

			if enabled {
				assert.Equal(t, http.StatusOK, rec.Code, path)
			} else {
				assert.Equal(t, http.StatusNotFound, rec.Code, path)
			}
		}
	}
}
