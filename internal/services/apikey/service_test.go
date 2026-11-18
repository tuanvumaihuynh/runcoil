package apikey

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
)

type fakeStore struct {
	store.APIKeyStore

	err error

	created store.CreateAPIKeyParams
	revoked uuid.UUID
}

func (f *fakeStore) CreateAPIKey(_ context.Context, params store.CreateAPIKeyParams) (domain.APIKey, error) {
	f.created = params
	return domain.APIKey{ID: params.ID, Name: params.Name, KeyHint: params.KeyHint}, f.err
}

func (f *fakeStore) RevokeAPIKey(_ context.Context, _, id uuid.UUID) error {
	f.revoked = id
	return f.err
}

func newTestService(apiKeyStore store.APIKeyStore) *Service {
	return New(apiKeyStore)
}

var keyPattern = regexp.MustCompile(`^rc_api_[0-9A-Za-z]{43}$`)

func TestNewKey(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		key, err := newKey()
		require.NoError(t, err)
		assert.Regexp(t, keyPattern, key)
		assert.Len(t, key, 50)
		assert.False(t, seen[key], "a key is never made twice")

		seen[key] = true
	}
}

func TestCreateAPIKey(t *testing.T) {
	apiKeyStore := &fakeStore{}
	s := newTestService(apiKeyStore)
	projectID, userID := uuid.NewV7(), uuid.NewV7()

	res, err := s.CreateAPIKey(context.Background(), services.CreateAPIKeyParams{ProjectID: projectID, Name: "ci", CreatedBy: userID})
	require.NoError(t, err)

	assert.Regexp(t, keyPattern, res.Key)
	hash := sha256.Sum256([]byte(res.Key))
	assert.Equal(t, hash[:], apiKeyStore.created.KeyHash, "only the hash of the key is stored")
	assert.Equal(t, res.Key[len(res.Key)-4:], apiKeyStore.created.KeyHint)
	assert.Equal(t, projectID, apiKeyStore.created.ProjectID)
	assert.Equal(t, userID, apiKeyStore.created.CreatedBy)
	assert.Equal(t, "ci", apiKeyStore.created.Name)
	assert.Equal(t, apiKeyStore.created.ID, res.APIKey.ID)

	apiKeyStore.err = errors.New("connection refused")

	_, err = s.CreateAPIKey(context.Background(), services.CreateAPIKeyParams{ProjectID: projectID, Name: "ci", CreatedBy: userID})
	require.ErrorIs(t, err, apiKeyStore.err)
}

func TestRevokeAPIKey(t *testing.T) {
	apiKeyStore := &fakeStore{}
	s := newTestService(apiKeyStore)
	id := uuid.NewV7()

	require.NoError(t, s.RevokeAPIKey(context.Background(), services.RevokeAPIKeyParams{ProjectID: uuid.NewV7(), APIKeyID: id}))
	assert.Equal(t, id, apiKeyStore.revoked)

	apiKeyStore.err = fmt.Errorf("revoke api key in transaction: %w", domain.ErrAPIKeyNotFound)

	err := s.RevokeAPIKey(context.Background(), services.RevokeAPIKeyParams{ProjectID: uuid.NewV7(), APIKeyID: id})

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodeNotFound, appErr.Code)
}
