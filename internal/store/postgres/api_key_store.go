package postgres

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.APIKeyStore = (*Store)(nil)

// ListAPIKeys returns the API keys of the project, revoked ones included, newest first.
func (s *Store) ListAPIKeys(ctx context.Context, projectID uuid.UUID) ([]domain.APIKey, error) {
	rows, err := s.queries.ApiKeyList(ctx, s.db, projectID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}

	keys := make([]domain.APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, apiKeyFromRow(row))
	}

	return keys, nil
}

// CreateAPIKey stores a new API key.
func (s *Store) CreateAPIKey(ctx context.Context, params store.CreateAPIKeyParams) (domain.APIKey, error) {
	row, err := s.queries.ApiKeyCreate(ctx, s.db, gen.ApiKeyCreateParams(params))
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("create api key: %w", err)
	}

	return apiKeyFromRow(gen.ApiKeyListRow(row)), nil
}

// RevokeAPIKey revokes the key. The sessions it opened are refused at their next call, and
// the reaper ends them. It returns domain.ErrAPIKeyNotFound when the project has no such
// key. Revoking a revoked key keeps the first time and is not an error.
func (s *Store) RevokeAPIKey(ctx context.Context, projectID, id uuid.UUID) error {
	_, err := s.queries.ApiKeyRevoke(ctx, s.db, gen.ApiKeyRevokeParams{ProjectID: projectID, ID: id})
	if isNoRowsError(err) {
		return domain.ErrAPIKeyNotFound
	}

	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}

	return nil
}

// TouchAPIKeys records when each key was last used. A time older than the stored one is
// ignored, and so is a revoked key.
func (s *Store) TouchAPIKeys(ctx context.Context, ids []uuid.UUID, usedAt []time.Time) error {
	if err := s.queries.ApiKeyTouch(ctx, s.db, gen.ApiKeyTouchParams{Ids: ids, UsedAt: usedAt}); err != nil {
		return fmt.Errorf("touch api keys: %w", err)
	}

	return nil
}
