package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
)

const (
	keyPrefix = "rc_api_"
	keyBytes  = 32
	// keyDigits is how many base62 digits 256 bits need: 62^43 > 2^256.
	keyDigits = 43
	hintChars = 4
)

// The errors the service returns to clients.
var (
	ErrAPIKeyNotFound = apperr.NotFound(apperr.CodeNotFound, "api key not found")
)

var _ services.APIKeyService = (*Service)(nil)

// Service manages the API keys of the projects.
type Service struct {
	apiKeyStore store.APIKeyStore
}

// New creates the API key service.
func New(apiKeyStore store.APIKeyStore) *Service {
	return &Service{apiKeyStore: apiKeyStore}
}

// ListAPIKeys returns the API keys of the project, revoked ones included, newest first.
func (s *Service) ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]domain.APIKey, error) {
	keys, err := s.apiKeyStore.ListAPIKeys(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}

	return keys, nil
}

// CreateAPIKey makes a new API key for the project. Only its hash is stored, so the
// result is the one time the key is known.
func (s *Service) CreateAPIKey(ctx context.Context, params services.CreateAPIKeyParams) (services.CreateAPIKeyResult, error) {
	key, err := newKey()
	if err != nil {
		return services.CreateAPIKeyResult{}, fmt.Errorf("new key: %w", err)
	}

	hash := sha256.Sum256([]byte(key))
	apiKey, err := s.apiKeyStore.CreateAPIKey(ctx, store.CreateAPIKeyParams{
		ID:        uuid.NewV7(),
		ProjectID: params.ProjectID,
		Name:      params.Name,
		KeyHash:   hash[:],
		KeyHint:   key[len(key)-hintChars:],
		CreatedBy: params.CreatedBy,
	})
	if err != nil {
		return services.CreateAPIKeyResult{}, fmt.Errorf("create api key: %w", err)
	}

	return services.CreateAPIKeyResult{APIKey: apiKey, Key: key}, nil
}

// RevokeAPIKey revokes the key. The worker sessions it opened are refused at their next
// call. Revoking a revoked key does nothing.
func (s *Service) RevokeAPIKey(ctx context.Context, params services.RevokeAPIKeyParams) error {
	err := s.apiKeyStore.RevokeAPIKey(ctx, params.ProjectID, params.APIKeyID)
	if err != nil {
		if errors.Is(err, domain.ErrAPIKeyNotFound) {
			return ErrAPIKeyNotFound
		}

		return fmt.Errorf("revoke api key: %w", err)
	}

	return nil
}

// newKey makes a key: the prefix and 256 random bits in base62, padded to a fixed length.
// Base62 has no - or _, so a double click selects the whole key.
func newKey() (string, error) {
	raw := make([]byte, keyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	digits := new(big.Int).SetBytes(raw).Text(62)

	return keyPrefix + strings.Repeat("0", keyDigits-len(digits)) + digits, nil
}
