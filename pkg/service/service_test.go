package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errBoom = errors.New("boom")
	errStop = errors.New("stop requested")
)

// events records what the stubs do, in the order they do it.
type events struct {
	mu  sync.Mutex
	log []string
}

func (e *events) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, event)
}

func (e *events) list() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.log...)
}

// stops returns the events that are Stop calls, in order.
func (e *events) stops() []string {
	var out []string
	for _, event := range e.list() {
		if strings.HasPrefix(event, "stop ") {
			out = append(out, event)
		}
	}

	return out
}

// stub is a Service for the tests. Run closes started, then blocks until Stop is called
// or the test sends an error on fail. Run waits for release, when it is set, after Stop
// before it returns.
type stub struct {
	name     string
	events   *events
	started  chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
	fail     chan error
	release  chan struct{}
}

func newStub(name string, events *events) *stub {
	return &stub{
		name:    name,
		events:  events,
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		fail:    make(chan error, 1),
	}
}

func (s *stub) Run(_ context.Context) error {
	close(s.started)

	var err error
	select {
	case <-s.stopped:
	case err = <-s.fail:
	}

	if s.release != nil {
		<-s.release
	}

	s.events.add("returned " + s.name)
	return err
}

func (s *stub) Stop() {
	s.events.add("stop " + s.name)
	s.stopOnce.Do(func() { close(s.stopped) })
}

// runAsync runs the manager in a goroutine, and returns the channel that receives its error.
func runAsync(ctx context.Context, m *Manager) <-chan error {
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	return done
}

// waitRun waits for the manager's Run to return. The limit only bounds a broken test.
func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the manager did not return")
		return nil
	}
}

// waitStarted waits until every stub's Run has begun.
func waitStarted(t *testing.T, stubs ...*stub) {
	t.Helper()
	for _, s := range stubs {
		select {
		case <-s.started:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "a service did not start", s.name)
		}
	}
}

func TestManagerWithNoServicesReturnsNil(t *testing.T) {
	require.NoError(t, NewManager().Run(context.Background()))
}

func TestStopIsCalledInReverseRegistrationOrder(t *testing.T) {
	log := &events{}
	a, b, c := newStub("a", log), newStub("b", log), newStub("c", log)
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, NewManager(a, b, c))
	waitStarted(t, a, b, c)

	cancel()

	require.ErrorIs(t, waitRun(t, done), context.Canceled)
	assert.Equal(t, []string{"stop c", "stop b", "stop a"}, log.stops())
}

func TestAFailingServiceStopsTheOthersAndItsErrorIsReturned(t *testing.T) {
	log := &events{}
	a, b, c := newStub("a", log), newStub("b", log), newStub("c", log)
	done := runAsync(context.Background(), NewManager(a, b, c))
	waitStarted(t, a, b, c)

	b.fail <- errBoom

	require.ErrorIs(t, waitRun(t, done), errBoom)
	assert.Equal(t, []string{"stop c", "stop b", "stop a"}, log.stops(), "every service gets Stop, in reverse order")
}

func TestAServiceThatReturnsNilStopsTheOthers(t *testing.T) {
	log := &events{}
	a, b := newStub("a", log), newStub("b", log)
	done := runAsync(context.Background(), NewManager(a, b))
	waitStarted(t, a, b)

	b.fail <- nil

	require.NoError(t, waitRun(t, done))
	assert.Equal(t, []string{"stop b", "stop a"}, log.stops(), "every service gets Stop, in reverse order")
}

func TestCancellingTheContextStopsEveryServiceAndReturnsTheCause(t *testing.T) {
	log := &events{}
	a, b, c := newStub("a", log), newStub("b", log), newStub("c", log)
	ctx, cancel := context.WithCancelCause(context.Background())
	done := runAsync(ctx, NewManager(a, b, c))
	waitStarted(t, a, b, c)

	cancel(errStop)

	require.ErrorIs(t, waitRun(t, done), errStop)
	assert.Equal(t, []string{"stop c", "stop b", "stop a"}, log.stops())
}

func TestRunReturnsOnlyAfterEveryServiceRanItsRun(t *testing.T) {
	log := &events{}
	a := newStub("a", log)
	a.release = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, NewManager(a))
	waitStarted(t, a)

	cancel()
	<-a.stopped

	select {
	case <-done:
		require.FailNow(t, "the manager returned while the service's Run was still running")
	case <-time.After(2 * time.Millisecond):
	}

	close(a.release)
	require.ErrorIs(t, waitRun(t, done), context.Canceled)
	assert.Equal(t, []string{"stop a", "returned a"}, log.list())
}

// A signal is handled by the manager, which stops the services and returns nil. The test
// sends the signal only after every service has started, which is after the manager has
// registered its signal handling.
func TestSignalStopsEveryServiceAndReturnsNil(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			log := &events{}
			a, b := newStub("a", log), newStub("b", log)
			done := runAsync(context.Background(), NewManager(a, b))
			waitStarted(t, a, b)

			require.NoError(t, syscall.Kill(os.Getpid(), sig))

			require.NoError(t, waitRun(t, done))
			assert.Equal(t, []string{"stop b", "stop a"}, log.stops())
		})
	}
}

// Add registers a service from its run and stop functions. The manager calls the stop
// function when the context ends.
func TestAddRegistersAServiceWithItsStopFunction(t *testing.T) {
	var (
		mu      sync.Mutex
		ran     bool
		stopped bool
	)
	release := make(chan struct{})
	var once sync.Once

	m := NewManager()
	m.Add(
		func(_ context.Context) error {
			<-release
			mu.Lock()
			defer mu.Unlock()
			ran = true
			return nil
		},
		func() {
			mu.Lock()
			stopped = true
			mu.Unlock()
			once.Do(func() { close(release) })
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, m)
	cancel()

	require.ErrorIs(t, waitRun(t, done), context.Canceled)
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, stopped, "the stop function must be called")
	assert.True(t, ran, "the run function must have returned before Run did")
}
