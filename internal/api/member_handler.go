package api

import (
	"context"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type memberHandler struct {
	projectService services.ProjectService
}

func newMemberHandler(projectService services.ProjectService) *memberHandler {
	return &memberHandler{projectService: projectService}
}

func (h *memberHandler) ListMembers(ctx context.Context, req gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	res, err := h.projectService.ListMembers(ctx, uuid.UUID(req.ProjectId))
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	members := make([]gen.Member, 0, len(res))
	for _, member := range res {
		members = append(members, memberToResponse(member))
	}

	return gen.ListMembers200JSONResponse{Members: members}, nil
}

func (h *memberHandler) AddMember(ctx context.Context, req gen.AddMemberRequestObject) (gen.AddMemberResponseObject, error) {
	res, err := h.projectService.AddMember(ctx, services.AddMemberParams{
		ProjectID: uuid.UUID(req.ProjectId),
		Username:  req.Body.Username,
		Role:      domain.Role(req.Body.Role),
	})
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	return gen.AddMember201JSONResponse(memberToResponse(res)), nil
}

func (h *memberHandler) UpdateMember(ctx context.Context, req gen.UpdateMemberRequestObject) (gen.UpdateMemberResponseObject, error) {
	res, err := h.projectService.UpdateMember(ctx, services.UpdateMemberParams{
		ProjectID: uuid.UUID(req.ProjectId),
		UserID:    uuid.UUID(req.UserId),
		Role:      domain.Role(req.Body.Role),
	})
	if err != nil {
		return nil, fmt.Errorf("update member: %w", err)
	}

	return gen.UpdateMember200JSONResponse(memberToResponse(res)), nil
}

func (h *memberHandler) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	err := h.projectService.RemoveMember(ctx, services.RemoveMemberParams{
		ProjectID: uuid.UUID(req.ProjectId),
		UserID:    uuid.UUID(req.UserId),
	})
	if err != nil {
		return nil, fmt.Errorf("remove member: %w", err)
	}

	return gen.RemoveMember204Response{}, nil
}

func memberToResponse(member domain.Member) gen.Member {
	return gen.Member{
		UserId:   openapi_types.UUID(member.UserID),
		Username: member.Username,
		Email:    member.Email,
		Role:     gen.Role(member.Role),
	}
}
