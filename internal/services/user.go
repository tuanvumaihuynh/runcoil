package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/pkg/paging"
)

// UserService manages the users of the server.
type UserService interface {
	ListUsers(ctx context.Context, params paging.Params) (paging.List[domain.User], error)
	LookupUsers(ctx context.Context, params paging.Params) (paging.List[domain.User], error)
	CreateUser(ctx context.Context, params CreateUserParams) (domain.User, error)
	UpdateUser(ctx context.Context, params UpdateUserParams) (domain.User, error)
	ResetPassword(ctx context.Context, params ResetPasswordParams) error
}

// CreateUserParams is a new user.
type CreateUserParams struct {
	Username      string
	Email         *string
	Password      string
	IsServerAdmin bool
}

// UpdateUserParams replaces the email, the server admin role and the disabled state of the
// user with ID. A nil Email removes the email.
type UpdateUserParams struct {
	ID            uuid.UUID
	Email         *string
	IsServerAdmin bool
	Disabled      bool
}

// ResetPasswordParams is a new password for the user with ID, set by a server admin.
type ResetPasswordParams struct {
	ID          uuid.UUID
	NewPassword string
}
