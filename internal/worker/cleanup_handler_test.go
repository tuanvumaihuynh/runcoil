package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return testNow }

// fakeRun is a Run that the fake store holds: finished, at a time, or still running.
type fakeRun struct {
	finished   bool
	finishedAt time.Time
}

// fakeRuns is a RunStore that holds Runs and idempotency keys, and records the calls the
// cleanup makes. A failure set on it is returned by every call of that method.
type fakeRuns struct {
	mu   sync.Mutex
	runs map[string]fakeRun
	// keys holds the expiry of each idempotency key.
	keys map[string]time.Time
	// cutoffs and limits are the arguments of each DeleteFinishedBefore call.
	cutoffs []time.Time
	limits  []int
	// pruneLimits are the limits of each PruneKeys call.
	pruneLimits []int
	deleteErr   error
	pruneErr    error
}

func newFakeRuns() *fakeRuns {
	return &fakeRuns{runs: make(map[string]fakeRun), keys: make(map[string]time.Time)}
}

// DeleteFinishedBefore removes up to limit Runs that finished before cutoff.
func (f *fakeRuns) DeleteFinishedBefore(_ context.Context, cutoff time.Time, limit int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}

	n := 0
	for id, run := range f.runs {
		if n == limit {
			break
		}
		if !run.finished || !run.finishedAt.Before(cutoff) {
			continue
		}

		delete(f.runs, id)
		n++
	}

	return n, nil
}

// PruneKeys removes up to limit keys whose expiry has passed at now.
func (f *fakeRuns) PruneKeys(_ context.Context, now time.Time, limit int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLimits = append(f.pruneLimits, limit)
	if f.pruneErr != nil {
		return 0, f.pruneErr
	}

	n := 0
	for key, expiresAt := range f.keys {
		if n == limit {
			break
		}
		if !expiresAt.After(now) {
			delete(f.keys, key)
			n++
		}
	}

	return n, nil
}

// addFinished puts n Runs that finished at the given time into the store.
func (f *fakeRuns) addFinished(n int, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range n {
		f.runs[fmt.Sprintf("finished-%d-%d", at.UnixNano(), i)] = fakeRun{finished: true, finishedAt: at}
	}
}

// addKeys puts n idempotency keys that expire at the given time into the store.
func (f *fakeRuns) addKeys(n int, expiresAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range n {
		f.keys[fmt.Sprintf("key-%d-%d", expiresAt.UnixNano(), i)] = expiresAt
	}
}

// ran reports whether the runs cleanup and the keys cleanup have each reached the store.
func (f *fakeRuns) ran() (runs, keys bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cutoffs) > 0, len(f.pruneLimits) > 0
}

// calls returns how many cleanup calls the store has received.
func (f *fakeRuns) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.limits) + len(f.pruneLimits)
}

// callCounts returns how many DeleteFinishedBefore calls and PruneKeys calls the store has received.
func (f *fakeRuns) callCounts() (runs, keys int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.limits), len(f.pruneLimits)
}

func (f *fakeRuns) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func (f *fakeRuns) keyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

func testCleanupConfig() Config {
	return Config{Cleanup: CleanupConfig{Interval: time.Hour, RunRetention: 7 * 24 * time.Hour}}
}

func newTestHandler(logs *bytes.Buffer, runs RunStore) *cleanupHandler {
	logger := slog.New(slog.NewTextHandler(logs, nil))
	return newCleanupHandler(testCleanupConfig(), logger, runs, fixedClock)
}

func TestRunsCleanupRemovesOnlyRunsPastRetention(t *testing.T) {
	store := newFakeRuns()
	store.runs["old"] = fakeRun{finished: true, finishedAt: testNow.Add(-8 * 24 * time.Hour)}
	store.runs["recent"] = fakeRun{finished: true, finishedAt: testNow.Add(-6 * 24 * time.Hour)}
	store.runs["running"] = fakeRun{}
	h := newTestHandler(new(bytes.Buffer), store)

	h.cleanupRuns(context.Background())

	wantCutoffs := []time.Time{testNow.Add(-7 * 24 * time.Hour)}
	require.Len(t, store.cutoffs, len(wantCutoffs), "one cut-off per call of DeleteFinishedBefore")
	for i, want := range wantCutoffs {
		assert.True(t, want.Equal(store.cutoffs[i]), "the cut-off is the retention period before now: want %v, got %v", want, store.cutoffs[i])
	}
	require.NotContains(t, store.runs, "old", "a Run past retention must be removed")
	require.Contains(t, store.runs, "recent", "a Run inside retention is kept")
	require.Contains(t, store.runs, "running", "a running Run is never removed")
}

func TestRunsCleanupBatchesUntilEmpty(t *testing.T) {
	store := newFakeRuns()
	store.addFinished(1203, testNow.Add(-8*24*time.Hour))
	h := newTestHandler(new(bytes.Buffer), store)

	h.cleanupRuns(context.Background())

	require.Zero(t, store.runCount(), "every expired Run is removed across the batches")
	require.Equal(t, []int{500, 500, 500}, store.limits, "a batch is 500, and the loop stops at the first short batch")
}

func TestKeysCleanupPrunesExpiredKeysInBatches(t *testing.T) {
	store := newFakeRuns()
	store.addKeys(1203, testNow.Add(-time.Minute))
	store.addKeys(5, testNow.Add(time.Hour))
	h := newTestHandler(new(bytes.Buffer), store)

	h.cleanupKeys(context.Background())

	require.Equal(t, 5, store.keyCount(), "only the keys whose window has passed are removed")
	require.Equal(t, []int{500, 500, 500}, store.pruneLimits, "a batch is 500, and the loop stops at the first short batch")
}

func TestRunsCleanupFailureDoesNotStopTheKeysCleanup(t *testing.T) {
	store := newFakeRuns()
	store.deleteErr = errors.New("database is down")
	store.addKeys(2, testNow.Add(-time.Minute))
	logs := new(bytes.Buffer)
	h := newTestHandler(logs, store)

	h.cleanupRuns(context.Background())
	h.cleanupKeys(context.Background())

	require.Len(t, store.cutoffs, 1, "the runs cleanup tried once and failed")
	require.Zero(t, store.keyCount(), "the keys cleanup still ran")
	require.Contains(t, logs.String(), `msg="runs cleanup failed"`)
	require.Contains(t, logs.String(), "database is down")
}

func TestKeysCleanupFailureDoesNotStopTheRunsCleanup(t *testing.T) {
	store := newFakeRuns()
	store.pruneErr = errors.New("database is down")
	store.addFinished(3, testNow.Add(-8*24*time.Hour))
	logs := new(bytes.Buffer)
	h := newTestHandler(logs, store)

	h.cleanupKeys(context.Background())
	h.cleanupRuns(context.Background())

	require.Len(t, store.pruneLimits, 1, "the keys cleanup tried once and failed")
	require.Zero(t, store.runCount(), "the runs cleanup still ran")
	require.Contains(t, logs.String(), `msg="idempotency keys cleanup failed"`)
}

func TestCleanupLogsOnlyWhatItRemoved(t *testing.T) {
	store := newFakeRuns()
	logs := new(bytes.Buffer)
	h := newTestHandler(logs, store)

	h.cleanupRuns(context.Background())
	h.cleanupKeys(context.Background())
	require.Empty(t, logs.String(), "a pass that removes nothing logs nothing")

	store.addFinished(2, testNow.Add(-8*24*time.Hour))
	store.addKeys(1, testNow.Add(-time.Minute))
	h.cleanupRuns(context.Background())
	h.cleanupKeys(context.Background())

	assert.Contains(t, logs.String(), `msg="runs cleaned up" runs_removed=2`)
	assert.Contains(t, logs.String(), `msg="idempotency keys cleaned up" keys_pruned=1`)
}

func TestCleanupFailureIsNotLoggedOnceTheContextEnds(t *testing.T) {
	store := newFakeRuns()
	store.deleteErr = errors.New("context canceled by shutdown")
	store.pruneErr = errors.New("context canceled by shutdown")
	logs := new(bytes.Buffer)
	h := newTestHandler(logs, store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.cleanupRuns(ctx)
	h.cleanupKeys(ctx)

	require.Empty(t, logs.String(), "a failure caused by shutdown is not an error to log")
}
