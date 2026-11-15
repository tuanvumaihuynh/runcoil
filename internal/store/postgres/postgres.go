package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
)

var sslModes = []string{"disable", "require", "verify-full"}

// Config is the PostgreSQL connection configuration.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DB       string
	SSLMode  string
	MaxConns int32
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Host, "postgres.host", "localhost", "PostgreSQL host.")
	fs.IntVar(&c.Port, "postgres.port", 5432, "PostgreSQL port.")
	fs.StringVar(&c.User, "postgres.user", "postgres", "PostgreSQL user.")
	fs.StringVar(&c.Password, "postgres.password", "postgres", "PostgreSQL password.")
	fs.StringVar(&c.DB, "postgres.db", "postgres", "PostgreSQL database.")
	fs.StringVar(&c.SSLMode, "postgres.ssl-mode", "disable", "TLS mode: "+strings.Join(sslModes, ", ")+".")
	fs.Int32Var(&c.MaxConns, "postgres.max-conns", 10, "Most connections in the pool.")
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.New("host is required")
	}

	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	if c.User == "" {
		return errors.New("user is required")
	}

	if c.Password == "" {
		return errors.New("password is required")
	}

	if c.DB == "" {
		return errors.New("db is required")
	}

	if !slices.Contains(sslModes, c.SSLMode) {
		return fmt.Errorf("ssl mode must be one of the following values: %s", strings.Join(sslModes, ", "))
	}

	if c.MaxConns <= 0 {
		return errors.New("max conns must be greater than 0")
	}

	return nil
}

// NewPgxPool creates a connection pool and checks that the database answers.
func NewPgxPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pgConf, err := pgxpool.ParseConfig(connectionString(cfg))
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	pgConf.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, pgConf)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// connectionString builds the URL of the database.
func connectionString(cfg Config) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   cfg.DB,
	}

	q := u.Query()
	q.Add("sslmode", cfg.SSLMode)
	u.RawQuery = q.Encode()

	return u.String()
}
