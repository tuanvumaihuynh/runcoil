package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

var _ store.ProjectStore = (*Store)(nil)

// ListProjectsWithRoleByUser returns the projects userID is a member of, with the role
// in each.
func (s *Store) ListProjectsWithRoleByUser(ctx context.Context, userID uuid.UUID) ([]domain.ProjectRole, error) {
	rows, err := s.queries.ProjectMemberListByUser(ctx, s.db, userID)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}

	roles := make([]domain.ProjectRole, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, projectRoleFromRow(row))
	}

	return roles, nil
}

// ListProjects returns every project, each with the role admin, as a server admin sees
// them.
func (s *Store) ListProjects(ctx context.Context) ([]domain.ProjectRole, error) {
	rows, err := s.queries.ProjectList(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	roles := make([]domain.ProjectRole, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, projectRoleFromRow(gen.ProjectMemberListByUserRow{
			ID:   row.ID,
			Name: row.Name,
			Role: string(domain.RoleAdmin),
		}))
	}

	return roles, nil
}
