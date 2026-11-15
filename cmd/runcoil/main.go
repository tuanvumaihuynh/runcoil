package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/api"
	"github.com/runcoil/runcoil/internal/build"
	"github.com/runcoil/runcoil/internal/dispatcher"
	"github.com/runcoil/runcoil/internal/log"
	"github.com/runcoil/runcoil/internal/scheduler"
	"github.com/runcoil/runcoil/internal/services/account"
	"github.com/runcoil/runcoil/internal/services/seed"
	"github.com/runcoil/runcoil/internal/store/postgres"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
	"github.com/runcoil/runcoil/pkg/configutil"
	"github.com/runcoil/runcoil/pkg/healthutil"
	"github.com/runcoil/runcoil/pkg/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running application: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cfg Config
	loadErr := configutil.Load("runcoil", &cfg, os.Args[1:], os.LookupEnv)
	if errors.Is(loadErr, pflag.ErrHelp) {
		return nil
	}

	if err := errors.Join(loadErr, cfg.Validate()); err != nil {
		return fmt.Errorf("invalid config:\n%w", err)
	}

	logger := log.NewLogger(cfg.Log)

	pool, err := postgres.NewPgxPool(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("new pgx pool: %w", err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		logger.InfoContext(ctx, "running migrations")

		if err := postgres.Migrate(pool); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	}

	db := postgres.NewClient(pool)
	queries := gen.Queries{}
	store := postgres.NewStore(db, queries)

	seedService := seed.New(cfg.Seed, logger, store)
	if err := seedService.Seed(ctx); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	cfg.Dispatcher.MaxLostAttempts = cfg.Scheduler.MaxLostAttempts
	dispatcherService := dispatcher.New(cfg.Dispatcher, logger, store, store)

	cfg.Scheduler.WorkerTTL = cfg.Dispatcher.WorkerTTL
	schedulerService := scheduler.New(cfg.Scheduler, logger, store, store)

	// The API serves the health checks of the whole process, so the probes call one port.
	checks := healthutil.New()
	checks.Add("postgres", pool.Ping)
	checks.Add("dispatcher", dispatcherService.Ready)
	checks.Add("scheduler", schedulerService.Ready)

	cfg.Account.BootstrapAdminUsername = cfg.Seed.AdminUsername
	cfg.Account.BootstrapAdminPassword = cfg.Seed.AdminPassword
	accountService := account.New(cfg.Account, store, store, store)

	apiService, err := api.New(cfg.API, logger, checks, accountService)
	if err != nil {
		return fmt.Errorf("new api service: %w", err)
	}

	// The scheduler and the dispatcher stop first, so /readyz fails while the API still
	// answers it.
	manager := service.NewManager(
		apiService,
		dispatcherService,
		schedulerService,
	)

	logger.InfoContext(ctx, "server is started",
		slog.String("version", build.Version),
		slog.String("commit", build.Commit),
	)

	if err := manager.Run(ctx); err != nil {
		return fmt.Errorf("run services: %w", err)
	}

	logger.InfoContext(ctx, "server is stopped")

	return nil
}
