package main

import (
	"os"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultConfig reads the embedded defaults the way NewConfig does, without the flags
// that NewConfig parses from os.Args, which a test binary does not share. It does not
// read the environment.
func defaultConfig(t *testing.T) Config {
	t.Helper()
	k := koanf.New(".")
	require.NoError(t, k.Load(rawbytes.Provider(defaultConfigBytes), yaml.Parser()))

	var cfg Config
	require.NoError(t, k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "yaml"}))
	return cfg
}

// validConfig is the embedded defaults with a token, the smallest valid server configuration.
func validConfig(t *testing.T) Config {
	t.Helper()
	cfg := defaultConfig(t)
	cfg.Engine.Token = "test-token"
	return cfg
}

// loadConfig runs NewConfig, which the server uses, so the environment is read too. It
// sets os.Args to a bare program name, as pkg/cmdutil's tests do, so no flags are parsed.
func loadConfig(t *testing.T) Config {
	t.Helper()
	args := os.Args
	os.Args = []string{"runcoil"}
	t.Cleanup(func() { os.Args = args })

	cfg, err := NewConfig()
	require.NoError(t, err)
	return *cfg
}

// The embedded default has no token, so the defaults alone are refused, with the token error.
func TestDefaultConfigAloneFailsWithoutToken(t *testing.T) {
	cfg := defaultConfig(t)
	require.Empty(t, cfg.Engine.Token, "the embedded default must not carry a token")
	require.EqualError(t, cfg.Validate(), "engine: token is required: set ENGINE__TOKEN")
}

// The embedded defaults plus ENGINE__TOKEN are valid, and the server uses that token.
func TestDefaultConfigWithEnvTokenIsValid(t *testing.T) {
	t.Setenv("ENGINE__TOKEN", "test-token")

	cfg := loadConfig(t)
	assert.Equal(t, "test-token", cfg.Engine.Token)
	require.NoError(t, cfg.Validate(), "the embedded defaults with ENGINE__TOKEN must be valid")
}

// A run retention shorter than the idempotency window is refused at validation: a key's
// Run must still exist while the key is held.
func TestRunRetentionMustNotBeShorterThanTheIdempotencyWindow(t *testing.T) {
	cfg := validConfig(t)
	cfg.Engine.Idempotency.Window = 24 * time.Hour

	cfg.Worker.Cleanup.RunRetention = 24 * time.Hour
	require.NoError(t, cfg.Validate(), "a retention equal to the window is allowed")

	cfg.Worker.Cleanup.RunRetention = time.Hour
	err := cfg.Validate()
	require.EqualError(t, err, "worker cleanup run retention must not be shorter than the engine idempotency window")
}

func TestWorkerCleanupMustBeConfigured(t *testing.T) {
	cfg := validConfig(t)
	cfg.Worker.Cleanup.Interval = 0
	err := cfg.Validate()
	require.ErrorContains(t, err, "worker: cleanup interval must be greater than 0")
}

// The embedded default stop timeout is 10 seconds, and a stop timeout of 0 is refused.
func TestShutdownTimeoutDefaultsToTenSecondsAndMustBePositive(t *testing.T) {
	cfg := validConfig(t)
	require.Equal(t, 10*time.Second, cfg.Engine.ShutdownTimeout, "the embedded default")
	require.NoError(t, cfg.Validate(), "the default must be valid")

	cfg.Engine.ShutdownTimeout = 0
	require.EqualError(t, cfg.Validate(), "engine: shutdown timeout must be greater than 0")
}
