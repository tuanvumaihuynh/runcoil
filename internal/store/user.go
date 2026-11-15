package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// User keeps the dashboard users.
type User interface {
	CreateFirstServerAdmin(ctx context.Context, admin domain.User, defaultProjectID uuid.UUID) (bool, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	GetUserByUsername(ctx context.Context, username string) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	UpdatePasswordAndDeleteOtherSessions(ctx context.Context, userID uuid.UUID, currentHash, newHash string, keepTokenHash []byte) error
}
