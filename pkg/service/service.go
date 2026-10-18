package service

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// ErrForcedStop is returned by Run when a signal arrives while the services are stopping.
// Run returns at once, without waiting for the services that have not returned.
var ErrForcedStop = errors.New("a signal arrived while the services were stopping")

// Service represents a long-running application component managed as part of
// the application lifecycle.
type Service interface {
	// Run must block until the service stops or encounters an error.
	Run(ctx context.Context) error
	// Stop initiates a graceful shutdown and must eventually cause Run to return.
	Stop()
}

// Manager runs a group of services concurrently and coordinates their shutdown.
type Manager struct {
	services []Service
}

// NewManager creates a Manager containing the provided services.
//
// Services should be registered in dependency order. A foundational service
// should normally be registered before services that depend on it so that it
// is stopped after its dependants.
func NewManager(services ...Service) *Manager {
	return &Manager{services: services}
}

// Add registers a service using the provided lifecycle functions.
func (m *Manager) Add(run func(ctx context.Context) error, stop func()) {
	m.services = append(m.services, &service{run: run, stop: stop})
}

// Run starts all registered services concurrently and blocks until one of the
// following occurs:
//
//   - the parent context is canceled;
//   - an OS interrupt signal is received;
//   - one of the services returns.
//
// Once shutdown begins, all services are stopped sequentially in reverse
// registration order. Run waits until every service's Run method has returned. A signal
// that arrives during the stop makes Run return ErrForcedStop at once, without waiting.
func (m *Manager) Run(ctx context.Context) error {
	if len(m.services) == 0 {
		return nil
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)

	serviceCount := len(m.services)
	results := make(chan error, serviceCount)

	for _, s := range m.services {
		go func(s Service) {
			results <- s.Run(ctx)
		}(s)
	}

	var (
		firstErr error
		received int
	)

	// Wait until context is done or a service exits or an OS signal arrives.
	select {
	case <-interrupt:
	case <-ctx.Done():
		firstErr = context.Cause(ctx)
	case firstErr = <-results:
		received++
	}

	// Stop services in reverse registration order.
	for i := serviceCount - 1; i >= 0; i-- {
		m.services[i].Stop()
	}

	// Wait until every Run method has returned. A signal during the stop ends Run at
	// once, without waiting for the services.
	for received < serviceCount {
		select {
		case <-results:
			received++
		case <-interrupt:
			return ErrForcedStop
		}
	}

	return firstErr
}

type service struct {
	run  func(ctx context.Context) error
	stop func()
}

func (s *service) Run(ctx context.Context) error {
	return s.run(ctx)
}

func (s *service) Stop() {
	s.stop()
}
