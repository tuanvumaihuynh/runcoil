package project

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

// ListProjects returns every project, by name.
func (s *Service) ListProjects(ctx context.Context) ([]domain.Project, error) {
	projects, err := s.projectStore.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	return projects, nil
}

// CreateProject creates a project with no members.
func (s *Service) CreateProject(ctx context.Context, name string) (domain.Project, error) {
	project, err := s.projectStore.CreateProject(ctx, domain.Project{ID: uuid.NewV7(), Name: name})
	if err != nil {
		if errors.Is(err, domain.ErrProjectNameTaken) {
			return domain.Project{}, ErrProjectNameTaken
		}

		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}

	return project, nil
}
