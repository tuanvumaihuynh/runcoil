package engine

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

type taskKey struct {
	workerID string
	taskID   string
}

// errStreamClosed reports that a stream ended before it could take a message.
var errStreamClosed = errors.New("listen stream closed")

// errSendTimeout reports that a stream did not take a message in time.
var errSendTimeout = errors.New("listen stream did not take the message in time")

// streamMessage is one message for an instance's Listen stream. Exactly one of
// Run and Cancel is set. Run gives the Run's attempt with its finished Steps and
// the Steps that have failed tries. Cancel asks the worker to stop the attempt named.
type streamMessage struct {
	Run    *domain.Run
	Steps  []domain.StepRecord
	Cancel *domain.AttemptRef
}

// outbound is one message handed to a stream, and the channel that reports whether
// the stream wrote it.
type outbound struct {
	Message streamMessage
	Result  chan<- error
}

// stream is one attached Listen stream of an instance. The dispatcher and the sweep
// call Send. The Listen handler reads Outbox and writes each message to the wire, then
// reports the result on the outbound message. A stream lives in the process that holds
// its connection.
type stream struct {
	out  chan outbound
	done chan struct{}
	once sync.Once
}

// candidate is an instance that serves a Task and has an open stream: the instance's
// registered slots and the stream that carries its messages.
type candidate struct {
	InstanceID string
	Slots      int
	Stream     *stream
}

// newStream creates an open stream.
func newStream() *stream {
	return &stream{
		out:  make(chan outbound),
		done: make(chan struct{}),
	}
}

// Send writes msg on the stream and returns once the stream has written it or failed.
// A stream that does not take the message within timeout is closed, so one slow
// worker cannot hold up the dispatch of every other Task.
func (s *stream) Send(ctx context.Context, msg streamMessage, timeout time.Duration) error {
	result := make(chan error, 1)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case s.out <- outbound{Message: msg, Result: result}:
	case <-s.done:
		return errStreamClosed
	case <-timer.C:
		s.Close()
		return errSendTimeout
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-result:
		return err
	case <-s.done:
		// The stream may have written the message just before it closed.
		select {
		case err := <-result:
			return err
		default:
			return errStreamClosed
		}
	case <-timer.C:
		s.Close()
		return errSendTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Outbox carries the messages the handler must write.
func (s *stream) Outbox() <-chan outbound {
	return s.out
}

// Done is closed when the stream ends.
func (s *stream) Done() <-chan struct{} {
	return s.done
}

// Close ends the stream. It is safe to call more than once.
func (s *stream) Close() {
	s.once.Do(func() { close(s.done) })
}

// Closed reports whether the stream has ended.
func (s *stream) Closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// instances is the in-process record of the workers: what each instance declared, the
// stream it listens on, and when it was last heard from. Its state is lost when the
// process ends, as its streams are. A stream lives where its connection is, so the
// record lives in the same process, and the process that holds a stream ends it. The
// record is safe for concurrent use, and no method is called inside a Mutate closure,
// so no call into the record runs while a Run is locked.
type instances struct {
	mu      sync.Mutex
	records map[string]*instance
}

// instance is one worker process.
type instance struct {
	id       string
	workerID string
	taskIDs  map[string]struct{}
	slots    int
	stream   *stream
	lastSeen time.Time
}

// newInstances creates an empty record of the workers.
func newInstances() *instances {
	return &instances{records: make(map[string]*instance)}
}

// Register stores what an instance declared. A new declaration replaces the old
// one. A stream the instance already has stays open. It reports whether the instance
// was not in the record before, so an instance that was evicted counts as new again.
func (d *instances) Register(reg domain.Registration, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	inst, ok := d.records[reg.InstanceID]
	if !ok {
		inst = &instance{id: reg.InstanceID}
		d.records[reg.InstanceID] = inst
	}

	inst.lastSeen = now
	inst.workerID = reg.WorkerID
	inst.slots = reg.Slots
	inst.taskIDs = make(map[string]struct{}, len(reg.Tasks))
	for _, task := range reg.Tasks {
		inst.taskIDs[task.TaskID] = struct{}{}
	}

	return !ok
}

// Attach gives the instance a new stream and ends the stream it had. It reports
// false when the instance is not registered.
func (d *instances) Attach(instanceID string, now time.Time) (*stream, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	inst, ok := d.records[instanceID]
	if !ok {
		return nil, false
	}

	if inst.stream != nil {
		inst.stream.Close()
	}

	s := newStream()
	inst.stream = s
	inst.lastSeen = now
	return s, true
}

// Detach removes s as the stream of the instance, when it still is one, and closes it.
func (d *instances) Detach(instanceID string, s *stream) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if inst, ok := d.records[instanceID]; ok && inst.stream == s {
		inst.stream = nil
	}

	s.Close()
}

// Touch records a heartbeat of the instance. It reports false when the instance is unknown.
func (d *instances) Touch(instanceID string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	inst, ok := d.records[instanceID]
	if !ok {
		return false
	}

	inst.lastSeen = now
	return true
}

// StreamOf returns the open stream of the instance, or nil when it has none.
func (d *instances) StreamOf(instanceID string) *stream {
	d.mu.Lock()
	defer d.mu.Unlock()

	inst, ok := d.records[instanceID]
	if !ok || inst.stream == nil || inst.stream.Closed() {
		return nil
	}

	return inst.stream
}

// Attached returns the IDs of the instances that have an open stream, sorted.
func (d *instances) Attached() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var ids []string
	for id, inst := range d.records {
		if inst.stream != nil && !inst.stream.Closed() {
			ids = append(ids, id)
		}
	}

	sort.Strings(ids)
	return ids
}

// Serving returns the instances that serve the Task and have an open stream, sorted by ID.
func (d *instances) Serving(ref taskKey) []candidate {
	d.mu.Lock()
	defer d.mu.Unlock()

	var out []candidate
	for _, inst := range d.records {
		if inst.stream == nil || inst.stream.Closed() || inst.workerID != ref.workerID {
			continue
		}

		if _, ok := inst.taskIDs[ref.taskID]; !ok {
			continue
		}

		out = append(out, candidate{InstanceID: inst.id, Slots: inst.slots, Stream: inst.stream})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].InstanceID < out[j].InstanceID })
	return out
}

// Sweep enforces instance liveness. An instance with an open stream that has been
// silent for longer than one lease duration loses its stream, so it gets no more
// dispatches. An instance with no stream that has been silent for longer than
// evictAfter is dropped, and it registers again if it returns. It reports how many
// streams it ended and the IDs of the instances it dropped.
func (d *instances) Sweep(now time.Time, lease, evictAfter time.Duration) (closed int, evicted []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for id, inst := range d.records {
		silent := now.Sub(inst.lastSeen)
		if inst.stream != nil && !inst.stream.Closed() && silent > lease {
			inst.stream.Close()
			closed++
		}

		if inst.stream == nil && silent > evictAfter {
			delete(d.records, id)
			evicted = append(evicted, id)
		}
	}

	sort.Strings(evicted)
	return closed, evicted
}

// CloseAll ends every open stream.
func (d *instances) CloseAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, inst := range d.records {
		if inst.stream != nil {
			inst.stream.Close()
		}
	}
}

// queuedReasonOf is why a Run of the Task waits now: no worker serves it, or the workers are full.
func queuedReasonOf(inst *instances, ref taskKey) domain.QueuedReason {
	if len(inst.Serving(ref)) == 0 {
		return domain.QueuedReasonWaitingForWorker
	}

	return domain.QueuedReasonWaitingForCapacity
}

// streamEnds records why the dispatcher closed a stream, until the stream is detached.
type streamEnds struct {
	mu     sync.Mutex
	causes map[*stream]string
}

// newStreamEnds creates an empty record of the closes the dispatcher made.
func newStreamEnds() *streamEnds {
	return &streamEnds{causes: make(map[*stream]string)}
}

// note records the cause of a close the dispatcher is making. The first cause is kept.
func (s *streamEnds) note(sess *stream, cause string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.causes[sess]; !ok {
		s.causes[sess] = cause
	}
}

// take returns the recorded cause of a stream and forgets it, or "" when there is none.
func (s *streamEnds) take(sess *stream) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cause := s.causes[sess]
	delete(s.causes, sess)
	return cause
}
