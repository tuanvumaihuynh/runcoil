package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// APIKeyService manages the API keys of the projects.
type APIKeyService interface {
	ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]domain.APIKey, error)
	CreateAPIKey(ctx context.Context, params CreateAPIKeyParams) (CreateAPIKeyResult, error)
	RevokeAPIKey(ctx context.Context, params RevokeAPIKeyParams) error
}

// CreateAPIKeyParams is a new API key of a project, made by a user.
type CreateAPIKeyParams struct {
	ProjectID uuid.UUID
	Name      string
	CreatedBy uuid.UUID
}

// CreateAPIKeyResult is the new API key and the key itself, which is never shown again.
type CreateAPIKeyResult struct {
	APIKey domain.APIKey
	Key    string
}

// RevokeAPIKeyParams is an API key of a project to revoke.
type RevokeAPIKeyParams struct {
	ProjectID uuid.UUID
	APIKeyID  uuid.UUID
}
