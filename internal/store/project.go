package store

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// ProjectStore keeps the projects and their members.
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRole, error)
	ListProjectsWithRoleByUser(ctx context.Context, userID uuid.UUID) ([]domain.ProjectRole, error)
}
