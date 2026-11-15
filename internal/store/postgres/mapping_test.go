package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
)

func TestRunRetryPolicy(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   *domain.RetryPolicy
	}{
		{name: "no config", config: ``},
		{name: "empty config", config: `{}`},
		{name: "no retry", config: `{"retry": null}`},
		{name: "a policy", config: `{"retry": {"max_attempts": 3, "delays": ["5s", "1m30s"]}}`,
			want: &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{5 * time.Second, 90 * time.Second}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runRetryPolicy(json.RawMessage(tt.config))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err := runRetryPolicy(json.RawMessage(`{"retry": {"delays": ["soon"]}}`))
	assert.ErrorContains(t, err, `parse delay "soon"`)
}

func TestRetryPolicyJSONRoundTrip(t *testing.T) {
	policy := domain.RetryPolicy{MaxAttempts: 5, Delays: []time.Duration{time.Second, time.Hour}}

	got, err := retryPolicyFromJSON(retryPolicyToJSON(policy))
	require.NoError(t, err)
	assert.Equal(t, policy, got)
}
