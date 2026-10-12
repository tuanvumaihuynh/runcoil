package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

var t0 = time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)

// attach is Attach for a test that expects the instance to be registered.
func attach(d *instances, id string, now time.Time) (*stream, bool) {
	return d.Attach(id, now)
}

func serving(d *instances, ref domain.TaskRef) []candidate {
	return d.Serving(ref)
}

func sweep(d *instances, now time.Time, lease, evict time.Duration) (int, int) {
	closed, evicted := d.Sweep(now, lease, evict)
	return closed, len(evicted)
}

func registerI1(d *instances, now time.Time) {
	d.Register(domain.Registration{WorkerID: "demo", InstanceID: "i-1", Slots: 1,
		Tasks: []domain.Task{{Ref: domain.TaskRef{WorkerID: "demo", TaskID: "hello"}}}}, now)
}

func TestSilentStreamIsEndedAfterOneLease(t *testing.T) {
	d := newInstances()
	registerI1(d, t0)
	sess, ok := attach(d, "i-1", t0)
	require.True(t, ok, "attach")

	lease, evict := 3*time.Second, 9*time.Second

	// A heartbeat at +1s keeps the stream alive.
	d.Touch("i-1", t0.Add(time.Second))
	closed, _ := sweep(d, t0.Add(4*time.Second), lease, evict)
	require.Equal(t, 0, closed, "3s after the last heartbeat is not over one lease")
	require.False(t, sess.Closed(), "3s after the last heartbeat is not over one lease")

	closed, _ = sweep(d, t0.Add(5*time.Second), lease, evict)
	require.Equal(t, 1, closed, "over one lease of silence the stream must end")
	require.True(t, sess.Closed(), "over one lease of silence the stream must end")

	got := d.StreamOf("i-1")
	require.Nil(t, got, "a closed stream is not an open one")

	dispatched := serving(d, domain.TaskRef{WorkerID: "demo", TaskID: "hello"})
	require.Empty(t, dispatched, "a closed stream must receive no dispatch")
}

func TestSilentInstanceWithoutStreamIsEvicted(t *testing.T) {
	d := newInstances()
	registerI1(d, t0)
	lease, evict := 3*time.Second, 9*time.Second

	_, evicted := sweep(d, t0.Add(8*time.Second), lease, evict)
	require.Equal(t, 0, evicted, "an instance is not evicted before the eviction period")
	_, evicted = sweep(d, t0.Add(10*time.Second), lease, evict)
	require.Equal(t, 1, evicted, "a silent instance without a stream is evicted")
	_, ok := attach(d, "i-1", t0.Add(10*time.Second))
	require.False(t, ok, "an evicted instance must register again through NOT_REGISTERED")
}

func TestHeartbeatKeepsInstanceAlive(t *testing.T) {
	d := newInstances()
	registerI1(d, t0)
	sess, _ := attach(d, "i-1", t0)
	for sec := 2; sec <= 12; sec += 2 {
		now := t0.Add(time.Duration(sec) * time.Second)
		d.Touch("i-1", now)
		sweep(d, now, 3*time.Second, 9*time.Second)
	}

	require.False(t, sess.Closed(), "regular heartbeats keep the stream open")
}

func TestTouchUnknownInstanceReportsFalse(t *testing.T) {
	ok := newInstances().Touch("nobody", t0)
	require.False(t, ok, "a heartbeat from an unknown instance is not recorded")
}

func TestServingIsSortedAndNeedsTheTaskAndStream(t *testing.T) {
	d := newInstances()
	ref := domain.TaskRef{WorkerID: "demo", TaskID: "hello"}
	for _, id := range []string{"i-b", "i-a"} {
		d.Register(domain.Registration{WorkerID: "demo", InstanceID: id, Slots: 2,
			Tasks: []domain.Task{{Ref: ref}}}, t0)

		attach(d, id, t0)
	}

	d.Register(domain.Registration{WorkerID: "demo", InstanceID: "i-c", Slots: 2,
		Tasks: []domain.Task{{Ref: ref}}}, t0) // no stream: not serving

	got := serving(d, ref)
	require.Len(t, got, 2, "want i-a and i-b, sorted")
	require.Equal(t, "i-a", got[0].InstanceID, "want i-a and i-b, sorted")
	require.Equal(t, "i-b", got[1].InstanceID, "want i-a and i-b, sorted")
	require.Equal(t, 2, got[0].Slots, "slots of i-a")
}

func TestDetachKeepsANewerStream(t *testing.T) {
	d := newInstances()
	registerI1(d, t0)
	old, _ := attach(d, "i-1", t0)
	current, _ := attach(d, "i-1", t0)
	d.Detach("i-1", old)
	require.Same(t, current, d.StreamOf("i-1"), "detaching an old stream must not remove the current one")
	require.True(t, old.Closed(), "the old stream is closed by detach")
}

var cancelMsg = streamMessage{Cancel: &domain.AttemptRef{Token: "t"}}

func TestStreamSendTimesOutAndCloses(t *testing.T) {
	// No reader takes the message, so the send must give up on its own.
	s := newStream()
	err := s.Send(context.Background(), cancelMsg, 10*time.Millisecond)
	require.ErrorIs(t, err, errSendTimeout, "send err")
	require.True(t, s.Closed(), "a stream that does not take a message in time must be closed")
}

func TestStreamSendOnClosedStream(t *testing.T) {
	s := newStream()
	s.Close()
	err := s.Send(context.Background(), cancelMsg, time.Second)
	require.ErrorIs(t, err, errStreamClosed, "send err")
}

func TestStreamSendDeliversWhenReaderTakesIt(t *testing.T) {
	s := newStream()
	go func() {
		item := <-s.Outbox()
		if item.Message.Cancel == nil {
			item.Result <- errors.New("wrong message")
			return
		}

		item.Result <- nil
	}()

	err := s.Send(context.Background(), cancelMsg, time.Second)
	require.NoError(t, err, "send")
}
