package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.UserStore = (*Store)(nil)

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

// CreateUser creates user. It returns domain.ErrUsernameTaken or domain.ErrEmailTaken
// when another user has the username or the email.
func (s *Store) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	row, err := s.queries.UserCreate(ctx, s.db, gen.UserCreateParams{
		ID:            user.ID,
		Username:      user.Username,
		Email:         user.Email,
		PasswordHash:  user.PasswordHash,
		IsServerAdmin: user.IsServerAdmin,
	})
	if err != nil {
		return domain.User{}, userWriteError("create user", err)
	}

	return domain.User(row), nil
}

// ListUsers returns at most limit users by ID, after afterID when it is set.
func (s *Store) ListUsers(ctx context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error) {
	rows, err := s.queries.UserList(ctx, s.db, gen.UserListParams{
		AfterID:   afterID,
		PageLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, domain.User(row))
	}

	return users, nil
}

// ListActiveUsers returns at most limit users who are not disabled, by ID, after afterID
// when it is set.
func (s *Store) ListActiveUsers(ctx context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error) {
	rows, err := s.queries.UserListActive(ctx, s.db, gen.UserListActiveParams{
		AfterID:   afterID,
		PageLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list active users: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, domain.User(row))
	}

	return users, nil
}

// UpdateUser replaces the email, the server admin role and the disabled state of a user,
// and deletes the user's sessions when it disables the user, in one transaction. It returns
// domain.ErrUserNotFound, domain.ErrEmailTaken, or domain.ErrLastServerAdmin when the change
// would leave no active server admin.
func (s *Store) UpdateUser(ctx context.Context, params store.UpdateUserParams) (domain.User, error) {
	var user domain.User
	err := s.db.WithTx(ctx, func(tx DB) error {
		if !params.IsServerAdmin || params.Disabled {
			// Every change that may remove a server admin takes one lock, so two of them run
			// one after the other. Locking the admin rows is not enough: a row lock covers only
			// the rows in the statement's snapshot, so an admin promoted after it is missed.
			// The list is read after the lock, by a new statement that sees every commit.
			if err := s.queries.UserLockServerAdminChanges(ctx, tx); err != nil {
				return fmt.Errorf("lock server admin changes: %w", err)
			}

			admins, err := s.queries.UserListActiveServerAdmins(ctx, tx)
			if err != nil {
				return fmt.Errorf("list server admins: %w", err)
			}

			if len(admins) == 1 && admins[0] == params.ID {
				return domain.ErrLastServerAdmin
			}
		}

		row, err := s.queries.UserUpdate(ctx, tx, gen.UserUpdateParams{
			ID:            params.ID,
			Email:         params.Email,
			IsServerAdmin: params.IsServerAdmin,
			Disabled:      params.Disabled,
		})
		if isNoRowsError(err) {
			return domain.ErrUserNotFound
		}

		if err != nil {
			return userWriteError("update user", err)
		}

		user = domain.User(row)

		if user.DisabledAt != nil {
			if err := s.queries.SessionDeleteByUser(ctx, tx, params.ID); err != nil {
				return fmt.Errorf("delete sessions: %w", err)
			}
		}

		return nil
	})

	return user, err
}

// SetPasswordAndDeleteSessions sets the password hash of the user with id and deletes all
// the user's sessions, in one transaction. It returns domain.ErrUserNotFound.
func (s *Store) SetPasswordAndDeleteSessions(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return s.db.WithTx(ctx, func(tx DB) error {
		rows, err := s.queries.UserSetPassword(ctx, tx, gen.UserSetPasswordParams{
			ID:           id,
			PasswordHash: passwordHash,
		})
		if err != nil {
			return fmt.Errorf("set password: %w", err)
		}

		if rows == 0 {
			return domain.ErrUserNotFound
		}

		if err := s.queries.SessionDeleteByUser(ctx, tx, id); err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}

		return nil
	})
}

func userWriteError(op string, err error) error {
	switch constraint, _ := uniqueViolation(err); constraint {
	case "users_username_key":
		return domain.ErrUsernameTaken
	case "users_email_key":
		return domain.ErrEmailTaken
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
