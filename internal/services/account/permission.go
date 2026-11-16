package account

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

var viewerPermissions = []domain.Permission{
	domain.PermissionRunsRead,
	domain.PermissionTasksRead,
	domain.PermissionWorkersRead,
}

var editorPermissions = append(slices.Clone(viewerPermissions),
	domain.PermissionRunsCancel,
	domain.PermissionSchedulesWrite,
)

var adminPermissions = append(slices.Clone(editorPermissions),
	domain.PermissionMembersRead,
	domain.PermissionMembersWrite,
	domain.PermissionAPIKeysRead,
	domain.PermissionAPIKeysWrite,
)

var rolePermissions = map[domain.Role][]domain.Permission{
	domain.RoleViewer: viewerPermissions,
	domain.RoleEditor: editorPermissions,
	domain.RoleAdmin:  adminPermissions,
}

var serverAdminPermissions = []domain.Permission{
	domain.PermissionProjectsRead,
	domain.PermissionProjectsCreate,
	domain.PermissionUsersRead,
	domain.PermissionUsersWrite,
}

// GetUserAccess returns what user may do. A server admin is admin in every project.
func (s *Service) GetUserAccess(ctx context.Context, user domain.User) (services.Access, error) {
	access := services.Access{Permissions: []domain.Permission{}}

	roles, err := s.listProjectRoles(ctx, user)
	if err != nil {
		return services.Access{}, fmt.Errorf("list project roles: %w", err)
	}

	if user.IsServerAdmin {
		access.Permissions = serverAdminPermissions
	}

	access.Projects = make([]services.ProjectAccess, 0, len(roles))
	for _, role := range roles {
		access.Projects = append(access.Projects, services.ProjectAccess{
			ProjectRole: role,
			Permissions: rolePermissions[role.Role],
		})
	}

	return access, nil
}

func (s *Service) listProjectRoles(ctx context.Context, user domain.User) ([]domain.ProjectRole, error) {
	if !user.IsServerAdmin {
		roles, err := s.projectStore.ListProjectsWithRoleByUser(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("list projects of user: %w", err)
		}

		return roles, nil
	}

	projects, err := s.projectStore.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	roles := make([]domain.ProjectRole, 0, len(projects))
	for _, project := range projects {
		roles = append(roles, domain.ProjectRole{ProjectID: project.ID, ProjectName: project.Name, Role: domain.RoleAdmin})
	}

	return roles, nil
}

// Authorize checks that the identity has the permission in the project. A permission the identity
// holds server-wide holds in every project. A user who is not a member gets
// ErrProjectNotFound, so a project is hidden from those who cannot open it.
func (s *Service) Authorize(ctx context.Context, params services.AuthorizeParams) error {
	role, err := s.projectStore.GetProjectRoleOfUser(ctx, params.ProjectID, params.Identity.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return ErrProjectNotFound
		}

		return fmt.Errorf("get project role: %w", err)
	}

	if slices.Contains(params.Identity.Permissions, params.Permission) {
		return nil
	}

	if role == nil {
		return ErrProjectNotFound
	}

	if !slices.Contains(rolePermissions[*role], params.Permission) {
		return ErrPermissionDenied
	}

	return nil
}

// userPermissions returns what user may do anywhere on the server. A server admin has
// every permission, in every project too.
func userPermissions(user domain.User) []domain.Permission {
	if user.IsServerAdmin {
		return domain.Permissions
	}

	return []domain.Permission{}
}
