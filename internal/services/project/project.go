package project

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// ListProjects returns the projects the caller can open, by name: every project when the
// caller may read Runs server-wide, otherwise the projects they are a member of.
func (s *Service) ListProjects(ctx context.Context, identity domain.Identity) ([]domain.Project, error) {
	var projects []domain.Project
	var err error
	if slices.Contains(identity.Permissions, domain.PermissionRunsRead) {
		projects, err = s.projectStore.ListProjects(ctx)
	} else {
		projects, err = s.projectStore.ListProjectsByMember(ctx, identity.UserID)
	}

	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	return projects, nil
}

// CreateProject creates a project with no members.
func (s *Service) CreateProject(ctx context.Context, name string) (domain.Project, error) {
	project, err := s.projectStore.CreateProject(ctx, domain.Project{ID: uuid.NewV7(), Name: name})
	if errors.Is(err, domain.ErrProjectNameTaken) {
		return domain.Project{}, ErrProjectNameTaken
	}

	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}

	return project, nil
}
