package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

// ListMembers returns the members of the project, by username.
func (s *Service) ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error) {
	members, err := s.projectStore.ListMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	return members, nil
}

// AddMember gives the user with the username a role in the project.
func (s *Service) AddMember(ctx context.Context, params services.AddMemberParams) (domain.Member, error) {
	user, err := s.userStore.GetUserByUsername(ctx, strings.ToLower(params.Username))
	if errors.Is(err, domain.ErrUserNotFound) {
		return domain.Member{}, ErrNoSuchUsername
	}

	if err != nil {
		return domain.Member{}, fmt.Errorf("get user: %w", err)
	}

	err = s.projectStore.AddMember(ctx, domain.AddMemberParams{
		ProjectID: params.ProjectID,
		UserID:    user.ID,
		Role:      params.Role,
	})
	if errors.Is(err, domain.ErrMemberExists) {
		return domain.Member{}, ErrMemberExists
	}

	if err != nil {
		return domain.Member{}, fmt.Errorf("add member: %w", err)
	}

	return domain.Member{UserID: user.ID, Username: user.Username, Email: user.Email, Role: params.Role}, nil
}

// UpdateMember gives a member another role in the project.
func (s *Service) UpdateMember(ctx context.Context, params services.UpdateMemberParams) (domain.Member, error) {
	member, err := s.projectStore.UpdateMemberRole(ctx, domain.UpdateMemberRoleParams(params))
	if errors.Is(err, domain.ErrMemberNotFound) {
		return domain.Member{}, ErrMemberNotFound
	}

	if err != nil {
		return domain.Member{}, fmt.Errorf("update member role: %w", err)
	}

	return member, nil
}

// RemoveMember takes userID out of the project.
func (s *Service) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	err := s.projectStore.RemoveMember(ctx, projectID, userID)
	if errors.Is(err, domain.ErrMemberNotFound) {
		return ErrMemberNotFound
	}

	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	return nil
}
