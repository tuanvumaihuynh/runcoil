package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/log"
	"github.com/runcoil/runcoil/internal/store/postgres"
	"github.com/runcoil/runcoil/pkg/configutil"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running migration: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	var cfg Config
	loadErr := configutil.Load("runcoil-migrate", &cfg, os.Args[1:], os.LookupEnv)
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

	logger.InfoContext(ctx, "running migrations")

	if err := postgres.Migrate(pool); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	logger.InfoContext(ctx, "migrations completed successfully")

	return nil
}
