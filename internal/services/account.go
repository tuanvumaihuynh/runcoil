package services

import (
	"context"
	"time"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// AccountService signs users in and out, manages their own passwords and decides what
// they may do.
type AccountService interface {
	Login(ctx context.Context, params LoginParams) (LoginResult, error)
	Authenticate(ctx context.Context, credential domain.Credential) (domain.Identity, error)
	Logout(ctx context.Context, identity domain.Identity) error
	ChangePassword(ctx context.Context, params ChangePasswordParams) error
	GetUser(ctx context.Context, id uuid.UUID) (domain.User, error)
	GetUserAccess(ctx context.Context, user domain.User) (Access, error)
	Authorize(ctx context.Context, params AuthorizeParams) error
}

// LoginParams is a username or an email and its password.
type LoginParams struct {
	Login    string
	Password string
}

// LoginResult is a new session and its user.
type LoginResult struct {
	Token                   string
	ExpiresAt               time.Time
	User                    domain.User
	PasswordChangeSuggested bool
}

// ChangePasswordParams is a password change by the signed-in user.
type ChangePasswordParams struct {
	Identity        domain.Identity
	CurrentPassword string
	NewPassword     string
}

// AuthorizeParams is a permission to check in a project.
type AuthorizeParams struct {
	Identity   domain.Identity
	ProjectID  uuid.UUID
	Permission domain.Permission
}

// Access is what a user may do: the server-wide permissions and the projects the user can
// open.
type Access struct {
	Permissions []domain.Permission
	Projects    []ProjectAccess
}

// ProjectAccess is a project the user can open, with the role and its permissions.
type ProjectAccess struct {
	domain.ProjectRole
	Permissions []domain.Permission
}
