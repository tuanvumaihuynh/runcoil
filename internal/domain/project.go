package domain

import (
	"time"
	"uuid"
)

// Project is a set of Tasks, Runs and API keys, and the users who may see them.
type Project struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// Member is a user with a role in a project.
type Member struct {
	UserID   uuid.UUID
	Username string
	Email    *string
	Role     Role
}

// ProjectRole is a project and the role a user has in it.
type ProjectRole struct {
	ProjectID   uuid.UUID
	ProjectName string
	Role        Role
}
