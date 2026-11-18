package api

import (
	"context"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/pkg/paging"
)

type userHandler struct {
	userService services.UserService
}

func newUserHandler(userService services.UserService) *userHandler {
	return &userHandler{userService: userService}
}

func (h *userHandler) ListUsers(ctx context.Context, req gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	res, err := h.userService.ListUsers(ctx, pagingParamsFromRequest(req.Params.PageSize, req.Params.PageToken))
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	users := make([]gen.ManagedUser, 0, len(res.Items))
	for _, user := range res.Items {
		users = append(users, managedUserToResponse(user))
	}

	var nextPageToken *string
	if res.NextPageToken != "" {
		nextPageToken = &res.NextPageToken
	}

	return gen.ListUsers200JSONResponse{Users: users, NextPageToken: nextPageToken}, nil
}

func (h *userHandler) LookupUsers(ctx context.Context, req gen.LookupUsersRequestObject) (gen.LookupUsersResponseObject, error) {
	res, err := h.userService.LookupUsers(ctx, pagingParamsFromRequest(req.Params.PageSize, req.Params.PageToken))
	if err != nil {
		return nil, fmt.Errorf("look up users: %w", err)
	}

	users := make([]gen.UserSummary, 0, len(res.Items))
	for _, user := range res.Items {
		users = append(users, gen.UserSummary{
			Id:       openapi_types.UUID(user.ID),
			Username: user.Username,
			Email:    user.Email,
		})
	}

	var nextPageToken *string
	if res.NextPageToken != "" {
		nextPageToken = &res.NextPageToken
	}

	return gen.LookupUsers200JSONResponse{Users: users, NextPageToken: nextPageToken}, nil
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
	err := h.userService.ResetPassword(ctx, services.ResetPasswordParams{
		ID:          uuid.UUID(req.UserId),
		NewPassword: req.Body.NewPassword,
	})
	if err != nil {
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
		IsServerAdmin: body.IsServerAdmin,
		Disabled:      body.Disabled,
	}
}

func pagingParamsFromRequest(pageSize *int32, pageToken *string) paging.Params {
	var params paging.Params
	if pageSize != nil {
		params.PageSize = *pageSize
	}

	if pageToken != nil {
		params.PageToken = *pageToken
	}

	return params
}
