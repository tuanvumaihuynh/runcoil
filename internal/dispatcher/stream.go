package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

// streamBuffer is how many messages wait for a stream: an Assign for every slot a worker
// may have, and room for the other messages.
const streamBuffer = domain.MaxSlots + 16

// stream is the Listen stream of one worker on this server. The fields after ended are
// guarded by Service.mu.
type stream struct {
	id       uuid.UUID
	workerID uuid.UUID
	taskIDs  []uuid.UUID
	slots    int
	out      chan streamMessage
	// ended is closed when the dispatcher ends the stream, so its Listen returns.
	ended chan struct{}

	// held is the attempts the worker runs on this stream: the ones it reported at the
	// attach and still holds, and the ones pushed since. A worker's calls reach the server
	// that holds its stream, so every attempt that ends passes through here.
	held map[domain.Lease]struct{}
}

// free returns how many more attempts the worker can take.
func (st *stream) free() int {
	return max(st.slots-len(st.held), 0)
}

// streamMessage is one message for a worker's stream: exactly one field is set.
type streamMessage struct {
	Assign *domain.Assign
	// LeaseLost names an attempt the worker runs but no longer holds.
	LeaseLost *domain.Lease
	Keepalive bool
}

// claimTarget is a stream with free slots, copied out of the map for one claim.
type claimTarget struct {
	workerID uuid.UUID
	streamID uuid.UUID
	taskIDs  []uuid.UUID
	free     int
}

// Listen attaches the worker to a new stream and sends its messages until ctx ends or
// the dispatcher stops. running is what the worker reports it is running: the leases it
// does not report count as lost and go back to the queue, and the reported attempts it
// no longer holds get a lease-lost message.
func (s *Service) Listen(
	ctx context.Context,
	worker domain.Worker,
	running []domain.Lease,
	send func(streamMessage) error,
) error {
	if s.isStopping() {
		return apperr.Unavailable("", "server is shutting down")
	}

	streamID := uuid.NewV7()
	st := &stream{
		id:       streamID,
		workerID: worker.ID,
		taskIDs:  worker.TaskIDs,
		slots:    worker.Slots,
		out:      make(chan streamMessage, streamBuffer),
		ended:    make(chan struct{}),
		held:     make(map[domain.Lease]struct{}, len(running)),
	}

	// The stream goes in the map while the worker row is locked, so the map follows the
	// order of the row lock: a slower, older Listen cannot replace a newer one.
	free := 0
	attached, err := s.workerStore.AttachStream(ctx, domain.AttachStreamParams{
		WorkerID:     worker.ID,
		DispatcherID: s.id,
		StreamID:     streamID,
		Running:      running,
		MaxLost:      s.cfg.MaxLostAttempts,
		Attached: func(notHeld []domain.Lease) {
			for _, l := range running {
				st.held[l] = struct{}{}
			}

			for _, l := range notHeld {
				delete(st.held, l)
			}

			free = st.free()
			s.put(st)
		},
	})
	if err != nil {
		s.drop(worker.ID, streamID)
		return attachError(err)
	}
	defer s.drop(worker.ID, streamID)

	s.logger.InfoContext(ctx, "stream attached",
		slog.String("worker_id", worker.ID.String()),
		slog.String("stream_id", streamID.String()),
		slog.String("app", worker.App),
		slog.Int("running", len(running)),
		slog.Int("not_held", len(attached.NotHeld)),
		slog.Int("unknown_requeued", attached.Requeued),
		slog.Int("unknown_failed", attached.Failed),
		slog.Int("free_slots", free),
	)

	attachedAt := time.Now()
	err = s.pump(ctx, st, attached.NotHeld, send)

	s.logger.InfoContext(ctx, "stream detached",
		slog.String("worker_id", worker.ID.String()),
		slog.String("stream_id", streamID.String()),
		slog.Duration("duration", time.Since(attachedAt)),
		slog.Any("error", err),
	)

	return err
}

// pump sends the messages of st until the worker goes, the server stops, or the
// dispatcher ends the stream. It first tells the worker which of the attempts it reported
// are no longer its own.
func (s *Service) pump(ctx context.Context, st *stream, notHeld []domain.Lease, send func(streamMessage) error) error {
	for _, l := range notHeld {
		s.logger.InfoContext(ctx, "lease lost sent",
			slog.String("worker_id", st.workerID.String()),
			slog.String("run_id", l.RunID.String()),
			slog.Int64("epoch", l.Epoch),
		)

		if err := send(streamMessage{LeaseLost: &l}); err != nil {
			return fmt.Errorf("send lease lost: %w", err)
		}
	}

	keepalive := time.NewTicker(s.cfg.HeartbeatInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.stopped:
			return nil
		case <-st.ended:
			return errStreamEnded
		case msg := <-st.out:
			if err := send(msg); err != nil {
				return fmt.Errorf("send message: %w", err)
			}
		case <-keepalive.C:
			if err := send(streamMessage{Keepalive: true}); err != nil {
				return fmt.Errorf("send keepalive: %w", err)
			}
		}
	}
}

// put makes st the worker's stream on this server.
func (s *Service) put(st *stream) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.streams[st.workerID] = st
}

// drop removes the worker's stream if it is still streamID, so an old stream that ends
// late never removes a newer one.
func (s *Service) drop(workerID, streamID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.streams[workerID]; ok && st.id == streamID {
		delete(s.streams, workerID)
	}
}

// end removes the worker's stream if it is still streamID and ends its Listen. The worker
// reconnects, and the reconcile of its next Listen puts the attempts it was never sent
// back in the queue, counted as lost.
func (s *Service) end(workerID, streamID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.streams[workerID]; ok && st.id == streamID {
		s.endLocked(st)
	}
}

// endLocked is end for a caller that holds s.mu.
func (s *Service) endLocked(st *stream) {
	delete(s.streams, st.workerID)
	close(st.ended)
}

// claimable returns the streams with free slots.
func (s *Service) claimable() []claimTarget {
	s.mu.Lock()
	defer s.mu.Unlock()

	targets := make([]claimTarget, 0, len(s.streams))
	for _, st := range s.streams {
		if free := st.free(); free > 0 {
			targets = append(targets, claimTarget{
				workerID: st.workerID,
				streamID: st.id,
				taskIDs:  st.taskIDs,
				free:     free,
			})
		}
	}

	return targets
}

// push sends the claimed attempts to the stream that claimed them. When one cannot be
// sent, the stream ends: the worker reconnects without reporting it, so the reconcile
// puts it back in the queue, counted as lost.
func (s *Service) push(ctx context.Context, workerID, streamID uuid.UUID, assigns []domain.Assign) {
	if len(assigns) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.streams[workerID]
	if !ok || st.id != streamID {
		s.logger.WarnContext(ctx, "stream gone before assign",
			slog.String("worker_id", workerID.String()),
			slog.Int("runs", len(assigns)),
		)

		return
	}

	for _, a := range assigns {
		select {
		case st.out <- streamMessage{Assign: &a}:
			st.held[a.Lease] = struct{}{}
			s.logger.DebugContext(ctx, "run assigned",
				slog.String("run_id", a.Lease.RunID.String()),
				slog.Int64("epoch", a.Lease.Epoch),
				slog.String("task_name", a.TaskName),
				slog.String("worker_id", workerID.String()),
			)
		default:
			s.logger.WarnContext(ctx, "stream buffer full, ending the stream",
				slog.String("worker_id", workerID.String()),
				slog.String("run_id", a.Lease.RunID.String()),
			)
			s.endLocked(st)

			return
		}
	}
}

// release gives back the slot of an attempt that ended, or that the worker no longer
// holds. An attempt the worker's stream does not hold frees nothing, so a release that
// arrives after a reconnect never frees a slot twice.
func (s *Service) release(workerID uuid.UUID, lease domain.Lease) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.streams[workerID]; ok {
		delete(st.held, lease)
	}
}

// endWorker ends the worker's stream on this server, whichever it is.
func (s *Service) endWorker(workerID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st, ok := s.streams[workerID]; ok {
		s.endLocked(st)
	}
}

var errStreamEnded = apperr.Unavailable("", "stream ended, reconnect")

func attachError(err error) error {
	if errors.Is(err, domain.ErrSessionEnded) {
		return apperr.Unauthenticated(apperr.CodeUnauthenticated, "worker session has ended, register again")
	}

	return fmt.Errorf("attach stream: %w", err)
}
