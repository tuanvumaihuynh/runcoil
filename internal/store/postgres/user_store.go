package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.User = (*Store)(nil)

// CreateFirstServerAdmin creates admin and the project default in one transaction, only
// while no user exists. It reports whether it created them.
func (s *Store) CreateFirstServerAdmin(ctx context.Context, admin domain.User, defaultProjectID uuid.UUID) (bool, error) {
	var created bool
	err := s.db.WithTx(ctx, func(tx DB) error {
		rows, err := s.queries.UserCreateFirstServerAdmin(ctx, tx, gen.UserCreateFirstServerAdminParams{
			ID:           admin.ID,
			Username:     admin.Username,
			PasswordHash: admin.PasswordHash,
		})
		if err != nil {
			return fmt.Errorf("create first server admin: %w", err)
		}

		if rows == 0 {
			return nil
		}

		if err := s.queries.ProjectCreateDefault(ctx, tx, defaultProjectID); err != nil {
			return fmt.Errorf("create default project: %w", err)
		}

		created = true

		return nil
	})

	return created, err
}

// GetUserByID returns the user with id, or domain.ErrUserNotFound.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := s.queries.UserGetByID(ctx, s.db, id)
	if isNoRowsError(err) {
		return domain.User{}, domain.ErrUserNotFound
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("get user by id: %w", err)
	}

	return domain.User(row), nil
}

// GetUserByUsername returns the user with username, or domain.ErrUserNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	row, err := s.queries.UserGetByUsername(ctx, s.db, username)
	if isNoRowsError(err) {
		return domain.User{}, domain.ErrUserNotFound
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("get user by username: %w", err)
	}

	return domain.User(row), nil
}

// GetUserByEmail returns the user with email, or domain.ErrUserNotFound.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := s.queries.UserGetByEmail(ctx, s.db, email)
	if isNoRowsError(err) {
		return domain.User{}, domain.ErrUserNotFound
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("get user by email: %w", err)
	}

	return domain.User(row), nil
}

// UpdatePasswordAndDeleteOtherSessions replaces the password hash currentHash of userID
// with newHash and deletes the user's sessions other than the one of keepTokenHash, in one
// transaction. It changes nothing and returns domain.ErrSessionNotFound when that session
// has ended, domain.ErrPasswordChanged when the hash is no longer currentHash, and
// domain.ErrUserNotFound when the user is gone.
func (s *Store) UpdatePasswordAndDeleteOtherSessions(ctx context.Context, userID uuid.UUID, currentHash, newHash string, keepTokenHash []byte) error {
	return s.db.WithTx(ctx, func(tx DB) error {
		// The user row is locked first, so two password changes of one user run one after
		// the other: the second sees that the first ended its session or changed the hash.
		_, err := s.queries.UserLockForUpdate(ctx, tx, userID)
		if isNoRowsError(err) {
			return domain.ErrUserNotFound
		}

		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		live, err := s.queries.SessionExistsForUser(ctx, tx, gen.SessionExistsForUserParams{
			TokenHash: keepTokenHash,
			UserID:    userID,
		})
		if err != nil {
			return fmt.Errorf("check session: %w", err)
		}

		if !live {
			return domain.ErrSessionNotFound
		}

		rows, err := s.queries.UserUpdatePassword(ctx, tx, gen.UserUpdatePasswordParams{
			ID:                  userID,
			NewPasswordHash:     newHash,
			CurrentPasswordHash: currentHash,
		})
		if err != nil {
			return fmt.Errorf("update password: %w", err)
		}

		if rows == 0 {
			return domain.ErrPasswordChanged
		}

		err = s.queries.SessionDeleteOthers(ctx, tx, gen.SessionDeleteOthersParams{
			UserID:    userID,
			TokenHash: keepTokenHash,
		})
		if err != nil {
			return fmt.Errorf("delete other sessions: %w", err)
		}

		return nil
	})
}
