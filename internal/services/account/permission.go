package account

import (
	"context"
	"fmt"
	"slices"

	"github.com/runcoil/runcoil/internal/domain"
)

var viewerPermissions = []domain.Permission{
	domain.PermissionRunsRead,
	domain.PermissionTasksRead,
	domain.PermissionSchedulesRead,
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
	domain.PermissionProjectsCreate,
	domain.PermissionUsersRead,
	domain.PermissionUsersWrite,
}

// Access is what a user may do: the server-wide permissions and the projects the user can
// open.
type Access struct {
	Permissions []domain.Permission
	Projects    []ProjectAccess
}

// ProjectAccess is a project the user can open, with the role and its permissions.
type ProjectAccess struct {
	domain.ProjectRole
	Permissions []domain.Permission
}

// GetUserAccess returns what user may do. A server admin is admin in every project.
func (s *Service) GetUserAccess(ctx context.Context, user domain.User) (Access, error) {
	access := Access{Permissions: []domain.Permission{}}

	var roles []domain.ProjectRole
	var err error
	if user.IsServerAdmin {
		access.Permissions = serverAdminPermissions
		roles, err = s.projectStore.ListProjects(ctx)
	} else {
		roles, err = s.projectStore.ListProjectsWithRoleByUser(ctx, user.ID)
	}

	if err != nil {
		return Access{}, fmt.Errorf("list project roles: %w", err)
	}

	access.Projects = make([]ProjectAccess, 0, len(roles))
	for _, role := range roles {
		access.Projects = append(access.Projects, ProjectAccess{
			ProjectRole: role,
			Permissions: rolePermissions[role.Role],
		})
	}

	return access, nil
}

// userPermissions returns what user may do anywhere on the server. A server admin has
// every permission, in every project too.
func userPermissions(user domain.User) []domain.Permission {
	if user.IsServerAdmin {
		return domain.Permissions
	}

	return []domain.Permission{}
}
