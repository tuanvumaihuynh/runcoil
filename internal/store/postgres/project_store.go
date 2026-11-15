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

// CreateProject creates project. It returns domain.ErrProjectNameTaken when another
// project has the name.
func (s *Store) CreateProject(ctx context.Context, project domain.Project) (domain.Project, error) {
	row, err := s.queries.ProjectCreate(ctx, s.db, gen.ProjectCreateParams{
		ID:   project.ID,
		Name: project.Name,
	})
	if constraint, ok := uniqueViolation(err); ok && constraint == "projects_name_key" {
		return domain.Project{}, domain.ErrProjectNameTaken
	}

	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}

	return domain.Project(row), nil
}

// ListProjects returns every project, by name.
func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.queries.ProjectList(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	projects := make([]domain.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, domain.Project(row))
	}

	return projects, nil
}

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

// GetProjectRoleOfUser returns the role of userID in the project, nil when the user is not
// a member, or domain.ErrProjectNotFound.
func (s *Store) GetProjectRoleOfUser(ctx context.Context, projectID, userID uuid.UUID) (*domain.Role, error) {
	row, err := s.queries.ProjectGetRoleOfUser(ctx, s.db, gen.ProjectGetRoleOfUserParams{
		ProjectID: projectID,
		UserID:    userID,
	})
	if isNoRowsError(err) {
		return nil, domain.ErrProjectNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get project role: %w", err)
	}

	if row.Role == nil {
		return nil, nil
	}

	role := domain.Role(*row.Role)

	return &role, nil
}

// ListMembers returns the members of the project, by username.
func (s *Store) ListMembers(ctx context.Context, projectID uuid.UUID) ([]domain.Member, error) {
	rows, err := s.queries.ProjectMemberList(ctx, s.db, projectID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	members := make([]domain.Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, memberFromRow(row))
	}

	return members, nil
}

// AddMember gives a user a role in a project. It returns domain.ErrMemberExists when the
// user is already a member.
func (s *Store) AddMember(ctx context.Context, params store.AddMemberParams) error {
	err := s.queries.ProjectMemberAdd(ctx, s.db, gen.ProjectMemberAddParams{
		ProjectID: params.ProjectID,
		UserID:    params.UserID,
		Role:      string(params.Role),
	})
	if constraint, ok := uniqueViolation(err); ok && constraint == "project_members_pkey" {
		return domain.ErrMemberExists
	}

	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	return nil
}

// UpdateMemberRole gives a member another role in a project. It returns
// domain.ErrMemberNotFound when the user is not a member.
func (s *Store) UpdateMemberRole(ctx context.Context, params store.UpdateMemberRoleParams) (domain.Member, error) {
	row, err := s.queries.ProjectMemberUpdateRole(ctx, s.db, gen.ProjectMemberUpdateRoleParams{
		ProjectID: params.ProjectID,
		UserID:    params.UserID,
		Role:      string(params.Role),
	})
	if isNoRowsError(err) {
		return domain.Member{}, domain.ErrMemberNotFound
	}

	if err != nil {
		return domain.Member{}, fmt.Errorf("update member role: %w", err)
	}

	return memberFromRow(gen.ProjectMemberListRow(row)), nil
}

// RemoveMember takes userID out of the project. It returns domain.ErrMemberNotFound when
// the user is not a member.
func (s *Store) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	rows, err := s.queries.ProjectMemberRemove(ctx, s.db, gen.ProjectMemberRemoveParams{
		ProjectID: projectID,
		UserID:    userID,
	})
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	if rows == 0 {
		return domain.ErrMemberNotFound
	}

	return nil
}
