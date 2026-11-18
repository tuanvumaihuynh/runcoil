package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// ProjectService manages the projects and their members.
type ProjectService interface {
	ListProjects(ctx context.Context) ([]domain.Project, error)
	CreateProject(ctx context.Context, name string) (domain.Project, error)
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error)
	AddMember(ctx context.Context, params AddMemberParams) (domain.Member, error)
	UpdateMember(ctx context.Context, params UpdateMemberParams) (domain.Member, error)
	RemoveMember(ctx context.Context, params RemoveMemberParams) error
}

// AddMemberParams is a user, by username, to add to a project with a role.
type AddMemberParams struct {
	ProjectID uuid.UUID
	Username  string
	Role      domain.Role
}

// UpdateMemberParams is a member's new role in a project.
type UpdateMemberParams struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Role      domain.Role
}

// RemoveMemberParams is a member to take out of a project.
type RemoveMemberParams struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
}
