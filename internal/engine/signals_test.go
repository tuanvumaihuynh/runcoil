package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNotifyWakesSubscriberOnItsTopic(t *testing.T) {
	s := newInProcessSignals()
	runs, stopRuns := s.Subscribe("runs")
	defer stopRuns()
	schedules, stopSchedules := s.Subscribe("schedules")
	defer stopSchedules()

	s.Notify("runs")
	select {
	case <-runs:
	case <-time.After(time.Second):
		require.Fail(t, "a notified topic must wake its subscriber")
	}

	select {
	case <-schedules:
		require.Fail(t, "a different topic must not wake the subscriber")
	default:
	}
}

func TestRepeatedNotifyCountsAsOneWakeUp(t *testing.T) {
	s := newInProcessSignals()
	ch, stop := s.Subscribe("runs")
	defer stop()
	s.Notify("runs")
	s.Notify("runs")
	s.Notify("runs")
	<-ch
	select {
	case <-ch:
		require.Fail(t, "signals are not queued: three notifications make one wake-up")
	default:
	}
}

func TestNotifyWithoutSubscribersDoesNotBlock(t *testing.T) {
	newInProcessSignals().Notify("nobody")
}

// Once a subscriber unsubscribes, a notification does not reach its channel.
func TestUnsubscribeStopsDelivery(t *testing.T) {
	s := newInProcessSignals()
	ch, stop := s.Subscribe("runs")
	stop()
	s.Notify("runs")
	select {
	case <-ch:
		require.Fail(t, "an unsubscribed channel must receive nothing")
	default:
	}

	require.Empty(t, s.subs, "an unsubscribed topic with no subscribers is dropped from memory")
}

// A finished Run wakes the WaitRun calls of that Run only, and an ended wait leaves no topic behind.
func TestRunFinishedTopicWakesOnlyItsOwnRun(t *testing.T) {
	s := newInProcessSignals()
	first, stopFirst := s.Subscribe(topicRunFinished("run-1"))
	second, stopSecond := s.Subscribe(topicRunFinished("run-2"))

	s.Notify(topicRunFinished("run-1"))
	select {
	case <-first:
	case <-time.After(time.Second):
		require.Fail(t, "a finished Run must wake the waiters of that Run")
	}

	select {
	case <-second:
		require.Fail(t, "a finished Run must not wake the waiters of another Run")
	default:
	}

	stopFirst()
	stopSecond()
	require.Empty(t, s.subs, "every ended wait leaves no topic in memory")
}
