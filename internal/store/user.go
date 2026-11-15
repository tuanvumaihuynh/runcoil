package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// UserStore keeps the dashboard users.
type UserStore interface {
	CreateFirstServerAdmin(ctx context.Context, admin domain.User, defaultProjectID uuid.UUID) (bool, error)
	CreateUser(ctx context.Context, user domain.User) (domain.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	GetUserByUsername(ctx context.Context, username string) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	ListUsers(ctx context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error)
	UpdateUser(ctx context.Context, params domain.UpdateUserParams) (domain.User, error)
	UpdatePasswordAndDeleteOtherSessions(ctx context.Context, userID uuid.UUID, currentHash, newHash string, keepTokenHash []byte) error
	SetPasswordAndDeleteSessions(ctx context.Context, id uuid.UUID, passwordHash string) error
}
