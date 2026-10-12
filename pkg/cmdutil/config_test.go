package cmdutil

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// format is a text-unmarshaled key, as log.Format is in the server configuration.
type format uint8

func (f *format) UnmarshalText(text []byte) error {
	switch string(text) {
	case "text":
		*f = 1
	case "json":
		*f = 2
	default:
		return fmt.Errorf("unknown format: %s", text)
	}

	return nil
}

// testConfig has one key of each kind the loader must fill: a list of durations, a
// string, and a text-unmarshaled value.
type testConfig struct {
	Log struct {
		Format format `yaml:"format"`
	} `yaml:"log"`
	Postgres struct {
		Password string `yaml:"password"`
	} `yaml:"postgres"`
	Engine struct {
		Retry struct {
			Delays []time.Duration `yaml:"delays"`
		} `yaml:"retry"`
	} `yaml:"engine"`
}

const testDefaults = `
log:
  format: text
postgres:
  password: postgres
engine:
  retry:
    delays: [5s, 10s, 30s, 1m]
`

// loadTestConfig runs LoadConfig on the test defaults.
func loadTestConfig(t *testing.T) testConfig {
	t.Helper()
	var cfg testConfig
	require.NoError(t, loadWithTestDefaults(t, &cfg))
	return cfg
}

// loadWithTestDefaults runs LoadConfig on the test defaults into out. LoadConfig parses
// the flags in os.Args, which a test binary does not share, so os.Args is set to a bare
// program name for the call.
func loadWithTestDefaults(t *testing.T, out any) error {
	t.Helper()
	args := os.Args
	os.Args = []string{"test"}
	t.Cleanup(func() { os.Args = args })

	return LoadConfig("test", []byte(testDefaults), out)
}

// A comma-separated environment value fills a list-typed key: the delays of the
// documented example in .env.example.
func TestEnvironmentCommaListFillsListKey(t *testing.T) {
	t.Setenv("ENGINE__RETRY__DELAYS", "5s,10s,30s,1m,3m,10m,30m,1h")

	cfg := loadTestConfig(t)
	assert.Equal(t, []time.Duration{
		5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute,
		3 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour,
	}, cfg.Engine.Retry.Delays)
}

// A single value in the environment is a list of one element.
func TestEnvironmentSingleValueFillsListKey(t *testing.T) {
	t.Setenv("ENGINE__RETRY__DELAYS", "5s")

	cfg := loadTestConfig(t)
	assert.Equal(t, []time.Duration{5 * time.Second}, cfg.Engine.Retry.Delays)
}

// A string key keeps a comma inside its value: a password with a comma is one string.
func TestEnvironmentCommaInStringKeyStaysOneValue(t *testing.T) {
	t.Setenv("POSTGRES__PASSWORD", "pa,ss,word")

	cfg := loadTestConfig(t)
	assert.Equal(t, "pa,ss,word", cfg.Postgres.Password)
}

// A YAML list still loads as a list of durations when the environment does not set it.
func TestYAMLListStillLoads(t *testing.T) {
	cfg := loadTestConfig(t)
	assert.Equal(t, []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute},
		cfg.Engine.Retry.Delays)
}

// A text-unmarshaled key still loads from the environment.
func TestEnvironmentTextKeyStillLoads(t *testing.T) {
	t.Setenv("LOG__FORMAT", "json")

	cfg := loadTestConfig(t)
	assert.Equal(t, format(2), cfg.Log.Format)
}

// An element of the environment list that is not a duration is refused at load, so a typo
// in one element does not reach the server as a wrong delay.
func TestEnvironmentInvalidDurationInListIsRefused(t *testing.T) {
	t.Setenv("ENGINE__RETRY__DELAYS", "5s,soon")

	var cfg testConfig
	err := loadWithTestDefaults(t, &cfg)
	require.Error(t, err, "an element that is not a duration must be refused")
	require.ErrorContains(t, err, "engine.retry.delays[1]", "the error must name the element")
}
