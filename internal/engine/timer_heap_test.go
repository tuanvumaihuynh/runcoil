package engine

import (
	"container/heap"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// timerBase is the fixed tick that the test timers are offset from.
var timerBase = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

// timerAt returns a timer named by its offset, due the offset after timerBase.
func timerAt(offset time.Duration) domain.ScheduleTimer {
	return domain.ScheduleTimer{
		Ref:  domain.TaskRef{WorkerID: "demo", TaskID: fmt.Sprintf("t%d", int(offset/time.Second))},
		Tick: timerBase.Add(offset),
	}
}

// checkPopOrder checks that the popped timers are the timers of the offsets, in ascending order.
func checkPopOrder(t *testing.T, got []domain.ScheduleTimer, offsets []time.Duration) {
	t.Helper()
	require.Len(t, got, len(offsets), "popped timers")

	sorted := slices.Clone(offsets)
	slices.Sort(sorted)
	for i, offset := range sorted {
		want := timerAt(offset)
		require.True(t, want.Tick.Equal(got[i].Tick), "pop %d at %v, want %v", i, got[i].Tick, want.Tick)
		require.Equal(t, want.Ref, got[i].Ref, "pop %d", i)
	}
}

// popAll pops every timer, earliest first.
func popAll(h *timerHeap) []domain.ScheduleTimer {
	var out []domain.ScheduleTimer
	for h.Len() > 0 {
		out = append(out, heap.Pop(h).(domain.ScheduleTimer))
	}

	return out
}

// orderCases are offsets in mixed order, one list per shape. Each is popped in ascending order.
var orderCases = []struct {
	name    string
	offsets []time.Duration
}{
	{"empty", nil},
	{"one timer", []time.Duration{7 * time.Second}},
	{"already ascending", []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second}},
	{"descending", []time.Duration{3 * time.Second, 2 * time.Second, 1 * time.Second}},
	{"mixed", []time.Duration{5 * time.Second, 1 * time.Second, 4 * time.Second, 2 * time.Second, 3 * time.Second}},
	// (7*i) mod 13 for i from 0 to 12 is a permutation of 0 to 12, so there are no ties.
	{"thirteen in a scrambled order", scrambled(13)},
}

// scrambled returns the offsets 0 to n-1 seconds in a fixed scrambled order, for n
// coprime to 7.
func scrambled(n int) []time.Duration {
	out := make([]time.Duration, 0, n)
	for i := range n {
		out = append(out, time.Duration((7*i)%n)*time.Second)
	}

	return out
}

func TestTimerHeapPopsEarliestTickFirst(t *testing.T) {
	for _, tt := range orderCases {
		t.Run(tt.name, func(t *testing.T) {
			var h timerHeap
			for _, offset := range tt.offsets {
				heap.Push(&h, timerAt(offset))
			}

			checkPopOrder(t, popAll(&h), tt.offsets)
		})
	}
}

func TestTimerHeapInitOrdersASlice(t *testing.T) {
	for _, tt := range orderCases {
		t.Run(tt.name, func(t *testing.T) {
			h := make(timerHeap, 0, len(tt.offsets))
			for _, offset := range tt.offsets {
				h = append(h, timerAt(offset))
			}

			heap.Init(&h)
			checkPopOrder(t, popAll(&h), tt.offsets)
		})
	}
}

// Timers with equal ticks are all kept: each pushed timer comes out exactly once.
func TestTimerHeapKeepsEqualTicks(t *testing.T) {
	tests := []struct {
		name    string
		offsets []time.Duration
	}{
		{"all equal", []time.Duration{0, 0, 0, 0}},
		{"equal ticks interleaved", []time.Duration{2 * time.Second, 1 * time.Second, 2 * time.Second, 1 * time.Second, 3 * time.Second, 2 * time.Second}},
		{"a tie with the earliest tick", []time.Duration{0, 5 * time.Second, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var h timerHeap
			pushed := make(map[domain.TaskRef]int)
			for i, offset := range tt.offsets {
				// The index keeps every Task distinct, so a lost or doubled timer shows.
				timer := timerAt(offset)
				timer.Ref.TaskID = fmt.Sprintf("%s-%d", timer.Ref.TaskID, i)
				pushed[timer.Ref]++
				heap.Push(&h, timer)
			}

			got := popAll(&h)
			require.Len(t, got, len(tt.offsets), "popped timers")

			seen := make(map[domain.TaskRef]int)
			for i, timer := range got {
				seen[timer.Ref]++
				if i > 0 {
					require.False(t, timer.Tick.Before(got[i-1].Tick),
						"pop %d at %v came after %v", i, timer.Tick, got[i-1].Tick)
				}
			}

			for ref, n := range pushed {
				require.Equal(t, n, seen[ref], "timer %v popped", ref)
			}
		})
	}
}

func TestTimerHeapOrdersPushesAfterPops(t *testing.T) {
	var h timerHeap
	heap.Push(&h, timerAt(10*time.Second))
	heap.Push(&h, timerAt(20*time.Second))
	heap.Push(&h, timerAt(30*time.Second))

	first := heap.Pop(&h).(domain.ScheduleTimer)
	require.True(t, timerAt(10*time.Second).Tick.Equal(first.Tick), "first pop at %v, want 10s", first.Tick)

	// Earlier than the timers still held, and between them.
	heap.Push(&h, timerAt(5*time.Second))
	heap.Push(&h, timerAt(15*time.Second))

	want := []time.Duration{5, 15, 20, 30}
	for i, offset := range want {
		got := heap.Pop(&h).(domain.ScheduleTimer)
		require.True(t, timerAt(offset*time.Second).Tick.Equal(got.Tick), "pop %d at %v, want %ds", i, got.Tick, offset)
	}

	require.Equal(t, 0, h.Len(), "len after all pops")
}

func TestTimerHeapLenFollowsPushesAndPops(t *testing.T) {
	steps := []struct {
		push    bool
		wantLen int
	}{
		{push: true, wantLen: 1},
		{push: true, wantLen: 2},
		{push: true, wantLen: 3},
		{push: false, wantLen: 2},
		{push: true, wantLen: 3},
		{push: false, wantLen: 2},
		{push: false, wantLen: 1},
		{push: false, wantLen: 0},
	}

	var h timerHeap
	for i, step := range steps {
		if step.push {
			heap.Push(&h, timerAt(time.Duration(i)*time.Second))
		} else {
			heap.Pop(&h)
		}

		require.Equal(t, step.wantLen, h.Len(), "step %d: len", i)
	}
}
