package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	runcoilv1 "github.com/tuanvumaihuynh/runcoil-proto/gen/runcoil/v1"
	"github.com/tuanvumaihuynh/runcoil/internal/apierr"
	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// newClockedService builds an engine whose clock the test moves.
func newClockedService(t *testing.T, store *fakeStore, clk *fakeClock) *Service {
	t.Helper()
	return newService(
		testConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		store,
		store,
		store,
		newFakeSignals(),
		clk.Now,
		func() float64 { return 0.5 },
	)
}

func TestInvokeWithTheSameKeyAndInputIsDeduplicated(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	ctx := context.Background()

	first, dup, err := e.invoke(ctx, testRef, "order-1", []byte(`{"n":1}`))
	require.NoError(t, err, "first invoke: id=%v dup=%v", first, dup)
	require.False(t, dup, "first invoke: id=%v dup=%v", first, dup)

	second, dup, err := e.invoke(ctx, testRef, "order-1", []byte(`{"n":1}`))
	require.NoError(t, err, "repeat invoke")
	require.Equal(t, first, second, "repeat: want %v deduplicated", first)
	require.True(t, dup, "repeat: want %v deduplicated", first)
	require.Len(t, store.runs, 1, "a repeat must create no Run")
}

func TestInvokeWithTheSameKeyAndDifferentInputConflicts(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	ctx := context.Background()

	first, _, err := e.invoke(ctx, testRef, "order-1", []byte(`{"n":1}`))
	require.NoError(t, err, "first invoke")

	_, _, err = e.invoke(ctx, testRef, "order-1", []byte(`{"n":2}`))
	var apiErr *apierr.Error
	require.ErrorAs(t, err, &apiErr, "err = %v, want IDEMPOTENCY_CONFLICT", err)
	require.Equal(t, apierr.CodeIdempotencyConflict, apiErr.Code)
	require.Equal(t, first, apiErr.Details["run_id"], "conflict names the existing run")
}

// A key is held for the window. After it, the same key is a new submission.
func TestExpiredKeyCountsAsAbsent(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	clk := newFakeClock(testNow)
	e := newClockedService(t, store, clk)
	ctx := context.Background()

	first, _, err := e.invoke(ctx, testRef, "order-1", []byte("x"))
	require.NoError(t, err, "first")

	clk.Set(testNow.Add(24*time.Hour - time.Second))
	held, _, _ := e.invoke(ctx, testRef, "order-1", []byte("x"))
	require.Equal(t, first, held, "inside the window the key is held")

	clk.Set(testNow.Add(24 * time.Hour))
	again, dup, err := e.invoke(ctx, testRef, "order-1", []byte("x"))
	require.NoError(t, err, "after the window")
	require.False(t, dup, "after the window: want a new Run")
	require.NotEqual(t, first, again, "after the window: want a new Run")
}

func TestInvokeRequiresAKeyOfAtMost255Characters(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	_, _, err := e.invoke(context.Background(), testRef, "", nil)
	require.Equal(t, apierr.CodeInvalidRequest, engineCode(err), "empty key: err = %v", err)

	long := make([]byte, 256)
	for i := range long {
		long[i] = 'k'
	}

	_, _, err = e.invoke(context.Background(), testRef, string(long), nil)
	require.Equal(t, apierr.CodeInvalidRequest, engineCode(err), "256 characters: err = %v", err)
}

// An Invoke with an empty worker_id or task_id is refused before the Task is looked up.
// A request with no ID at all is the same case as both IDs empty.
func TestInvokeRefusesAnEmptyWorkerIDOrTaskID(t *testing.T) {
	h := runHandler{s: newTestService(t, newFakeStore())}
	cases := []struct {
		name     string
		workerID string
		taskID   string
	}{
		{name: "empty worker_id", workerID: "", taskID: "hello"},
		{name: "empty task_id", workerID: "demo", taskID: ""},
		{name: "no IDs", workerID: "", taskID: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := h.Invoke(context.Background(), &runcoilv1.InvokeRequest{
				WorkerId:       c.workerID,
				TaskId:         c.taskID,
				IdempotencyKey: "k-1",
			})
			var apiErr *apierr.Error
			require.ErrorAs(t, err, &apiErr, "err = %v, want INVALID_REQUEST", err)
			require.Equal(t, apierr.KindInvalid, apiErr.Kind, "kind")
			require.Equal(t, apierr.CodeInvalidRequest, apiErr.Code, "code")
			require.Equal(t, "worker_id and task_id are required", apiErr.Message, "message")
		})
	}
}

func TestGetRunByKeyFollowsTheKey(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	ctx := context.Background()

	id, _, err := e.invoke(ctx, testRef, "order-1", []byte("x"))
	require.NoError(t, err, "invoke")

	run, _, err := e.getRunByKey(ctx, "order-1", false)
	require.NoError(t, err, "by key")
	require.Equal(t, id, run.ID, "by key")

	_, _, err = e.getRunByKey(ctx, "never-used", false)
	require.Equal(t, apierr.CodeRunNotFound, engineCode(err), "unknown key: err = %v, want RUN_NOT_FOUND", err)
}

// A Run that the store cannot create does not get a key: the error is the store's, and
// the key is not left behind.
func TestAnnouncementFailureDoesNotFailInvoke(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newService(
		testConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		store,
		failingReady{store},
		store,
		newFakeSignals(),
		func() time.Time { return testNow },
		func() float64 { return 0.5 },
	)

	id, _, err := e.invoke(context.Background(), testRef, "order-1", []byte("x"))
	require.NoError(t, err, "a failed announcement must not fail the committed Invoke")

	run, err := store.GetRun(context.Background(), id)
	require.NoError(t, err, "the Run is committed")
	require.Equal(t, domain.RunStatusQueued, run.Status, "the Run is committed and queued")
}

// failingReady is a queue whose announcement fails; claims still come from the store.
type failingReady struct {
	*fakeStore
}

func (failingReady) Ready(context.Context, string, string, time.Time) error {
	return errors.New("announce failed")
}

// seedFinished puts a finished Run with the given Steps into the store.
func seedFinished(t *testing.T, store *fakeStore, status domain.RunStatus, steps ...domain.StepRecord) string {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	runID := uuid.NewString()
	store.runs[runID] = domain.Run{
		ID: runID, WorkerID: testRef.workerID, TaskID: testRef.taskID, Status: status, Source: domain.RunSourceInvoke,
		Input: []byte(`{"order":7}`), Attempts: 1, DueAt: testNow, CreatedAt: testNow,
		StartedAt: testNow, FinishedAt: testNow.Add(time.Minute),
	}

	store.steps[runID] = steps
	return runID
}

func TestRerunFromTheStartCopiesNoSteps(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	original := seedFinished(t, store, domain.RunStatusSucceeded,
		domain.StepRecord{StepID: "reserve", Position: 1, Status: domain.StepStatusSucceeded, Output: []byte(`"r"`)})

	id, dup, err := e.rerun(context.Background(), original, "", "rerun-1")
	require.NoError(t, err, "rerun")
	require.False(t, dup, "rerun")

	run, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunSourceRerun, run.Source, "rerun source")
	require.Equal(t, original, run.RerunOf, "rerun_of")
	require.Equal(t, `{"order":7}`, string(run.Input), "the same input")
	require.Equal(t, domain.RunStatusQueued, run.Status, "status")
	steps, _ := store.ListSteps(context.Background(), id)
	require.Empty(t, steps, "a rerun from the start copies no Steps")
}

func TestRerunFromAStepCopiesTheEarlierRecords(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	original := seedFinished(t, store, domain.RunStatusFailed,
		domain.StepRecord{StepID: "reserve", Position: 1, Status: domain.StepStatusSucceeded, Output: []byte(`"r"`)},
		domain.StepRecord{StepID: "charge#1", Position: 2, Status: domain.StepStatusFailed,
			Failure: &domain.Failure{Code: domain.FailureCodeError, Message: "declined"}},
		domain.StepRecord{StepID: "charge#2", Position: 3, Status: domain.StepStatusSucceeded, Output: []byte(`"c"`)},
	)

	id, _, err := e.rerun(context.Background(), original, "charge#2", "rerun-2")
	require.NoError(t, err, "rerun from step")

	steps, _ := store.ListSteps(context.Background(), id)
	require.Len(t, steps, 2, "copied steps: reserve and charge#1 before the selected one")
	require.Equal(t, "reserve", steps[0].StepID, "copied steps")
	require.Equal(t, "charge#1", steps[1].StepID, "copied steps")
	require.Equal(t, domain.StepStatusFailed, steps[1].Status, "copied records keep their status, numbered from 1")
	require.Equal(t, 1, steps[0].Position, "copied records keep their status, numbered from 1")
	require.Equal(t, 2, steps[1].Position, "copied records keep their status, numbered from 1")
}

func TestRerunRefusesAnUnfinishedRunAndAnUnknownStep(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	ctx := context.Background()

	queued := store.seedQueued(t, testRef, testNow)
	_, _, err := e.rerun(ctx, queued, "", "rerun-3")
	require.Equal(t, apierr.CodeRunNotFinished, engineCode(err), "unfinished: err = %v, want RUN_NOT_FINISHED", err)

	finished := seedFinished(t, store, domain.RunStatusSucceeded,
		domain.StepRecord{StepID: "reserve", Position: 1, Status: domain.StepStatusSucceeded})
	_, _, err = e.rerun(ctx, finished, "charge", "rerun-4")
	require.Equal(t, apierr.CodeStepNotFound, engineCode(err), "unknown step: err = %v, want STEP_NOT_FOUND", err)

	_, _, err = e.rerun(ctx, uuid.NewString(), "", "rerun-5")
	require.Equal(t, apierr.CodeRunNotFound, engineCode(err), "unknown run: err = %v, want RUN_NOT_FOUND", err)
}

func TestRerunWithAKeyIsDeduplicatedLikeInvoke(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	original := seedFinished(t, store, domain.RunStatusSucceeded)

	first, _, err := e.rerun(context.Background(), original, "", "rerun-6")
	require.NoError(t, err, "rerun")

	again, dup, err := e.rerun(context.Background(), original, "", "rerun-6")
	require.NoError(t, err, "repeat rerun")
	require.True(t, dup, "repeat rerun: want %v deduplicated", first)
	require.Equal(t, first, again, "repeat rerun: want %v deduplicated", first)

	// The same key for a different request is a conflict.
	_, _, err = e.rerun(context.Background(), original, "reserve", "rerun-6")
	require.Contains(t, []apierr.Code{apierr.CodeIdempotencyConflict, apierr.CodeStepNotFound}, engineCode(err),
		"different request under one key: err = %v", err)
}

func TestListRunsPagesNewestFirstWithFilters(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	clk := newFakeClock(testNow)
	e := newClockedService(t, store, clk)
	ctx := context.Background()

	var ids []string
	for i := range 5 {
		clk.Set(testNow.Add(time.Duration(i) * time.Second))
		id, _, err := e.invoke(ctx, testRef, "k-"+string(rune('a'+i)), []byte("x"))
		require.NoError(t, err, "invoke %d", i)

		ids = append(ids, id)
	}

	page1, token, err := e.listRuns(ctx, domain.RunFilter{WorkerID: "demo"}, 2, "")
	require.NoError(t, err, "page 1")
	require.Len(t, page1, 2, "page 1")
	require.NotEmpty(t, token, "page 1 must have a token")
	require.Equal(t, ids[4], page1[0].ID, "page 1 must be newest first")
	require.Equal(t, ids[3], page1[1].ID, "page 1 must be newest first")

	page2, token, err := e.listRuns(ctx, domain.RunFilter{WorkerID: "demo"}, 2, token)
	require.NoError(t, err, "page 2")
	require.Len(t, page2, 2, "page 2")
	require.Equal(t, ids[2], page2[0].ID, "page 2")
	require.Equal(t, ids[1], page2[1].ID, "page 2")

	page3, token, err := e.listRuns(ctx, domain.RunFilter{WorkerID: "demo"}, 2, token)
	require.NoError(t, err, "page 3")
	require.Len(t, page3, 1, "page 3: the last run")
	require.Equal(t, ids[0], page3[0].ID, "page 3: the last run")
	require.Empty(t, token, "page 3: no token")

	// Filters: a window and a status that nothing has.
	window, _, err := e.listRuns(ctx, domain.RunFilter{
		WorkerID:      "demo",
		CreatedAfter:  testNow.Add(time.Second),
		CreatedBefore: testNow.Add(3 * time.Second),
	}, 50, "")
	require.NoError(t, err, "window")
	require.Len(t, window, 2, "window: the two created at 1s and 2s")

	none, _, err := e.listRuns(ctx, domain.RunFilter{WorkerID: "demo", Statuses: []domain.RunStatus{domain.RunStatusFailed}}, 50, "")
	require.NoError(t, err, "status filter")
	require.Empty(t, none, "status filter: none")
}

func TestListRunsRefusesBadPagingInput(t *testing.T) {
	store := newFakeStore()
	e := newTestService(t, store)
	_, _, err := e.listRuns(context.Background(), domain.RunFilter{}, 201, "")
	require.Equal(t, apierr.CodeInvalidRequest, engineCode(err), "page size 201: err = %v", err)

	_, _, err = e.listRuns(context.Background(), domain.RunFilter{}, 10, "not-a-token")
	require.Equal(t, apierr.CodeInvalidRequest, engineCode(err), "bad token: err = %v", err)
}

func TestCursorRoundTrip(t *testing.T) {
	want := runCursor{CreatedAt: time.Date(2026, 10, 11, 6, 0, 1, 123456000, time.UTC), ID: "run-1"}
	got, err := decodeCursor(encodeCursor(want))
	require.NoError(t, err, "decode")
	require.True(t, got.CreatedAt.Equal(want.CreatedAt), "round trip = %+v, want %+v", got, want)
	require.Equal(t, want.ID, got.ID, "round trip = %+v, want %+v", got, want)
}

// This token was produced by the encoder before the paging code moved to the engine,
// when the ID was a UUID. A token a client holds must still decode, so the encoding
// must not change.
func TestDecodeCursorAcceptsTokensIssuedBefore(t *testing.T) {
	const token = "eyJjIjoiMjAyNi0xMC0xMVQwNjowMDowMS4xMjM0NTZaIiwiaSI6IjAxOTVmMGEyLTdjM2UtN2QxYS04YjRmLTJlNmM5YTFkNWYxMCJ9"
	got, err := decodeCursor(token)
	require.NoError(t, err, "decode")
	require.True(t, got.CreatedAt.Equal(time.Date(2026, 10, 11, 6, 0, 1, 123456000, time.UTC)), "decoded = %+v", got)
	require.Equal(t, "0195f0a2-7c3e-7d1a-8b4f-2e6c9a1d5f10", got.ID, "decoded = %+v", got)
}

// A cursor ID is text, not a UUID. The token below is what the current encoder writes
// for the ID "run-1", and the decoder must accept it.
func TestDecodeCursorAcceptsTextIDs(t *testing.T) {
	want := runCursor{CreatedAt: time.Date(2026, 10, 10, 23, 0, 1, 123456000, time.UTC), ID: "run-1"}
	const token = "eyJjIjoiMjAyNi0xMC0xMFQyMzowMDowMS4xMjM0NTZaIiwiaSI6InJ1bi0xIn0"
	require.Equal(t, token, encodeCursor(want), "the token must be what the encoder writes today")

	got, err := decodeCursor(token)
	require.NoError(t, err, "decode")
	require.True(t, got.CreatedAt.Equal(want.CreatedAt), "decoded = %+v, want %+v", got, want)
	require.Equal(t, want.ID, got.ID, "decoded = %+v, want %+v", got, want)
}

func TestDecodeCursorRefusesWhatWasNotIssued(t *testing.T) {
	for _, token := range []string{"", "not base64!!", "e30", "eyJjIjoiMjAyNi0xMC0xMVQwNjowMDowMFoifQ"} {
		_, err := decodeCursor(token)
		require.ErrorIs(t, err, errPageToken, "token %q", token)
	}
}

// A token whose ID has a NUL byte is refused as a bad page token. The request check cannot
// see inside the encoded token, so this is the only check of that ID.
func TestDecodeCursorRefusesAnIDWithANulByte(t *testing.T) {
	createdAt := time.Date(2026, 10, 10, 23, 0, 1, 0, time.UTC)
	token := encodeCursor(runCursor{CreatedAt: createdAt, ID: "run\x00-1"})

	_, err := decodeCursor(token)
	require.ErrorIs(t, err, errPageToken, "decode of a NUL in the ID")

	store := newFakeStore()
	e := newTestService(t, store)
	_, _, err = e.listRuns(context.Background(), domain.RunFilter{}, 10, token)
	require.Equal(t, apierr.CodeInvalidRequest, engineCode(err), "listRuns with a NUL in the cursor ID: err = %v", err)
}

func TestPageSizeDefaultsAndCaps(t *testing.T) {
	n, err := pageSize(0)
	require.NoError(t, err, "zero")
	require.Equal(t, defaultPageSize, n, "zero: want the default")

	n, err = pageSize(maxPageSize)
	require.NoError(t, err, "max")
	require.Equal(t, maxPageSize, n, "max")

	_, err = pageSize(maxPageSize + 1)
	require.ErrorIs(t, err, errPageSize, "over the max")
}

// waitService builds an engine whose WaitRun timers the test fires by hand.
func waitService(t *testing.T, store *fakeStore) (*Service, *fakeAfter) {
	t.Helper()
	e := newTestService(t, store)
	fa := &fakeAfter{}
	e.after = fa.After
	return e, fa
}

// waitForTimers blocks until the engine has armed at least n timers.
func waitForTimers(t *testing.T, fa *fakeAfter, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		fa.mu.Lock()
		defer fa.mu.Unlock()
		return len(fa.timers) >= n
	}, 2*time.Second, 2*time.Millisecond, "timers armed, want %d", n)
}

// A run_id is text that the stores look up as it is. An ID that names no Run is not
// found, whatever its form, and only an empty run_id is refused.
func TestGetRunLooksUpAnyTextAndRefusesOnlyAnEmptyRunID(t *testing.T) {
	h := runHandler{s: newTestService(t, newFakeStore())}
	ctx := context.Background()

	_, err := h.GetRun(ctx, &runcoilv1.GetRunRequest{
		By: &runcoilv1.GetRunRequest_RunId{RunId: "not-a-uuid"},
	})
	var apiErr *apierr.Error
	require.ErrorAs(t, err, &apiErr, "malformed run_id: err = %v, want RUN_NOT_FOUND", err)
	require.Equal(t, apierr.KindNotFound, apiErr.Kind, "malformed run_id")
	require.Equal(t, apierr.CodeRunNotFound, apiErr.Code, "malformed run_id")

	_, err = h.GetRun(ctx, &runcoilv1.GetRunRequest{
		By: &runcoilv1.GetRunRequest_RunId{RunId: ""},
	})
	require.ErrorAs(t, err, &apiErr, "empty run_id: err = %v, want INVALID_ARGUMENT with run_id is required", err)
	require.Equal(t, apierr.KindInvalid, apiErr.Kind, "empty run_id")
	require.Equal(t, "run_id is required", apiErr.Message, "empty run_id")
}

func TestWaitRunReturnsWhenTheRunFinishes(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)

	type result struct {
		run      domain.Run
		timedOut bool
		err      error
	}

	done := make(chan result, 1)
	go func() {
		run, _, timedOut, err := e.waitRun(context.Background(), id, 0, false)
		done <- result{run, timedOut, err}
	}()

	waitForTimers(t, fa, 2)

	// The Run finishes; the decision is taken under the lock, then the waiters are woken.
	err := store.Mutate(context.Background(), id, func(s domain.Snapshot) (domain.Write, error) {
		s.Run.Status = domain.RunStatusSucceeded
		s.Run.FinishedAt = testNow
		return domain.Write{Run: s.Run}, nil
	})
	require.NoError(t, err, "finish")

	e.signals.Notify(topicRunFinished(id))

	select {
	case r := <-done:
		require.NoError(t, r.err, "wait: want the finished Run")
		require.False(t, r.timedOut, "wait: want the finished Run")
		require.Equal(t, domain.RunStatusSucceeded, r.run.Status, "wait: want the finished Run")
	case <-time.After(2 * time.Second):
		require.Fail(t, "WaitRun did not return once the Run finished")
	}
}

func TestWaitRunTimesOutWithoutChangingTheRun(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)

	done := make(chan struct {
		run      domain.Run
		timedOut bool
		err      error
	}, 1)

	go func() {
		run, _, timedOut, err := e.waitRun(context.Background(), id, 2*time.Second, false)
		done <- struct {
			run      domain.Run
			timedOut bool
			err      error
		}{run, timedOut, err}
	}()

	waitForTimers(t, fa, 2)
	fa.FireFirst(t, 2*time.Second)

	select {
	case r := <-done:
		require.NoError(t, r.err, "wait: want timed out with the Run still queued")
		require.True(t, r.timedOut, "wait: want timed out with the Run still queued")
		require.Equal(t, domain.RunStatusQueued, r.run.Status, "wait: want timed out with the Run still queued")
	case <-time.After(2 * time.Second):
		require.Fail(t, "WaitRun did not return at its timeout")
	}

	stored, _ := store.GetRun(context.Background(), id)
	require.Equal(t, domain.RunStatusQueued, stored.Status, "a timed-out wait must not change the Run")
}

func TestWaitRunTimeoutIsCappedByConfiguration(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)

	go func() {
		_, _, _, _ = e.waitRun(context.Background(), id, time.Hour, false)
	}()

	waitForTimers(t, fa, 2)
	// The configured maximum is one minute, so a one-hour request waits one minute.
	fa.FireFirst(t, time.Minute)
}

func TestWaitRunStopsWhenTheCallIsCancelled(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, _, err := e.waitRun(ctx, id, 0, false)
		done <- err
	}()

	waitForTimers(t, fa, 2)
	cancel()

	select {
	case err := <-done:
		var apiErr *apierr.Error
		require.ErrorAs(t, err, &apiErr, "err = %v, want a canceled error", err)
		require.Equal(t, apierr.KindCanceled, apiErr.Kind, "err = %v, want a canceled error", err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "WaitRun kept waiting after its call was cancelled")
	}
}

func TestWaitRunUnknownRunIsNotFound(t *testing.T) {
	store := newFakeStore()
	e, _ := waitService(t, store)
	_, _, _, err := e.waitRun(context.Background(), uuid.NewString(), time.Second, false)
	require.Equal(t, apierr.CodeRunNotFound, engineCode(err), "err = %v, want RUN_NOT_FOUND", err)
}

// kindOf is the apierr kind of an error, or KindInternal when it is not an apierr.
func kindOf(err error) apierr.Kind {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Kind
	}

	return apierr.KindInternal
}

// A call over the configured number of waiters is refused with an unavailable error.
func TestWaitRunOverTheWaiterCapIsUnavailable(t *testing.T) {
	store := newFakeStore()
	e, _ := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)
	for range cap(e.waitSlots) {
		e.waitSlots <- struct{}{} // every slot is taken by a waiting call
	}

	_, _, _, err := e.waitRun(context.Background(), id, time.Second, false)
	require.Equal(t, apierr.KindUnavailable, kindOf(err), "err = %v, want an unavailable error over the cap", err)

	<-e.waitSlots
	fa := &fakeAfter{}
	e.after = fa.After
	done := make(chan error, 1)
	go func() {
		_, _, _, err := e.waitRun(context.Background(), id, time.Second, false)
		done <- err
	}()

	waitForTimers(t, fa, 2)
	fa.FireFirst(t, time.Second)
	err = <-done
	require.NoError(t, err, "a call under the cap waits normally")
}

// Shutdown ends a waiting call at once, as unavailable.
func TestWaitRunEndsAtShutdown(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)
	done := make(chan error, 1)
	go func() {
		_, _, _, err := e.waitRun(context.Background(), id, time.Hour, false)
		done <- err
	}()

	waitForTimers(t, fa, 2)
	e.stopOnce.Do(func() { close(e.stopped) })
	select {
	case err := <-done:
		require.Equal(t, apierr.KindUnavailable, kindOf(err), "err = %v, want unavailable at shutdown", err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "a waiting call must end when the engine shuts down")
	}
}

// A client deadline that passes while waiting is deadline-exceeded, not canceled.
func TestWaitRunDeadlineIsDeadlineExceeded(t *testing.T) {
	store := newFakeStore()
	e, fa := waitService(t, store)
	id := store.seedQueued(t, testRef, testNow)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, err := e.waitRun(ctx, id, time.Hour, false)
		done <- err
	}()

	waitForTimers(t, fa, 1)
	select {
	case err := <-done:
		require.Equal(t, apierr.KindDeadlineExceeded, kindOf(err), "err = %v, want deadline exceeded", err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "an expired call deadline must end the wait")
	}
}

// A rerun whose source Run was removed after it was read answers RUN_NOT_FOUND.
func TestRerunOfARunRemovedMidFlightIsNotFound(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	original := seedFinished(t, store, domain.RunStatusSucceeded)
	// The source is read, then removed before the insert: the store reports it gone.
	e.runs = goneOnCreate{store}
	_, _, err := e.rerun(context.Background(), original, "", "rerun-gone")
	require.Equal(t, apierr.CodeRunNotFound, engineCode(err), "err = %v, want RUN_NOT_FOUND", err)
}

// goneOnCreate is a run store whose keyed create finds the rerun's source removed.
type goneOnCreate struct {
	*fakeStore
}

func (goneOnCreate) CreateRun(context.Context, domain.Run, domain.KeyRecord, []domain.StepRecord) (domain.KeyHolder, bool, error) {
	return domain.KeyHolder{}, false, domain.ErrNotFound
}

// A rerun from a pending Step is refused: the Step has no final record.
func TestRerunFromAPendingStepIsNotFound(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newTestService(t, store)
	original := seedFinished(t, store, domain.RunStatusFailed,
		domain.StepRecord{StepID: "charge", Position: 1, Status: domain.StepStatusPending, RetryNotBefore: testNow})
	_, _, err := e.rerun(context.Background(), original, "charge", "rerun-pending")
	require.Equal(t, apierr.CodeStepNotFound, engineCode(err), "err = %v, want STEP_NOT_FOUND for a pending Step", err)
}

func TestInvokeInputOverLimitIsRejected(t *testing.T) {
	store := newFakeStore()
	registerTestTask(t, store, nil)
	e := newServiceWith(t, store, nil) // input limit is 8 bytes
	_, _, err := e.invoke(context.Background(), testRef, "k-big", []byte("0123456789"))
	require.Equal(t, apierr.CodeInputTooLarge, engineCode(err), "err = %v, want INPUT_TOO_LARGE", err)

	_, _, err = e.invoke(context.Background(), testRef, "k-fits", []byte("01234567"))
	require.NoError(t, err, "input at the limit must be accepted")
}
