package postgres

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validConfig is a configuration that passes Validate. Each case changes one field of it.
func validConfig() Config {
	return Config{
		Host:            "db.internal",
		Port:            5432,
		User:            "runcoil",
		Password:        "secret",
		DB:              "runcoil",
		SSLMode:         "require",
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{name: "a complete configuration is valid", change: func(*Config) {}},
		{name: "an empty SSL mode is allowed", change: func(c *Config) { c.SSLMode = "" }},
		{name: "SSL mode disable is allowed", change: func(c *Config) { c.SSLMode = "disable" }},
		{name: "SSL mode verify-full is allowed", change: func(c *Config) { c.SSLMode = "verify-full" }},
		{name: "a missing host", change: func(c *Config) { c.Host = "" }, wantErr: "host is required"},
		{name: "a missing user", change: func(c *Config) { c.User = "" }, wantErr: "user is required"},
		{name: "a missing password", change: func(c *Config) { c.Password = "" }, wantErr: "password is required"},
		{name: "a missing database name", change: func(c *Config) { c.DB = "" }, wantErr: "db is required"},
		{name: "a zero Port field", change: func(c *Config) { c.Port = 0 }, wantErr: "port is required"},
		{
			name:    "an SSL mode outside the list",
			change:  func(c *Config) { c.SSLMode = "verify-ca" },
			wantErr: "ssl mode must be one of the following values: disable, require, verify-full",
		},
		{name: "max conns of zero", change: func(c *Config) { c.MaxConns = 0 }, wantErr: "max conns must be greater than 0"},
		{name: "max conns below zero", change: func(c *Config) { c.MaxConns = -1 }, wantErr: "max conns must be greater than 0"},
		{name: "min conns of zero", change: func(c *Config) { c.MinConns = 0 }, wantErr: "min conns must be greater than 0"},
		{
			name:    "max conn lifetime of zero",
			change:  func(c *Config) { c.MaxConnLifetime = 0 },
			wantErr: "max conn lifetime must be greater than 0",
		},
		{
			name:    "max conn idle time below zero",
			change:  func(c *Config) { c.MaxConnIdleTime = -time.Second },
			wantErr: "max conn idle time must be greater than 0",
		},
		{name: "a statement timeout of zero is allowed", change: func(c *Config) { c.StatementTimeout = 0 }},
		{name: "a statement timeout of 30 seconds is allowed", change: func(c *Config) { c.StatementTimeout = 30 * time.Second }},
		{
			name:    "a statement timeout below zero",
			change:  func(c *Config) { c.StatementTimeout = -time.Second },
			wantErr: "statement timeout must be greater than or equal to 0",
		},
		{
			name:    "a statement timeout shorter than a millisecond",
			change:  func(c *Config) { c.StatementTimeout = time.Microsecond * 500 },
			wantErr: "statement timeout must be 0, or at least 1ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.change(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)

				return
			}

			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

// A statement timeout reaches the server as the statement_timeout parameter, in whole milliseconds.
// Zero is sent too, so it overrides any default the server or the role has.
func TestPoolConfigSendsStatementTimeoutInMilliseconds(t *testing.T) {
	cfg := validConfig()

	cfg.StatementTimeout = 30 * time.Second
	pgConf, err := poolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "30000", pgConf.ConnConfig.RuntimeParams["statement_timeout"])

	cfg.StatementTimeout = 0
	pgConf, err = poolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "0", pgConf.ConnConfig.RuntimeParams["statement_timeout"],
		"a zero statement timeout is sent as 0, which means no timeout")
}

func TestConnectionStringSurvivesSpecialCharactersInPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		sslmode  string
	}{
		{"at, colon, slash, question mark, and hash", "p@ss:w/rd?x#y", "disable"},
		{"percent, plus, space, ampersand, and equals", "100%+sure &=", "require"},
		{"a password with verify-full", "p@ss:w/rd?x#y", "verify-full"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Host:     "db.internal",
				Port:     5432,
				User:     "runcoil_app",
				Password: tt.password,
				DB:       "runcoil",
				SSLMode:  tt.sslmode,
			}

			s := connectionString(cfg)

			u, err := url.Parse(s)
			require.NoError(t, err)
			password, ok := u.User.Password()
			require.True(t, ok, "the URL carries a password")
			assert.Equal(t, cfg.User, u.User.Username())
			assert.Equal(t, cfg.Password, password)
			assert.Equal(t, cfg.Host, u.Hostname())
			assert.Equal(t, strconv.Itoa(cfg.Port), u.Port())
			assert.Equal(t, "/"+cfg.DB, u.Path)
			assert.Equal(t, cfg.SSLMode, u.Query().Get("sslmode"))

			pool, err := pgxpool.ParseConfig(s)
			require.NoError(t, err)
			conn := pool.ConnConfig
			assert.Equal(t, cfg.User, conn.User)
			assert.Equal(t, cfg.Password, conn.Password)
			assert.Equal(t, cfg.Host, conn.Host)
			assert.Equal(t, uint16(cfg.Port), conn.Port)
			assert.Equal(t, cfg.DB, conn.Database)

			if cfg.SSLMode == "disable" {
				assert.Nil(t, conn.TLSConfig, "sslmode disable does not use TLS")

				return
			}

			require.NotNil(t, conn.TLSConfig, "any other sslmode uses TLS")
			assert.Equal(t, cfg.SSLMode != "verify-full", conn.TLSConfig.InsecureSkipVerify,
				"only verify-full checks the server certificate")
		})
	}
}
