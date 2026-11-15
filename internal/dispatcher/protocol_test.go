package dispatcher

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

func TestRegistrationError(t *testing.T) {
	err := registrationError(&domain.RegistrationError{Message: "task is declared twice", TaskName: "a"})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeInvalidRegistration, appErr.Code)
	assert.Equal(t, "task is declared twice", appErr.Message)
	assert.Equal(t, "a", appErr.Details["task_name"])
}

func TestNewSessionTokenHashesTheToken(t *testing.T) {
	first, firstHash, err := newSessionToken()
	require.NoError(t, err)

	second, _, err := newSessionToken()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.Len(t, firstHash, 32)
}

// fakeRunStore answers CompleteStep with result or err, keeps the decider it was given,
// and answers FinishRun with ended or err.
type fakeRunStore struct {
	store.RunStore

	result domain.StepResult
	err    error
	ended  bool
	decide domain.RetryDecider
	params *domain.CompleteStepParams
	end    *domain.RunEnd
}

func (f *fakeRunStore) CompleteStep(_ context.Context, params domain.CompleteStepParams, decide domain.RetryDecider) (domain.StepResult, error) {
	f.decide = decide
	f.params = &params
	return f.result, f.err
}

func (f *fakeRunStore) FinishRun(_ context.Context, _ uuid.UUID, _ domain.Lease, end domain.RunEnd) (bool, error) {
	f.end = &end
	return f.ended, f.err
}

// heldStream returns a one-slot stream of workerID that holds lease.
func heldStream(workerID uuid.UUID, lease domain.Lease) *stream {
	return &stream{id: uuid.New(), workerID: workerID, slots: 1, held: map[domain.Lease]struct{}{lease: {}}}
}

func retryService(jitter float64, runStore store.RunStore) *Service {
	return &Service{
		cfg: Config{Retry: RetryConfig{
			MaxAttempts: 3,
			Delays:      []time.Duration{10 * time.Second, time.Minute},
			Jitter:      jitter,
		}},
		logger:   slog.New(slog.DiscardHandler),
		runStore: runStore,
		streams:  map[uuid.UUID]*stream{},
	}
}

func TestCompleteStepFreesTheSlotOfARetry(t *testing.T) {
	workerID := uuid.New()
	lease := domain.Lease{RunID: uuid.New(), Epoch: 1}
	params := domain.CompleteStepParams{WorkerID: workerID, Lease: lease, StepID: "charge"}

	tests := []struct {
		name     string
		result   domain.StepResult
		err      error
		wantFree int
	}{
		{name: "a retry ends the attempt", result: domain.StepResult{Retry: &domain.ScheduledRetry{TryCount: 1}}, wantFree: 1},
		{name: "a stale lease is no longer held", err: domain.ErrStaleLease, wantFree: 1},
		{name: "a record keeps it", result: domain.StepResult{Record: domain.StepRecord{StepID: "charge"}}, wantFree: 0},
		{name: "a failed store keeps it", err: errors.New("timeout"), wantFree: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := retryService(0, &fakeRunStore{result: tt.result, err: tt.err})
			s.streams[workerID] = heldStream(workerID, lease)

			got, err := s.CompleteStep(context.Background(), params)
			if tt.err == nil {
				require.NoError(t, err)
				assert.Equal(t, tt.result, got)
			}

			assert.Equal(t, tt.wantFree, s.streams[workerID].free())
		})
	}
}

func TestCompleteRunFreesTheSlot(t *testing.T) {
	worker := domain.Worker{ID: uuid.New()}
	lease := domain.Lease{RunID: uuid.New(), Epoch: 1}

	tests := []struct {
		name     string
		store    *fakeRunStore
		wantErr  bool
		wantFree int
	}{
		{name: "the run ended", store: &fakeRunStore{ended: true}, wantFree: 1},
		{name: "the run had already ended", store: &fakeRunStore{}, wantFree: 1},
		{name: "a stale lease", store: &fakeRunStore{err: domain.ErrStaleLease}, wantErr: true, wantFree: 1},
		{name: "a failed store", store: &fakeRunStore{err: errors.New("timeout")}, wantErr: true, wantFree: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := retryService(0, tt.store)
			s.streams[worker.ID] = heldStream(worker.ID, lease)

			err := s.CompleteRun(context.Background(), worker, lease, domain.RunResult{})
			assert.Equal(t, tt.wantErr, err != nil)
			assert.Equal(t, tt.wantFree, s.streams[worker.ID].free())
		})
	}
}

func TestCompleteStepRefusesAnInvalidRetry(t *testing.T) {
	store := &fakeRunStore{}
	s := retryService(0, store)

	_, err := s.CompleteStep(context.Background(), domain.CompleteStepParams{
		Retry: domain.StepRetry{Policy: &domain.RetryPolicy{MaxAttempts: 31}},
	})

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodeInvalidRequest, appErr.Code)
	assert.Nil(t, store.decide, "nothing reaches the store")
}

func TestCompleteStepRefusesANULInTheStepID(t *testing.T) {
	store := &fakeRunStore{}
	s := retryService(0, store)

	_, err := s.CompleteStep(context.Background(), domain.CompleteStepParams{StepID: "bad\x00id"})

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodeInvalidRequest, appErr.Code)
	assert.Equal(t, "step id must not contain a NUL byte", appErr.Message)
	assert.Nil(t, store.params, "nothing reaches the store")
}

func TestCompleteStepRefusesAnOutputOverTheLimit(t *testing.T) {
	store := &fakeRunStore{}
	s := retryService(0, store)

	_, err := s.CompleteStep(context.Background(), domain.CompleteStepParams{
		Lease:   domain.Lease{RunID: uuid.New(), Epoch: 1},
		StepID:  "big",
		Outcome: domain.StepOutcome{Output: make([]byte, domain.MaxOutputBytes+1)},
	})

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.KindInvalid, appErr.Kind)
	assert.Equal(t, apperr.CodeOutputTooLarge, appErr.Code)
	assert.Nil(t, store.params, "nothing reaches the store")
}

func TestCompleteStepStoresAnOutputAtTheLimit(t *testing.T) {
	store := &fakeRunStore{}
	s := retryService(0, store)

	_, err := s.CompleteStep(context.Background(), domain.CompleteStepParams{
		StepID:  "big",
		Outcome: domain.StepOutcome{Output: make([]byte, domain.MaxOutputBytes)},
	})
	require.NoError(t, err)
	assert.NotNil(t, store.params)
}

func TestCompleteRunRefusesAnOutputOverTheLimit(t *testing.T) {
	worker := domain.Worker{ID: uuid.New()}
	lease := domain.Lease{RunID: uuid.New(), Epoch: 1}
	store := &fakeRunStore{ended: true}
	s := retryService(0, store)
	s.streams[worker.ID] = heldStream(worker.ID, lease)

	err := s.CompleteRun(context.Background(), worker, lease, domain.RunResult{Output: make([]byte, domain.MaxOutputBytes+1)})

	var apiErr *apperr.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apperr.KindInvalid, apiErr.Kind)
	assert.Equal(t, apperr.CodeOutputTooLarge, apiErr.Code)
	assert.Equal(t, "run output is 1048577 bytes, over the limit of 1048576 bytes", apiErr.Message)
	assert.Nil(t, store.end, "nothing reaches the store")
	assert.Equal(t, 0, s.streams[worker.ID].free(), "the worker still holds the attempt and reports its failure")
}

func failed(code domain.FailureCode) domain.StepOutcome {
	return domain.StepOutcome{Failure: &domain.Failure{Code: code, Message: "boom"}}
}

func ptr[T any](v T) *T { return &v }

func TestRetryDecider(t *testing.T) {
	s := retryService(0, nil)
	runPolicy := &domain.RetryPolicy{MaxAttempts: 5, Delays: []time.Duration{time.Second}}

	tests := []struct {
		name      string
		outcome   domain.StepOutcome
		retry     domain.StepRetry
		tries     int
		runPolicy *domain.RetryPolicy
		want      time.Duration
		wantRetry bool
	}{
		{name: "server default", outcome: failed(domain.FailureCodeError), tries: 1, want: 10 * time.Second, wantRetry: true},
		{name: "server default, last delay repeats", outcome: failed(domain.FailureCodeError), tries: 2, want: time.Minute, wantRetry: true},
		{name: "server default, out of attempts", outcome: failed(domain.FailureCodeError), tries: 3},
		{name: "run policy over the default", outcome: failed(domain.FailureCodeError), tries: 4, runPolicy: runPolicy, want: time.Second, wantRetry: true},
		{
			name:      "step policy over the run's, delays from the run",
			outcome:   failed(domain.FailureCodeError),
			retry:     domain.StepRetry{Policy: &domain.RetryPolicy{MaxAttempts: 2}},
			tries:     1,
			runPolicy: runPolicy,
			want:      time.Second,
			wantRetry: true,
		},
		{
			name:      "step policy ends the retries",
			outcome:   failed(domain.FailureCodeError),
			retry:     domain.StepRetry{Policy: &domain.RetryPolicy{MaxAttempts: 2}},
			tries:     2,
			runPolicy: runPolicy,
		},
		{name: "retry after replaces the delay", outcome: failed(domain.FailureCodeError), retry: domain.StepRetry{After: ptr(3 * time.Second)}, tries: 1, want: 3 * time.Second, wantRetry: true},
		{name: "retry after does not add attempts", outcome: failed(domain.FailureCodeError), retry: domain.StepRetry{After: ptr(3 * time.Second)}, tries: 3},
		{name: "non retriable", outcome: failed(domain.FailureCodeError), retry: domain.StepRetry{NonRetriable: true}, tries: 1},
		{name: "panic", outcome: failed(domain.FailureCodePanic), tries: 1},
		{name: "output not encodable", outcome: failed(domain.FailureCodeOutputNotEncodable), tries: 1},
		{name: "success", outcome: domain.StepOutcome{Output: []byte(`1`)}, tries: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, retry := s.retryDecider(tt.outcome, tt.retry)(tt.tries, tt.runPolicy)
			assert.Equal(t, tt.wantRetry, retry)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRetryDeciderJittersOnlyThePolicyDelay(t *testing.T) {
	s := retryService(0.5, nil)

	for range 100 {
		got, retry := s.retryDecider(failed(domain.FailureCodeError), domain.StepRetry{})(1, nil)
		require.True(t, retry)
		assert.InDelta(t, float64(10*time.Second), float64(got), float64(5*time.Second))

		got, retry = s.retryDecider(failed(domain.FailureCodeError), domain.StepRetry{After: ptr(3 * time.Second)})(1, nil)
		require.True(t, retry)
		assert.Equal(t, 3*time.Second, got, "retry_after is not spread")
	}
}

func TestJitterStaysInItsRange(t *testing.T) {
	for range 1000 {
		got := jitter(10*time.Second, 0.2)
		assert.GreaterOrEqual(t, got, 8*time.Second)
		assert.LessOrEqual(t, got, 12*time.Second)
	}

	assert.Equal(t, 10*time.Second, jitter(10*time.Second, 0))
	assert.Zero(t, jitter(0, 0.2))
}

// deregisterStore answers Deregister with released or err.
type deregisterStore struct {
	store.WorkerStore

	released int
	err      error
}

func (f *deregisterStore) Deregister(context.Context, uuid.UUID) (int, error) {
	return f.released, f.err
}

func TestDeregisterEndsTheStream(t *testing.T) {
	s := newStreamService(t, &deregisterStore{released: 2})
	worker := domain.Worker{ID: uuid.New(), App: "shop"}
	st := newStream(worker.ID, 1, 1, 1)
	s.put(st)

	require.NoError(t, s.Deregister(context.Background(), worker, "shutdown"))

	assert.True(t, isEnded(st))
	assert.NotContains(t, s.streams, worker.ID)
}

func TestDeregisterOfAnEndedSession(t *testing.T) {
	s := newStreamService(t, &deregisterStore{err: domain.ErrSessionEnded})

	err := s.Deregister(context.Background(), domain.Worker{ID: uuid.New()}, "shutdown")

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodeUnauthenticated, appErr.Code)
}
