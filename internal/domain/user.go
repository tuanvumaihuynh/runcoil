package domain

import (
	"time"
	"uuid"
)

// User is a dashboard account.
type User struct {
	ID            uuid.UUID
	Username      string
	Email         *string
	PasswordHash  string
	IsServerAdmin bool
	DisabledAt    *time.Time
	CreatedAt     time.Time
}

// Role is a user's role in a project.
type Role string

// The roles of a project member.
const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Permission is what a user may do, as resource:verb.
type Permission string

// The permissions of the dashboard API.
const (
	PermissionRunsRead       Permission = "runs:read"
	PermissionRunsCancel     Permission = "runs:cancel"
	PermissionTasksRead      Permission = "tasks:read"
	PermissionSchedulesWrite Permission = "schedules:write"
	PermissionWorkersRead    Permission = "workers:read"
	PermissionMembersRead    Permission = "members:read"
	PermissionMembersWrite   Permission = "members:write"
	PermissionAPIKeysRead    Permission = "api_keys:read"  //nolint:gosec // A permission name, not a credential.
	PermissionAPIKeysWrite   Permission = "api_keys:write" //nolint:gosec // A permission name, not a credential.
	PermissionProjectsRead   Permission = "projects:read"
	PermissionProjectsCreate Permission = "projects:create"
	PermissionUsersRead      Permission = "users:read"
	PermissionUsersWrite     Permission = "users:write"
)

// Permissions lists every Permission.
var Permissions = []Permission{
	PermissionRunsRead,
	PermissionRunsCancel,
	PermissionTasksRead,
	PermissionSchedulesWrite,
	PermissionWorkersRead,
	PermissionMembersRead,
	PermissionMembersWrite,
	PermissionAPIKeysRead,
	PermissionAPIKeysWrite,
	PermissionProjectsRead,
	PermissionProjectsCreate,
	PermissionUsersRead,
	PermissionUsersWrite,
}
