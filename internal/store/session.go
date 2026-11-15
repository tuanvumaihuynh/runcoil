package store

import (
	"context"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// Session keeps the dashboard sessions.
type Session interface {
	CreateSession(ctx context.Context, tokenHash []byte, userID uuid.UUID, passwordHash string, maxLifetime time.Duration) error
	GetSessionUserAndTouch(ctx context.Context, tokenHash []byte, maxInactive time.Duration) (domain.User, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
}
