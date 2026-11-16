package postgres

import (
	"context"
	"fmt"
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

// RevokeAPIKey revokes the key, ends the worker sessions it opened and puts the attempts
// they hold back in the queue, in one transaction. A revoke is not the workers' fault, so
// the attempts do not count as lost. It returns how many sessions it ended, and
// domain.ErrAPIKeyNotFound when the project has no such key. Revoking a revoked key ends
// nothing and is not an error.
func (s *Store) RevokeAPIKey(ctx context.Context, projectID, id uuid.UUID) (int, error) {
	var ended int
	err := s.db.WithTx(ctx, func(tx DB) error {
		_, err := s.queries.ApiKeyRevoke(ctx, tx, gen.ApiKeyRevokeParams{ProjectID: projectID, ID: id})
		if isNoRowsError(err) {
			return domain.ErrAPIKeyNotFound
		}

		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}

		workerIDs, err := s.queries.WorkerEndByApiKey(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("end sessions: %w", err)
		}

		if len(workerIDs) == 0 {
			return nil
		}

		if _, err := s.queries.LeaseReleaseOfWorkers(ctx, tx, workerIDs); err != nil {
			return fmt.Errorf("release leases: %w", err)
		}

		ended = len(workerIDs)

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("revoke api key in transaction: %w", err)
	}

	return ended, nil
}
