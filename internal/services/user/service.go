package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/services/account"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

// The errors the service returns to clients.
var (
	ErrUserNotFound     = apperr.NotFound(apperr.CodeNotFound, "user not found")
	ErrUsernameTaken    = apperr.Conflict(apperr.CodeAlreadyExists, "username is taken")
	ErrEmailTaken       = apperr.Conflict(apperr.CodeAlreadyExists, "email is taken")
	ErrLastServerAdmin  = apperr.Conflict(apperr.CodeLastServerAdmin, "the last active server admin cannot lose the role or be disabled")
	ErrInvalidPageToken = apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")
)

var _ services.UserService = (*Service)(nil)

// Service manages the users of the server.
type Service struct {
	userStore store.UserStore
}

// New creates the user service.
func New(userStore store.UserStore) *Service {
	return &Service{userStore: userStore}
}

// CreateUser creates a user who can sign in. The username and the email are lowercased.
func (s *Service) CreateUser(ctx context.Context, params services.CreateUserParams) (domain.User, error) {
	username := strings.ToLower(params.Username)
	if err := account.ValidateUsername(username); err != nil {
		return domain.User{}, invalidField("username", err)
	}

	email, err := normalizeEmail(params.Email)
	if err != nil {
		return domain.User{}, err
	}

	if err := account.ValidatePassword(params.Password); err != nil {
		return domain.User{}, invalidField("password", err)
	}

	hash, err := account.HashPassword(ctx, params.Password)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.userStore.CreateUser(ctx, domain.User{
		ID:            uuid.NewV7(),
		Username:      username,
		Email:         email,
		PasswordHash:  hash,
		IsServerAdmin: params.IsServerAdmin,
	})
	if err != nil {
		return domain.User{}, storeError("create user", err)
	}

	return user, nil
}

// ListUsers returns one page of the users, by ID.
func (s *Service) ListUsers(ctx context.Context, params paging.Params) (paging.List[domain.User], error) {
	afterID, err := params.AfterID()
	if err != nil {
		return paging.List[domain.User]{}, ErrInvalidPageToken
	}

	users, err := s.userStore.ListUsers(ctx, afterID, params.Limit())
	if err != nil {
		return paging.List[domain.User]{}, fmt.Errorf("list users: %w", err)
	}

	return paging.NewList(params, users, func(user domain.User) uuid.UUID { return user.ID }), nil
}

// LookupUsers returns one page of the users who are not disabled, by ID, for a picker.
func (s *Service) LookupUsers(ctx context.Context, params paging.Params) (paging.List[domain.User], error) {
	afterID, err := params.AfterID()
	if err != nil {
		return paging.List[domain.User]{}, ErrInvalidPageToken
	}

	users, err := s.userStore.ListActiveUsers(ctx, afterID, params.Limit())
	if err != nil {
		return paging.List[domain.User]{}, fmt.Errorf("list active users: %w", err)
	}

	return paging.NewList(params, users, func(user domain.User) uuid.UUID { return user.ID }), nil
}

// UpdateUser replaces the email, the server admin role and the disabled state of a user.
// Disabling the user ends their sessions.
func (s *Service) UpdateUser(ctx context.Context, params services.UpdateUserParams) (domain.User, error) {
	email, err := normalizeEmail(params.Email)
	if err != nil {
		return domain.User{}, err
	}

	user, err := s.userStore.UpdateUser(ctx, store.UpdateUserParams{
		ID:            params.ID,
		Email:         email,
		IsServerAdmin: params.IsServerAdmin,
		Disabled:      params.Disabled,
	})
	if err != nil {
		return domain.User{}, storeError("update user", err)
	}

	return user, nil
}

// ResetPassword sets a new password for the user and ends all their sessions.
func (s *Service) ResetPassword(ctx context.Context, params services.ResetPasswordParams) error {
	if err := account.ValidatePassword(params.NewPassword); err != nil {
		return invalidField("newPassword", err)
	}

	hash, err := account.HashPassword(ctx, params.NewPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.userStore.SetPasswordAndDeleteSessions(ctx, params.ID, hash); err != nil {
		return storeError("set password", err)
	}

	return nil
}

func normalizeEmail(email *string) (*string, error) {
	if email == nil {
		return nil, nil
	}

	lower := strings.ToLower(*email)
	if err := account.ValidateEmail(lower); err != nil {
		return nil, invalidField("email", err)
	}

	return &lower, nil
}

func invalidField(field string, err error) error {
	return apperr.Invalid(apperr.CodeInvalidRequest, field+" "+err.Error()).WithDetail("field", field)
}

func storeError(op string, err error) error {
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		return ErrUserNotFound
	case errors.Is(err, domain.ErrUsernameTaken):
		return ErrUsernameTaken
	case errors.Is(err, domain.ErrEmailTaken):
		return ErrEmailTaken
	case errors.Is(err, domain.ErrLastServerAdmin):
		return ErrLastServerAdmin
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
