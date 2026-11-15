package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"
	"uuid"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services/account"
	"github.com/runcoil/runcoil/internal/store"
)

// Config is the configuration of the seed.
type Config struct {
	AdminUsername string
	AdminPassword string
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.AdminUsername, "bootstrap.admin-username", "admin", "Username of the first server admin, created at boot while no user exists.")
	fs.StringVar(&c.AdminPassword, "bootstrap.admin-password", "admin", "Password of the first server admin, created at boot while no user exists.")
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if err := account.ValidateUsername(c.AdminUsername); err != nil {
		return fmt.Errorf("admin username %w", err)
	}

	if c.AdminPassword == "" {
		return errors.New("admin password must not be empty")
	}

	if utf8.RuneCountInString(c.AdminPassword) > account.MaxPasswordLen {
		return fmt.Errorf("admin password must be at most %d characters", account.MaxPasswordLen)
	}

	return nil
}

// Service creates the first server admin and the project default.
type Service struct {
	cfg       Config
	logger    *slog.Logger
	userStore store.User
}

// New creates the seed service.
func New(cfg Config, logger *slog.Logger, userStore store.User) *Service {
	return &Service{
		cfg:       cfg,
		logger:    logger,
		userStore: userStore,
	}
}

// Seed creates the first server admin and the project default while no user exists.
func (s *Service) Seed(ctx context.Context) error {
	hash, err := account.HashPassword(ctx, s.cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	admin := domain.User{
		ID:           uuid.NewV7(),
		Username:     s.cfg.AdminUsername,
		PasswordHash: hash,
	}

	created, err := s.userStore.CreateFirstServerAdmin(ctx, admin, uuid.NewV7())
	if err != nil {
		return fmt.Errorf("create first server admin: %w", err)
	}

	if created {
		s.logger.InfoContext(ctx, "created the first server admin and the project default", slog.String("username", admin.Username))
	}

	return nil
}
