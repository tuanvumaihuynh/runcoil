// Command runcoil runs the Runcoil server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tuanvumaihuynh/runcoil/internal/engine"
	"github.com/tuanvumaihuynh/runcoil/internal/log"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres"
	"github.com/tuanvumaihuynh/runcoil/internal/worker"
	"github.com/tuanvumaihuynh/runcoil/pkg/service"
)

// The services the manager runs must satisfy service.Service.
var (
	_ service.Service = (*engine.Service)(nil)
	_ service.Service = (*worker.Service)(nil)
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error running application: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := NewConfig()
	if err != nil {
		return fmt.Errorf("new config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	logger, err := log.NewLogger(cfg.Log)
	if err != nil {
		return fmt.Errorf("new logger: %w", err)
	}

	pool, err := postgres.NewPgxPool(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("new pgx pool: %w", err)
	}
	defer pool.Close()

	// PostgreSQL holds the system of record: the tasks, the runs, and the schedules. The
	// worker instances and the signals live in this process, which holds the worker streams.
	db := postgres.NewClient(pool)
	tasks := postgres.NewTaskStore(db)
	runs := postgres.NewRunStore(db)
	schedules := postgres.NewScheduleStore(db)

	// The engine is registered first. The services are told to stop in reverse registration
	// order, so the worker service is told first. The manager returns after every service
	// has stopped, and the pool is closed after that. The run store is also the run queue:
	// the runs table is the queue.
	manager := service.NewManager(
		engine.NewService(cfg.Engine, logger, tasks, runs, runs, schedules),
		worker.NewService(cfg.Worker, logger, runs),
	)

	// The manager drains on the first signal. A second one exits at once.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go exitOnSecondSignal(ctx, logger, signals, os.Exit)

	if err := manager.Run(ctx); err != nil {
		return fmt.Errorf("run services: %w", err)
	}

	logger.InfoContext(ctx, "server is stopped")

	return nil
}

// exitOnSecondSignal calls exit with code 1 once the second signal arrives.
func exitOnSecondSignal(ctx context.Context, logger *slog.Logger, signals <-chan os.Signal, exit func(code int)) {
	<-signals
	<-signals
	logger.ErrorContext(ctx, "second signal received, exiting before the drain ends")
	exit(1)
}
