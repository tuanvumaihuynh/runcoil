package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// ProjectStore keeps the projects and their members.
type ProjectStore interface {
	CreateProject(ctx context.Context, project domain.Project) (domain.Project, error)
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ListProjectsByMember(ctx context.Context, userID uuid.UUID) ([]domain.Project, error)
	ListProjectsWithRoleByUser(ctx context.Context, userID uuid.UUID) ([]domain.ProjectRole, error)
	GetProjectRoleOfUser(ctx context.Context, projectID, userID uuid.UUID) (*domain.Role, error)
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error)
	AddMember(ctx context.Context, params domain.AddMemberParams) error
	UpdateMemberRole(ctx context.Context, params domain.UpdateMemberRoleParams) (domain.Member, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
}
