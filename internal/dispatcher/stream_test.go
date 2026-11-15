package dispatcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

type streamStore struct {
	store.WorkerStore

	notHeld []domain.Lease
	maxLost int
	claim   func(domain.ClaimRunsParams) ([]domain.Assign, error)
}

func (f *streamStore) AttachStream(_ context.Context, params domain.AttachStreamParams) (domain.AttachStreamResult, error) {
	f.maxLost = params.MaxLost
	params.Attached(f.notHeld)

	return domain.AttachStreamResult{NotHeld: f.notHeld}, nil
}

func (f *streamStore) ClaimRuns(_ context.Context, params domain.ClaimRunsParams) ([]domain.Assign, error) {
	return f.claim(params)
}

func newStreamService(t *testing.T, store store.WorkerStore) *Service {
	t.Helper()

	cfg := Config{Port: 7070, ClaimInterval: time.Hour, HeartbeatInterval: time.Hour, WorkerTTL: 2 * time.Hour, MaxLostAttempts: 5}
	return New(cfg, slog.New(slog.DiscardHandler), store, nil)
}

// newStream returns a stream whose worker already runs slots - free attempts.
func newStream(workerID uuid.UUID, slots, free, buffer int) *stream {
	st := &stream{
		id:       uuid.New(),
		workerID: workerID,
		slots:    slots,
		out:      make(chan streamMessage, buffer),
		ended:    make(chan struct{}),
		held:     map[domain.Lease]struct{}{},
	}
	for range slots - free {
		st.held[assign().Lease] = struct{}{}
	}

	return st
}

func assign() domain.Assign {
	return domain.Assign{Lease: domain.Lease{RunID: uuid.New(), Epoch: 1}}
}

func isEnded(st *stream) bool {
	select {
	case <-st.ended:
		return true
	default:
		return false
	}
}

func TestDropKeepsANewerStream(t *testing.T) {
	s := newStreamService(t, nil)
	workerID := uuid.New()
	old, newer := newStream(workerID, 1, 1, 1), newStream(workerID, 1, 1, 1)

	s.put(old)
	s.put(newer)
	s.drop(workerID, old.id)

	assert.Same(t, newer, s.streams[workerID])
}

func TestEnd(t *testing.T) {
	s := newStreamService(t, nil)
	workerID := uuid.New()
	st := newStream(workerID, 1, 1, 1)
	s.put(st)

	s.end(workerID, uuid.New())
	assert.False(t, isEnded(st), "another stream id ends nothing")

	s.end(workerID, st.id)
	assert.True(t, isEnded(st))
	assert.NotContains(t, s.streams, workerID)
}

func TestClaimableHasOnlyStreamsWithFreeSlots(t *testing.T) {
	s := newStreamService(t, nil)
	full, free := newStream(uuid.New(), 2, 0, 1), newStream(uuid.New(), 2, 2, 1)
	s.put(full)
	s.put(free)

	targets := s.claimable()

	require.Len(t, targets, 1)
	assert.Equal(t, free.id, targets[0].streamID)
	assert.Equal(t, 2, targets[0].free)
}

func TestPushTakesSlotsAndSends(t *testing.T) {
	s := newStreamService(t, nil)
	st := newStream(uuid.New(), 3, 3, 3)
	s.put(st)

	a, b := assign(), assign()
	s.push(context.Background(), st.workerID, st.id, []domain.Assign{a, b})

	assert.Equal(t, 1, st.free())
	assert.Contains(t, st.held, a.Lease)
	assert.Contains(t, st.held, b.Lease)
	assert.Len(t, st.out, 2)
}

func TestPushToAnEndedStreamSendsNothing(t *testing.T) {
	s := newStreamService(t, nil)
	st := newStream(uuid.New(), 1, 1, 1)
	s.put(st)
	s.drop(st.workerID, st.id)

	s.push(context.Background(), st.workerID, st.id, []domain.Assign{assign()})

	assert.Empty(t, st.out)
}

func TestPushEndsTheStreamWhenItsBufferIsFull(t *testing.T) {
	s := newStreamService(t, nil)
	st := newStream(uuid.New(), 2, 2, 1)
	s.put(st)

	s.push(context.Background(), st.workerID, st.id, []domain.Assign{assign(), assign()})

	assert.True(t, isEnded(st))
	assert.NotContains(t, s.streams, st.workerID)
}

func TestReleaseFreesOnlyTheSlotOfAHeldLease(t *testing.T) {
	s := newStreamService(t, nil)
	st := newStream(uuid.New(), 2, 2, 2)
	s.put(st)

	a := assign()
	s.push(context.Background(), st.workerID, st.id, []domain.Assign{a, assign()})
	require.Equal(t, 0, st.free())

	s.release(st.workerID, a.Lease)
	s.release(st.workerID, a.Lease)
	assert.Equal(t, 1, st.free(), "releasing the same lease twice frees one slot")

	s.release(st.workerID, assign().Lease)
	assert.Equal(t, 1, st.free(), "a lease the stream does not hold frees nothing")
}

func TestEndWorkerEndsWhicheverStream(t *testing.T) {
	s := newStreamService(t, nil)
	st := newStream(uuid.New(), 1, 1, 1)
	s.put(st)

	s.endWorker(uuid.New())
	assert.False(t, isEnded(st))

	s.endWorker(st.workerID)
	assert.True(t, isEnded(st))
	assert.NotContains(t, s.streams, st.workerID)
}

func TestClaimAllEndsTheStream(t *testing.T) {
	for _, claimErr := range []error{domain.ErrStreamFenced, errors.New("read assigns: timeout")} {
		t.Run(claimErr.Error(), func(t *testing.T) {
			store := &streamStore{claim: func(domain.ClaimRunsParams) ([]domain.Assign, error) { return nil, claimErr }}
			s := newStreamService(t, store)
			st := newStream(uuid.New(), 1, 1, 1)
			s.put(st)

			s.claimAll(context.Background())

			assert.True(t, isEnded(st))
		})
	}
}

func TestClaimAllPushesTheAssigns(t *testing.T) {
	var got domain.ClaimRunsParams
	store := &streamStore{claim: func(params domain.ClaimRunsParams) ([]domain.Assign, error) {
		got = params
		return []domain.Assign{assign()}, nil
	}}
	s := newStreamService(t, store)
	st := newStream(uuid.New(), 2, 2, 2)
	s.put(st)

	s.claimAll(context.Background())

	assert.Equal(t, 2, got.Slots)
	assert.Equal(t, s.id, got.DispatcherID)
	assert.Equal(t, 1, st.free())
	assert.Len(t, st.out, 1)
}

func TestListen(t *testing.T) {
	held, lost := domain.Lease{RunID: uuid.New(), Epoch: 1}, domain.Lease{RunID: uuid.New(), Epoch: 2}
	store := &streamStore{notHeld: []domain.Lease{lost}}
	s := newStreamService(t, store)
	worker := domain.Worker{ID: uuid.New(), Slots: 3}

	var (
		mu   sync.Mutex
		sent []streamMessage
	)
	send := func(msg streamMessage) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, msg)

		return nil
	}

	done := make(chan error, 1)
	go func() { done <- s.Listen(context.Background(), worker, []domain.Lease{held, lost}, send) }()

	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, ok := s.streams[worker.ID]

		return ok
	}, 5*time.Second, 10*time.Millisecond)

	s.mu.Lock()
	st := s.streams[worker.ID]
	free := st.free()
	_, holdsHeld := st.held[held]
	s.mu.Unlock()

	assert.Equal(t, 2, free, "the held attempt takes a slot, the lost one does not")
	assert.True(t, holdsHeld)
	assert.Equal(t, 5, store.maxLost, "unknown leases count toward the lost cap")

	s.end(worker.ID, st.id)

	select {
	case err := <-done:
		assert.ErrorIs(t, err, errStreamEnded)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Listen did not return")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	assert.Equal(t, &lost, sent[0].LeaseLost)
}

func TestListenRefusesWhileStopping(t *testing.T) {
	s := newStreamService(t, &streamStore{})
	s.Stop()

	err := s.Listen(context.Background(), domain.Worker{ID: uuid.New(), Slots: 1}, nil, func(streamMessage) error { return nil })

	assert.ErrorContains(t, err, "shutting down")
}
