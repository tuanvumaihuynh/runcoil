package account

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
)

func TestGetUserAccess(t *testing.T) {
	member := []domain.ProjectRole{{ProjectID: uuid.NewV7(), ProjectName: "a", Role: domain.RoleViewer}}
	every := []domain.ProjectRole{{ProjectID: uuid.NewV7(), ProjectName: "b", Role: domain.RoleAdmin}}
	s := newService(&fakeStore{roles: member, projects: every})

	access, err := s.GetUserAccess(context.Background(), domain.User{})
	require.NoError(t, err)
	assert.Equal(t, Access{
		Permissions: []domain.Permission{},
		Projects:    []ProjectAccess{{ProjectRole: member[0], Permissions: viewerPermissions}},
	}, access)

	access, err = s.GetUserAccess(context.Background(), domain.User{IsServerAdmin: true})
	require.NoError(t, err)
	assert.Equal(t, Access{
		Permissions: serverAdminPermissions,
		Projects:    []ProjectAccess{{ProjectRole: every[0], Permissions: adminPermissions}},
	}, access)
}

func TestRolePermissions(t *testing.T) {
	assert.Equal(t, []domain.Permission{"runs:read", "tasks:read", "schedules:read", "workers:read"}, rolePermissions[domain.RoleViewer])
	assert.Equal(t, []domain.Permission{"runs:read", "tasks:read", "schedules:read", "workers:read", "runs:cancel", "schedules:write"}, rolePermissions[domain.RoleEditor])
	assert.Equal(t, []domain.Permission{
		"runs:read", "tasks:read", "schedules:read", "workers:read", "runs:cancel", "schedules:write",
		"members:read", "members:write", "api_keys:read", "api_keys:write",
	}, rolePermissions[domain.RoleAdmin])
}
