package engine

import "github.com/tuanvumaihuynh/runcoil/internal/domain"

// timerHeap orders the loaded ticks by time.
type timerHeap []domain.ScheduleTimer

func (h timerHeap) Len() int { return len(h) }

func (h timerHeap) Less(i, j int) bool { return h[i].Tick.Before(h[j].Tick) }

func (h timerHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *timerHeap) Push(x any) { *h = append(*h, x.(domain.ScheduleTimer)) }

func (h *timerHeap) Pop() any { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }
