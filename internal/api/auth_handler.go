package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/api/middleware"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type authHandler struct {
	accountService services.AccountService
	secureCookie   bool
}

func newAuthHandler(accountService services.AccountService, secureCookie bool) *authHandler {
	return &authHandler{accountService: accountService, secureCookie: secureCookie}
}

func (h *authHandler) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	res, err := h.accountService.Login(ctx, services.LoginParams{
		Login:    req.Body.Login,
		Password: req.Body.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	return gen.Login200JSONResponse{
		Body: gen.LoginResponse{
			User:                    userToResponse(res.User),
			PasswordChangeSuggested: res.PasswordChangeSuggested,
		},
		Headers: gen.Login200ResponseHeaders{
			SetCookie: h.sessionCookie(res.Token, res.ExpiresAt).String(),
		},
	}, nil
}

func (h *authHandler) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if identity, ok := middleware.IdentityFromContext(ctx); ok {
		if err := h.accountService.Logout(ctx, identity); err != nil {
			return nil, fmt.Errorf("logout: %w", err)
		}
	}

	cleared := h.sessionCookie("", time.Time{}) //nolint:gosec // Secure follows api.secure-cookie, off for a dashboard over plain HTTP.
	cleared.MaxAge = -1

	return gen.Logout204Response{
		Headers: gen.Logout204ResponseHeaders{SetCookie: cleared.String()},
	}, nil
}

func (h *authHandler) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	identity, ok := middleware.IdentityFromContext(ctx)
	if !ok {
		return nil, errors.New("no identity in the context")
	}

	user, err := h.accountService.GetUser(ctx, identity.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	access, err := h.accountService.GetUserAccess(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("get access: %w", err)
	}

	projects := make([]gen.ProjectAccess, 0, len(access.Projects))
	for _, project := range access.Projects {
		projects = append(projects, gen.ProjectAccess{
			Id:          openapi_types.UUID(project.ProjectID),
			Name:        project.ProjectName,
			Role:        gen.Role(project.Role),
			Permissions: permissionsToResponse(project.Permissions),
		})
	}

	return gen.GetMe200JSONResponse{
		User:        userToResponse(user),
		Permissions: permissionsToResponse(access.Permissions),
		Projects:    projects,
	}, nil
}

func (h *authHandler) ChangeMyPassword(ctx context.Context, req gen.ChangeMyPasswordRequestObject) (gen.ChangeMyPasswordResponseObject, error) {
	identity, ok := middleware.IdentityFromContext(ctx)
	if !ok {
		return nil, errors.New("no identity in the context")
	}

	err := h.accountService.ChangePassword(ctx, services.ChangePasswordParams{
		Identity:        identity,
		CurrentPassword: req.Body.CurrentPassword,
		NewPassword:     req.Body.NewPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("change password: %w", err)
	}

	return gen.ChangeMyPassword204Response{}, nil
}

func (h *authHandler) sessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows api.secure-cookie, off for a dashboard over plain HTTP.
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		Secure:   h.secureCookie,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func userToResponse(user domain.User) gen.User {
	return gen.User{
		Id:       openapi_types.UUID(user.ID),
		Username: user.Username,
		Email:    user.Email,
	}
}

func permissionsToResponse(permissions []domain.Permission) []gen.Permission {
	out := make([]gen.Permission, 0, len(permissions))
	for _, permission := range permissions {
		out = append(out, gen.Permission(permission))
	}

	return out
}
