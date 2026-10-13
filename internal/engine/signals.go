package engine

import "sync"

// Topics of the Signals interface.
const (
	// TopicRuns is raised when a Run becomes ready, or a slot frees, so the dispatcher runs a pass.
	TopicRuns = "runs"
	// TopicSchedules is raised when a registration may have changed schedules, so the scheduler polls now.
	TopicSchedules = "schedules"
)

// topicRunFinished is the topic raised when the Run with this ID may have finished, so the
// WaitRun calls of that Run check again.
func topicRunFinished(runID string) string {
	return "run-finished:" + runID
}

// Signals is the wake-up channel between the parts of the server. Every consumer also
// polls on its own interval, so a signal that is lost costs latency only, never
// correctness.
type Signals interface {
	// Notify wakes the subscribers of the topic. It does not block.
	Notify(topic string)
	// Subscribe returns a channel that receives when the topic is notified, and a
	// function that ends the subscription. The caller calls it when it stops listening.
	Subscribe(topic string) (<-chan struct{}, func())
}

// inProcessSignals is the Signals that NewService creates, and that the engine's parts share.
// It keeps a channel per subscriber, which holds at most one wake-up. A signal is never
// queued: a subscriber that is awake when it is raised sees it as one wake-up, and any
// state it needs is read from the store.
type inProcessSignals struct {
	mu   sync.Mutex
	subs map[string]map[*subscription]struct{}
}

// subscription is one subscriber's channel, kept in its topic until unsubscribed.
type subscription struct {
	ch chan struct{}
}

// newInProcessSignals creates a signal hub with no subscribers.
func newInProcessSignals() *inProcessSignals {
	return &inProcessSignals{subs: make(map[string]map[*subscription]struct{})}
}

// Notify wakes the subscribers of the topic that are not already awake. It does not block.
func (s *inProcessSignals) Notify(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subs[topic] {
		select {
		case sub.ch <- struct{}{}:
		default:
		}
	}
}

// Subscribe returns a channel that receives a value when the topic is notified, and a
// function that ends the subscription. After the function returns, nothing is sent to
// the channel, and the subscriber is no longer kept in memory.
func (s *inProcessSignals) Subscribe(topic string) (<-chan struct{}, func()) {
	sub := &subscription{ch: make(chan struct{}, 1)}
	s.mu.Lock()
	if s.subs[topic] == nil {
		s.subs[topic] = make(map[*subscription]struct{})
	}

	s.subs[topic][sub] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs[topic], sub)
		if len(s.subs[topic]) == 0 {
			delete(s.subs, topic)
		}
	}

	return sub.ch, unsubscribe
}
