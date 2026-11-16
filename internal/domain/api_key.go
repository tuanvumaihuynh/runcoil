package domain

import (
	"time"
	"uuid"
)

// APIKey is a project's key for workers and app code. The key itself is shown once, when
// it is made; afterwards only its last characters are known.
type APIKey struct {
	ID   uuid.UUID
	Name string
	// KeyHint is the key's last 4 characters, nil for a key made before hints.
	KeyHint *string
	// CreatedBy is the username of the user who made the key, nil when it is not known.
	CreatedBy  *string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}
