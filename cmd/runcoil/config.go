package main

import (
	"errors"
	"fmt"

	"github.com/tuanvumaihuynh/runcoil/internal/engine"
	"github.com/tuanvumaihuynh/runcoil/internal/log"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres"
	"github.com/tuanvumaihuynh/runcoil/internal/worker"
	"github.com/tuanvumaihuynh/runcoil/pkg/cmdutil"

	_ "embed"
)

//go:embed config.yml
var defaultConfigBytes []byte

// Config is the configuration of the server.
type Config struct {
	Log      log.Config      `yaml:"log"`
	Postgres postgres.Config `yaml:"postgres"`
	Engine   engine.Config   `yaml:"engine"`
	Worker   worker.Config   `yaml:"worker"`
}

// Validate checks every section of the configuration, and the rules that span two of them.
func (c *Config) Validate() error {
	if err := c.Log.Validate(); err != nil {
		return fmt.Errorf("log: %w", err)
	}

	if err := c.Postgres.Validate(); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	if err := c.Engine.Validate(); err != nil {
		return fmt.Errorf("engine: %w", err)
	}

	if err := c.Worker.Validate(); err != nil {
		return fmt.Errorf("worker: %w", err)
	}

	// A key's Run must still exist while the key is held, or a repeat answers with a Run
	// that the cleanup has removed.
	if c.Worker.Cleanup.RunRetention < c.Engine.Idempotency.Window {
		return errors.New("worker cleanup run retention must not be shorter than the engine idempotency window")
	}

	return nil
}

// NewConfig loads the configuration from the embedded defaults, the --config file, and environment variables.
func NewConfig() (*Config, error) {
	var cfg Config
	if err := cmdutil.LoadConfig("runcoil", defaultConfigBytes, &cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &cfg, nil
}
