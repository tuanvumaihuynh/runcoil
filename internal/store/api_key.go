package store

import (
	"context"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// APIKeyStore keeps the API keys of the projects.
type APIKeyStore interface {
	ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]domain.APIKey, error)
	CreateAPIKey(ctx context.Context, params CreateAPIKeyParams) (domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, projectID, id uuid.UUID) error
	TouchAPIKeys(ctx context.Context, ids []uuid.UUID, usedAt []time.Time) error
}

// CreateAPIKeyParams is a new API key. Only the hash of the key is stored.
type CreateAPIKeyParams struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	KeyHash   []byte
	KeyHint   string
	CreatedBy uuid.UUID
}
