package worker

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runInBackground runs the service with a background context, and returns the channel that
// receives its error.
func runInBackground(svc *Service) <-chan error {
	done := make(chan error, 1)
	go func() { done <- svc.Run(context.Background()) }()
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

func TestRunRegistersTheTwoCleanupJobsAndStopsThem(t *testing.T) {
	store := newFakeRuns()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{Cleanup: CleanupConfig{Interval: 5 * time.Millisecond, RunRetention: time.Hour}}
	svc := NewService(cfg, logger, store)

	done := runInBackground(svc)

	require.Eventually(t, func() bool {
		runs, keys := store.ran()
		return runs && keys
	}, 5*time.Second, time.Millisecond, "both cleanup jobs must run")

	// The jobs that Run registers, read from a scheduler built the same way. It is not
	// started, so it runs nothing.
	scheduler, err := newScheduler(svc.cleanup)
	require.NoError(t, err)
	names := make([]string, 0, 2)
	for _, job := range scheduler.Jobs() {
		names = append(names, job.Name())
	}
	require.NoError(t, scheduler.Shutdown())
	assert.ElementsMatch(t, []string{"runs.cleanup", "idempotency-keys.cleanup"}, names, "exactly the two cleanup jobs")

	store.addKeys(1, time.Now().Add(-time.Minute))
	require.Eventually(t, func() bool { return store.keyCount() == 0 }, 5*time.Second, time.Millisecond,
		"the keys cleanup must keep running on its interval")

	svc.Stop()
	require.NoError(t, waitForRun(t, done))

	stopped := store.calls()
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, stopped, store.calls(), "no job runs after the scheduler stops")
}

// Each cleanup job runs at start, not only on its interval. With an interval of an hour,
// the first pass is the only one the test can see.
func TestEachCleanupJobRunsOnceAtStart(t *testing.T) {
	store := newFakeRuns()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{Cleanup: CleanupConfig{Interval: time.Hour, RunRetention: time.Hour}}
	svc := NewService(cfg, logger, store)

	done := runInBackground(svc)
	require.Eventually(t, func() bool {
		runs, keys := store.ran()
		return runs && keys
	}, 5*time.Second, time.Millisecond, "both cleanup jobs must run at start, without waiting an interval")

	svc.Stop()
	require.NoError(t, waitForRun(t, done))

	runCalls, keyCalls := store.callCounts()
	assert.Equal(t, 1, runCalls, "the runs cleanup runs once at start")
	assert.Equal(t, 1, keyCalls, "the keys cleanup runs once at start")
}

// A cleanup job does not start a pass while its previous pass is still running, and the
// other job is not held up by it. The runs cleanup blocks until the shutdown, so every
// later pass of it would be one more call that is still running.
func TestCleanupJobDoesNotOverlapItself(t *testing.T) {
	store := &blockingRuns{fakeRuns: newFakeRuns(), started: make(chan struct{}, 1)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{Cleanup: CleanupConfig{Interval: 5 * time.Millisecond, RunRetention: time.Hour}}
	svc := NewService(cfg, logger, store)

	done := runInBackground(svc)
	require.Eventually(t, func() bool { return store.entered.Load() >= 1 }, 5*time.Second, time.Millisecond,
		"the runs cleanup must start")
	require.Eventually(t, func() bool {
		_, keyCalls := store.callCounts()
		return keyCalls >= 10
	}, 5*time.Second, time.Millisecond, "the keys cleanup must keep running while the runs cleanup is blocked")

	assert.Equal(t, int32(1), store.entered.Load(), "no second pass of the runs cleanup starts while the first is running")

	svc.Stop()
	require.NoError(t, waitForRun(t, done))
}

func TestRunFailsWhenAJobCannotBeRegistered(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// A zero interval is refused by the scheduler. Run does not validate the configuration, so this reaches it.
	cfg := Config{Cleanup: CleanupConfig{Interval: 0, RunRetention: time.Hour}}
	svc := NewService(cfg, logger, newFakeRuns())

	err := svc.Run(context.Background())
	require.ErrorIs(t, err, gocron.ErrDurationJobIntervalZero)
}

// blockingRuns is a RunStore whose cleanup of Runs does not return until its context ends.
// entered counts the calls that have started.
type blockingRuns struct {
	*fakeRuns
	started chan struct{}
	entered atomic.Int32
}

func (b *blockingRuns) DeleteFinishedBefore(ctx context.Context, _ time.Time, _ int) (int, error) {
	b.entered.Add(1)
	select {
	case b.started <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return 0, ctx.Err()
}

// A running job's context ends when Stop is called, so the shutdown does not wait for the
// bound: the job returns at once, and Run returns nil.
func TestStopEndsTheContextOfARunningJob(t *testing.T) {
	store := &blockingRuns{fakeRuns: newFakeRuns(), started: make(chan struct{}, 1)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(testCleanupConfig(), logger, store)

	done := runInBackground(svc)
	select {
	case <-store.started:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the cleanup of Runs did not start")
	}

	svc.Stop()
	require.NoError(t, waitForRun(t, done), "the running job must end with its context")
}

// A Stop before Run makes Run shut the scheduler down at once, and it returns nil.
func TestStopBeforeRunMakesRunReturnNil(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(testCleanupConfig(), logger, newFakeRuns())

	svc.Stop()
	require.NoError(t, waitForRun(t, runInBackground(svc)))
}

func TestConfigValidateChecksBothCleanupSettings(t *testing.T) {
	tests := []struct {
		name    string
		cleanup CleanupConfig
		wantErr string
	}{
		{
			name:    "valid",
			cleanup: CleanupConfig{Interval: time.Hour, RunRetention: 168 * time.Hour},
		},
		{
			name:    "zero interval",
			cleanup: CleanupConfig{Interval: 0, RunRetention: 168 * time.Hour},
			wantErr: "cleanup interval must be greater than 0",
		},
		{
			name:    "zero run retention",
			cleanup: CleanupConfig{Interval: time.Hour, RunRetention: 0},
			wantErr: "run retention must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Cleanup: tt.cleanup}
			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.EqualError(t, err, tt.wantErr)
		})
	}
}
