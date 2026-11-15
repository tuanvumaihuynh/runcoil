package project

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/service"
)

// The errors the service returns to clients.
var (
	ErrProjectNameTaken = apperr.Conflict(apperr.CodeAlreadyExists, "project name is taken").WithDetail("field", "name")
	ErrMemberNotFound   = apperr.NotFound(apperr.CodeNotFound, "user is not a member of the project")
	ErrMemberExists     = apperr.Conflict(apperr.CodeAlreadyExists, "user is already a member of the project").WithDetail("field", "username")
	ErrNoSuchUsername   = apperr.Invalid(apperr.CodeInvalidRequest, "no user has this username").WithDetail("field", "username")
)

// ProjectStore keeps the projects and their members.
type ProjectStore interface {
	CreateProject(ctx context.Context, project domain.Project) (domain.Project, error)
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ListProjectsByMember(ctx context.Context, userID uuid.UUID) ([]domain.Project, error)
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error)
	AddMember(ctx context.Context, params domain.AddMemberParams) error
	UpdateMemberRole(ctx context.Context, params domain.UpdateMemberRoleParams) (domain.Member, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
}

// UserStore finds the users to add to a project.
type UserStore interface {
	GetUserByUsername(ctx context.Context, username string) (domain.User, error)
}

var _ service.ProjectService = (*Service)(nil)

// Service manages the projects and their members.
type Service struct {
	projectStore ProjectStore
	userStore    UserStore
}

// New creates the project service.
func New(projectStore ProjectStore, userStore UserStore) *Service {
	return &Service{projectStore: projectStore, userStore: userStore}
}
