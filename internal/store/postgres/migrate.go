package postgres

import (
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies every pending migration.
func Migrate(pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrationFS)

	if err := goose.SetDialect("pgx"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	stdDB := stdlib.OpenDBFromPool(pool)
	if err := goose.Up(stdDB, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	return nil
}
