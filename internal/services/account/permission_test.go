package account

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

func TestGetUserAccess(t *testing.T) {
	member := []domain.ProjectRole{{ProjectID: uuid.NewV7(), ProjectName: "a", Role: domain.RoleViewer}}
	every := []domain.Project{{ID: uuid.NewV7(), Name: "b"}}
	s := newService(&fakeStore{roles: member, projects: every})

	access, err := s.GetUserAccess(context.Background(), domain.User{})
	require.NoError(t, err)
	assert.Equal(t, services.Access{
		Permissions: []domain.Permission{},
		Projects:    []services.ProjectAccess{{ProjectRole: member[0], Permissions: viewerPermissions}},
	}, access)

	access, err = s.GetUserAccess(context.Background(), domain.User{IsServerAdmin: true})
	require.NoError(t, err)
	assert.Equal(t, services.Access{
		Permissions: serverAdminPermissions,
		Projects: []services.ProjectAccess{{
			ProjectRole: domain.ProjectRole{ProjectID: every[0].ID, ProjectName: "b", Role: domain.RoleAdmin},
			Permissions: adminPermissions,
		}},
	}, access)
}

func TestRolePermissions(t *testing.T) {
	assert.Equal(t, []domain.Permission{"runs:read", "tasks:read", "workers:read"}, rolePermissions[domain.RoleViewer])
	assert.Equal(t, []domain.Permission{"runs:read", "tasks:read", "workers:read", "runs:cancel", "schedules:write"}, rolePermissions[domain.RoleEditor])
	assert.Equal(t, []domain.Permission{
		"runs:read", "tasks:read", "workers:read", "runs:cancel", "schedules:write",
		"members:read", "members:write", "api_keys:read", "api_keys:write",
	}, rolePermissions[domain.RoleAdmin])
}

func TestAuthorize(t *testing.T) {
	member := domain.Identity{UserID: uuid.NewV7(), Permissions: []domain.Permission{}}
	serverAdmin := domain.Identity{UserID: uuid.NewV7(), Permissions: domain.Permissions}

	tests := []struct {
		name       string
		identity   domain.Identity
		store      fakeStore
		permission domain.Permission
		wantKind   apperr.Kind
	}{
		{name: "a role with the permission", identity: member, store: fakeStore{role: ptr(domain.RoleAdmin)}, permission: domain.PermissionMembersWrite},
		{name: "a role without the permission", identity: member, store: fakeStore{role: ptr(domain.RoleEditor)}, permission: domain.PermissionMembersRead, wantKind: apperr.KindPermissionDenied},
		{name: "not a member", identity: member, permission: domain.PermissionRunsRead, wantKind: apperr.KindNotFound},
		{name: "no project", identity: member, store: fakeStore{roleErr: domain.ErrProjectNotFound}, permission: domain.PermissionRunsRead, wantKind: apperr.KindNotFound},
		{name: "a server admin who is not a member", identity: serverAdmin, permission: domain.PermissionMembersWrite},
		{name: "a server admin and no project", identity: serverAdmin, store: fakeStore{roleErr: domain.ErrProjectNotFound}, permission: domain.PermissionRunsRead, wantKind: apperr.KindNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newService(&tt.store).Authorize(context.Background(), services.AuthorizeParams{Identity: tt.identity, ProjectID: uuid.NewV7(), Permission: tt.permission})
			if tt.wantKind == apperr.KindInternal {
				assert.NoError(t, err)
				return
			}

			appErr, ok := errors.AsType[*apperr.Error](err)
			require.True(t, ok, err)
			assert.Equal(t, tt.wantKind, appErr.Kind)
		})
	}
}

func TestAuthorizeStoreFailure(t *testing.T) {
	s := newService(&fakeStore{roleErr: errors.New("connection refused")})

	err := s.Authorize(context.Background(), services.AuthorizeParams{ProjectID: uuid.NewV7(), Permission: domain.PermissionRunsRead})

	assert.EqualError(t, err, "get project role: connection refused")
}
