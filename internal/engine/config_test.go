package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadCapIsTheLargestSinglePayloadEncodedPlusOverhead(t *testing.T) {
	cfg := testConfig()
	cfg.Limits = LimitsConfig{InputBytes: 1 << 20, StepOutputBytes: 1 << 20, RunOutputBytes: 1 << 20, RunTotalBytes: 16 << 20}
	got := cfg.ReadMaxBytes()
	require.Less(t, got, 3_000_000, "a 3 MB body must be refused")
	require.GreaterOrEqual(t, got, (1<<20)*4/3, "a 1 MiB payload must fit once base64-encoded")
}

// An empty token is refused. The same configuration with a token is valid, so the token is the only failure.
func TestTokenIsRequired(t *testing.T) {
	cfg := testConfig()
	cfg.Limits = LimitsConfig{InputBytes: 8, StepOutputBytes: 8, RunOutputBytes: 8, RunTotalBytes: 16}
	require.NoError(t, cfg.Validate(), "the configuration must be valid with a token")

	cfg.Token = ""
	require.EqualError(t, cfg.Validate(), "token is required: set ENGINE__TOKEN")
}

func TestWaiterCapMustBeAtLeastOne(t *testing.T) {
	cfg := testConfig()
	cfg.Limits = LimitsConfig{InputBytes: 8, StepOutputBytes: 8, RunOutputBytes: 8, RunTotalBytes: 16}
	cfg.WaitRun.MaxWaiters = 0
	err := cfg.Validate()
	require.Error(t, err, "zero waiters must be refused")
}

// The port is a TCP port, so only 1 to 65535 is valid.
func TestPortMustBeFromOneToSixtyFiveThousandFiveHundredThirtyFive(t *testing.T) {
	cfg := testConfig()
	cfg.Limits = LimitsConfig{InputBytes: 8, StepOutputBytes: 8, RunOutputBytes: 8, RunTotalBytes: 16}

	for _, port := range []int{1, 7070, 65535} {
		cfg.Port = port
		require.NoError(t, cfg.Validate(), "port %d must be valid", port)
	}

	for _, port := range []int{0, -1, 65536} {
		cfg.Port = port
		require.EqualError(t, cfg.Validate(), "port must be from 1 to 65535", "port %d must be refused", port)
	}
}
