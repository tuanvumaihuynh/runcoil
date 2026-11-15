package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

const defaultPageSize = 50

var errInvalidPageToken = apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")

type userHandler struct {
	userService services.UserService
}

func newUserHandler(userService services.UserService) *userHandler {
	return &userHandler{userService: userService}
}

func (h *userHandler) ListUsers(ctx context.Context, req gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	pageSize := int32(defaultPageSize)
	if req.Params.PageSize != nil {
		pageSize = *req.Params.PageSize
	}

	afterID, err := pageTokenToID(req.Params.PageToken)
	if err != nil {
		return nil, err
	}

	// One more than the page holds tells whether another page follows.
	res, err := h.userService.ListUsers(ctx, afterID, pageSize+1)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	var nextPageToken *string
	if len(res) > int(pageSize) {
		res = res[:pageSize]
		nextPageToken = idToPageToken(res[len(res)-1].ID)
	}

	users := make([]gen.ManagedUser, 0, len(res))
	for _, user := range res {
		users = append(users, managedUserToResponse(user))
	}

	return gen.ListUsers200JSONResponse{Users: users, NextPageToken: nextPageToken}, nil
}

func (h *userHandler) CreateUser(ctx context.Context, req gen.CreateUserRequestObject) (gen.CreateUserResponseObject, error) {
	res, err := h.userService.CreateUser(ctx, services.CreateUserParams{
		Username:      req.Body.Username,
		Email:         req.Body.Email,
		Password:      req.Body.Password,
		IsServerAdmin: req.Body.IsServerAdmin,
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return gen.CreateUser201JSONResponse(managedUserToResponse(res)), nil
}

func (h *userHandler) UpdateUser(ctx context.Context, req gen.UpdateUserRequestObject) (gen.UpdateUserResponseObject, error) {
	res, err := h.userService.UpdateUser(ctx, updateUserParamsFromRequest(uuid.UUID(req.UserId), *req.Body))
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}

	return gen.UpdateUser200JSONResponse(managedUserToResponse(res)), nil
}

func (h *userHandler) ResetUserPassword(ctx context.Context, req gen.ResetUserPasswordRequestObject) (gen.ResetUserPasswordResponseObject, error) {
	if err := h.userService.ResetPassword(ctx, uuid.UUID(req.UserId), req.Body.NewPassword); err != nil {
		return nil, fmt.Errorf("reset password: %w", err)
	}

	return gen.ResetUserPassword204Response{}, nil
}

func managedUserToResponse(user domain.User) gen.ManagedUser {
	return gen.ManagedUser{
		Id:            openapi_types.UUID(user.ID),
		Username:      user.Username,
		Email:         user.Email,
		IsServerAdmin: user.IsServerAdmin,
		DisabledAt:    user.DisabledAt,
		CreatedAt:     user.CreatedAt,
	}
}

func updateUserParamsFromRequest(id uuid.UUID, body gen.UpdateUserRequest) services.UpdateUserParams {
	return services.UpdateUserParams{
		ID:            id,
		Email:         body.Email,
		RemoveEmail:   body.RemoveEmail != nil && *body.RemoveEmail,
		IsServerAdmin: body.IsServerAdmin,
		Disabled:      body.Disabled,
	}
}

func idToPageToken(id uuid.UUID) *string {
	token := base64.RawURLEncoding.EncodeToString(id[:])
	return &token
}

func pageTokenToID(token *string) (*uuid.UUID, error) {
	if token == nil {
		return nil, nil
	}

	b, err := base64.RawURLEncoding.DecodeString(*token)
	if err != nil || len(b) != len(uuid.UUID{}) {
		return nil, errInvalidPageToken
	}

	id := uuid.UUID(b)

	return &id, nil
}
