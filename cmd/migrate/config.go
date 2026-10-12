package main

import (
	"fmt"

	"github.com/tuanvumaihuynh/runcoil/internal/log"
	"github.com/tuanvumaihuynh/runcoil/internal/storage/postgres"
	"github.com/tuanvumaihuynh/runcoil/pkg/cmdutil"

	_ "embed"
)

//go:embed config.yml
var defaultConfigBytes []byte

// Config is the configuration of the migration command.
type Config struct {
	Log      log.Config      `yaml:"log"`
	Postgres postgres.Config `yaml:"postgres"`
}

// Validate checks every section of the configuration.
func (c *Config) Validate() error {
	if err := c.Log.Validate(); err != nil {
		return fmt.Errorf("log: %w", err)
	}

	if err := c.Postgres.Validate(); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	return nil
}

// NewConfig loads the configuration from the embedded defaults, the --config file, and environment variables.
func NewConfig() (*Config, error) {
	var cfg Config
	if err := cmdutil.LoadConfig("migrate", defaultConfigBytes, &cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &cfg, nil
}
