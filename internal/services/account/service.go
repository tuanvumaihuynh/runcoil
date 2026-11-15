package account

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/store"
)

// The errors the service returns to clients.
var (
	ErrWrongLogin  = apperr.Unauthenticated(apperr.CodeUnauthenticated, "wrong login or password")
	ErrNotSignedIn = apperr.Unauthenticated(apperr.CodeUnauthenticated, "not signed in")
)

const minSessionMaxInactive = 5 * time.Minute

// Config is the configuration of the accounts and their sessions.
type Config struct {
	SessionMaxLifetime time.Duration
	SessionMaxInactive time.Duration

	BootstrapAdminUsername string
	BootstrapAdminPassword string
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&c.SessionMaxLifetime, "session.max-lifetime", 30*24*time.Hour, "Longest time a dashboard session lasts.")
	fs.DurationVar(&c.SessionMaxInactive, "session.max-inactive", 7*24*time.Hour, "Time without activity after which a dashboard session ends.")
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.SessionMaxLifetime <= 0 {
		return errors.New("max lifetime must be positive")
	}

	// A session records its activity at most once a minute, so a shorter limit would end
	// sessions in use.
	if c.SessionMaxInactive < minSessionMaxInactive {
		return errors.New("max inactive must be at least 5m")
	}

	return nil
}

// Service signs users in and out, manages their passwords and decides what they may do.
type Service struct {
	cfg          Config
	userStore    store.UserStore
	sessionStore store.SessionStore
	projectStore store.ProjectStore
	now          func() time.Time

	dummyHash string
}

// New creates the account service.
func New(cfg Config, userStore store.UserStore, sessionStore store.SessionStore, projectStore store.ProjectStore) *Service {
	dummyHash, _ := HashPassword(context.Background(), "")

	return &Service{
		cfg:          cfg,
		userStore:    userStore,
		sessionStore: sessionStore,
		projectStore: projectStore,
		now:          time.Now,
		dummyHash:    dummyHash,
	}
}
