package main

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/log"
	"github.com/runcoil/runcoil/internal/store/postgres"
)

// Config is the configuration of the migration command.
type Config struct {
	Log      log.Config
	Postgres postgres.Config
}

// RegisterFlags registers the flags of every section.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	c.Log.RegisterFlags(fs)
	c.Postgres.RegisterFlags(fs)
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
