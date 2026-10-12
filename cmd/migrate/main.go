// Command migrate applies the database migrations and exits.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tuanvumaihuynh/runcoil/internal/log"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error running migration: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

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

	logger.InfoContext(ctx, "running migrations")
	if err := postgres.Migrate(pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	logger.InfoContext(ctx, "migrations completed successfully")

	return nil
}
