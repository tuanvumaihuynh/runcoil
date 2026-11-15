package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

// fakeRunStore counts the calls of each job. A job with full set returns a full batch
// every time, as a backlog that never ends would.
type fakeRunStore struct {
	store.RunStore

	mu          sync.Mutex
	promotes    int
	expires     int
	fires       int
	fullPromote bool
	promoteErr  error
}

func (f *fakeRunStore) PromoteDueDelayed(_ context.Context, limit int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.promotes++
	if f.promoteErr != nil {
		return 0, f.promoteErr
	}

	if f.fullPromote {
		return limit, nil
	}

	return 0, nil
}

func (f *fakeRunStore) ExpireStartDeadlines(context.Context, int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.expires++

	return 0, nil
}

func (f *fakeRunStore) FireDueSchedules(context.Context, int) (domain.ScheduleTicks, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fires++

	return domain.ScheduleTicks{Fired: 1}, nil
}

func (f *fakeRunStore) calls() (promotes, expires int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.promotes, f.expires
}

// fakeWorkerStore keeps what each sweep of the reaper asked for.
type fakeWorkerStore struct {
	store.WorkerStore

	mu     sync.Mutex
	sweeps int
	ttl    time.Duration
	lost   int
}

func (f *fakeWorkerStore) EndDeadWorkers(_ context.Context, ttl time.Duration, maxLost, _ int) (domain.DeadWorkers, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sweeps++
	f.ttl, f.lost = ttl, maxLost

	return domain.DeadWorkers{Ended: 1, Requeued: 2}, nil
}

func (f *fakeWorkerStore) calls() (sweeps int, ttl time.Duration, maxLost int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.sweeps, f.ttl, f.lost
}

func testConfig() Config {
	return Config{
		RequeueDelayedRunsInterval: 10 * time.Millisecond,
		StartDeadlineCheckInterval: 10 * time.Millisecond,
		DeadWorkerCheckInterval:    10 * time.Millisecond,
		ScheduleCheckInterval:      10 * time.Millisecond,
		MaxLostAttempts:            5,
		WorkerTTL:                  20 * time.Millisecond,
	}
}

func newService(runStore store.RunStore) *Service {
	return New(testConfig(), slog.New(slog.DiscardHandler), runStore, &fakeWorkerStore{})
}

// start runs s until the test ends, and returns the channel Run's error arrives on.
func start(t *testing.T, s *Service) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	t.Cleanup(s.Stop)

	return done
}

func TestRunCallsEachJobOncePerTick(t *testing.T) {
	store := &fakeRunStore{fullPromote: true}
	s := newService(store)
	start(t, s)

	time.Sleep(105 * time.Millisecond)
	promotes, expires := store.calls()

	assert.LessOrEqual(t, promotes, 11, "a full batch waits for the next tick")
	assert.GreaterOrEqual(t, promotes, 5)
	assert.GreaterOrEqual(t, expires, 5, "a backlog of delayed Runs does not hold up the start deadline check")
}

func TestStopEndsRunDuringABacklog(t *testing.T) {
	store := &fakeRunStore{fullPromote: true}
	s := newService(store)
	done := start(t, s)

	require.Eventually(t, func() bool { p, _ := store.calls(); return p >= 2 }, time.Second, 5*time.Millisecond)

	s.Stop()
	s.Stop()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "Run did not return after Stop")
	}
}

func TestAFailedJobIsTriedAgainAtTheNextTick(t *testing.T) {
	store := &fakeRunStore{promoteErr: errors.New("db down")}
	s := newService(store)
	start(t, s)

	require.Eventually(t, func() bool { p, e := store.calls(); return p >= 3 && e >= 3 }, time.Second, 5*time.Millisecond,
		"a failed requeue is tried at each tick, and the start deadline check still runs")
}

func TestTheReaperEndsDeadWorkersWithTheWorkerTTL(t *testing.T) {
	workers := &fakeWorkerStore{}
	s := New(testConfig(), slog.New(slog.DiscardHandler), &fakeRunStore{}, workers)
	start(t, s)

	require.Eventually(t, func() bool { n, _, _ := workers.calls(); return n >= 2 }, time.Second, 5*time.Millisecond)

	_, ttl, maxLost := workers.calls()
	assert.Equal(t, 20*time.Millisecond, ttl)
	assert.Equal(t, 5, maxLost)
}

func TestTheReaperWaitsOneTTLAfterTheStart(t *testing.T) {
	cfg := testConfig()
	cfg.WorkerTTL = time.Hour
	workers := &fakeWorkerStore{}
	s := New(cfg, slog.New(slog.DiscardHandler), &fakeRunStore{}, workers)
	start(t, s)

	time.Sleep(60 * time.Millisecond)

	sweeps, _, _ := workers.calls()
	assert.Zero(t, sweeps, "the heartbeats of the workers are stale right after a restart")
}

func TestSchedulesFireAtEachTick(t *testing.T) {
	store := &fakeRunStore{fullPromote: true}
	s := newService(store)
	start(t, s)

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()

		return store.fires >= 3
	}, time.Second, 5*time.Millisecond, "a backlog of delayed Runs does not hold up the schedules")
}

func TestReady(t *testing.T) {
	s := newService(&fakeRunStore{})
	require.Error(t, s.Ready(context.Background()), "not running before Run")

	done := start(t, s)
	require.Eventually(t, func() bool { return s.Ready(context.Background()) == nil }, time.Second, 5*time.Millisecond)

	s.Stop()
	require.NoError(t, <-done)
	assert.Error(t, s.Ready(context.Background()), "not running after Stop")
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		want   string
	}{
		{name: "valid", modify: func(*Config) {}},
		{name: "no lost attempts allowed", modify: func(c *Config) { c.MaxLostAttempts = 0 }},
		{name: "requeue", modify: func(c *Config) { c.RequeueDelayedRunsInterval = 0 }, want: "requeue delayed runs interval must be greater than 0"},
		{name: "start deadline", modify: func(c *Config) { c.StartDeadlineCheckInterval = 0 }, want: "start deadline check interval must be greater than 0"},
		{name: "dead worker", modify: func(c *Config) { c.DeadWorkerCheckInterval = 0 }, want: "dead worker check interval must be greater than 0"},
		{name: "schedule", modify: func(c *Config) { c.ScheduleCheckInterval = 0 }, want: "schedule check interval must be greater than 0"},
		{name: "lost attempts negative", modify: func(c *Config) { c.MaxLostAttempts = -1 }, want: "max lost attempts must be between 0 and 100"},
		{name: "lost attempts too many", modify: func(c *Config) { c.MaxLostAttempts = 101 }, want: "max lost attempts must be between 0 and 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			tt.modify(&cfg)

			if tt.want == "" {
				assert.NoError(t, cfg.Validate())
				return
			}

			assert.EqualError(t, cfg.Validate(), tt.want)
		})
	}
}
