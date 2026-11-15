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

var _ store.SessionStore = (*Store)(nil)

// CreateSession creates a session that lasts at most maxLifetime, while the user still
// has passwordHash and is not disabled. It returns domain.ErrUserNotFound otherwise.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID uuid.UUID, passwordHash string, maxLifetime time.Duration) error {
	rows, err := s.queries.SessionCreate(ctx, s.db, gen.SessionCreateParams{
		TokenHash:    tokenHash,
		UserID:       userID,
		PasswordHash: passwordHash,
		MaxLifetime:  interval(&maxLifetime),
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	if rows == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// GetSessionUserAndTouch returns the user of the live session of tokenHash and records
// the activity, at most once a minute. It returns domain.ErrSessionNotFound when the
// session has ended or its user is disabled.
func (s *Store) GetSessionUserAndTouch(ctx context.Context, tokenHash []byte, maxInactive time.Duration) (domain.User, error) {
	row, err := s.queries.SessionGetUserAndTouch(ctx, s.db, gen.SessionGetUserAndTouchParams{
		TokenHash:   tokenHash,
		MaxInactive: interval(&maxInactive),
	})
	if isNoRowsError(err) {
		return domain.User{}, domain.ErrSessionNotFound
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("get session user: %w", err)
	}

	return domain.User(row), nil
}

// DeleteSession deletes the session of tokenHash.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if err := s.queries.SessionDelete(ctx, s.db, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}
