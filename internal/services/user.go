package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// UserService manages the users of the server.
type UserService interface {
	ListUsers(ctx context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error)
	CreateUser(ctx context.Context, params CreateUserParams) (domain.User, error)
	UpdateUser(ctx context.Context, params UpdateUserParams) (domain.User, error)
	ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error
}

// CreateUserParams is a new user.
type CreateUserParams struct {
	Username      string
	Email         *string
	Password      string
	IsServerAdmin bool
}

// UpdateUserParams is a change to the user with ID. A nil field stays as it is.
type UpdateUserParams struct {
	ID            uuid.UUID
	Email         *string
	RemoveEmail   bool
	IsServerAdmin *bool
	Disabled      *bool
}
