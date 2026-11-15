package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/pkg/configutil"
)

func validConfig() Config {
	return Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		DB:       "postgres",
		SSLMode:  "disable",
		MaxConns: 10,
	}
}

func TestConfigDefaults(t *testing.T) {
	var cfg Config
	require.NoError(t, configutil.Load("test", &cfg, nil, func(string) (string, bool) { return "", false }))

	assert.Equal(t, validConfig(), cfg)
	assert.NoError(t, cfg.Validate())
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		want   string
	}{
		{name: "host", modify: func(c *Config) { c.Host = "" }, want: "host is required"},
		{name: "port", modify: func(c *Config) { c.Port = 70000 }, want: "port must be between 1 and 65535"},
		{name: "user", modify: func(c *Config) { c.User = "" }, want: "user is required"},
		{name: "password", modify: func(c *Config) { c.Password = "" }, want: "password is required"},
		{name: "db", modify: func(c *Config) { c.DB = "" }, want: "db is required"},
		{name: "ssl mode", modify: func(c *Config) { c.SSLMode = "prefer" }, want: "ssl mode must be one of the following values: disable, require, verify-full"},
		{name: "max conns", modify: func(c *Config) { c.MaxConns = 0 }, want: "max conns must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(&cfg)

			assert.EqualError(t, cfg.Validate(), tt.want)
		})
	}
}

func TestConnectionStringEscapes(t *testing.T) {
	cfg := validConfig()
	cfg.User = "it's a user"
	cfg.Password = "p@ss:w/rd?#%"
	cfg.DB = "my db"

	parsed, err := pgconn.ParseConfig(connectionString(cfg))

	require.NoError(t, err)
	assert.Equal(t, "localhost", parsed.Host)
	assert.Equal(t, uint16(5432), parsed.Port)
	assert.Equal(t, "it's a user", parsed.User)
	assert.Equal(t, "p@ss:w/rd?#%", parsed.Password)
	assert.Equal(t, "my db", parsed.Database)
	assert.Nil(t, parsed.TLSConfig)
}

func TestPasswordNeverInErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 1
	cfg.Password = `zebra\'quartz violet-7731`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := NewPgxPool(ctx, cfg)
	require.ErrorContains(t, err, "ping database")

	for _, part := range []string{"zebra", "quartz", "violet", "7731"} {
		assert.NotContains(t, err.Error(), part)
	}
}
