package main

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/api"
	"github.com/runcoil/runcoil/internal/dispatcher"
	"github.com/runcoil/runcoil/internal/log"
	"github.com/runcoil/runcoil/internal/scheduler"
	"github.com/runcoil/runcoil/internal/services/account"
	"github.com/runcoil/runcoil/internal/services/seed"
	"github.com/runcoil/runcoil/internal/store/postgres"
)

// Config is the configuration of the server.
type Config struct {
	AutoMigrate bool
	Log         log.Config
	Postgres    postgres.Config
	API         api.Config
	Dispatcher  dispatcher.Config
	Scheduler   scheduler.Config
	Seed        seed.Config
	Account     account.Config
}

// RegisterFlags registers the flags of every section.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&c.AutoMigrate, "auto-migrate", false, "Apply the pending migrations at boot.")
	c.Log.RegisterFlags(fs)
	c.Postgres.RegisterFlags(fs)
	c.API.RegisterFlags(fs)
	c.Dispatcher.RegisterFlags(fs)
	c.Scheduler.RegisterFlags(fs)
	c.Seed.RegisterFlags(fs)
	c.Account.RegisterFlags(fs)
}

// Validate checks every section of the configuration.
func (c *Config) Validate() error {
	if err := c.Log.Validate(); err != nil {
		return fmt.Errorf("log: %w", err)
	}

	if err := c.Postgres.Validate(); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	if err := c.API.Validate(); err != nil {
		return fmt.Errorf("api: %w", err)
	}

	if err := c.Dispatcher.Validate(); err != nil {
		return fmt.Errorf("dispatcher: %w", err)
	}

	if err := c.Scheduler.Validate(); err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	if err := c.Seed.Validate(); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	if err := c.Account.Validate(); err != nil {
		return fmt.Errorf("account: %w", err)
	}

	return nil
}
