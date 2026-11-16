package api

import (
	"context"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type projectHandler struct {
	projectService services.ProjectService
}

func newProjectHandler(projectService services.ProjectService) *projectHandler {
	return &projectHandler{projectService: projectService}
}

func (h *projectHandler) ListProjects(ctx context.Context, _ gen.ListProjectsRequestObject) (gen.ListProjectsResponseObject, error) {
	res, err := h.projectService.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	projects := make([]gen.Project, 0, len(res))
	for _, project := range res {
		projects = append(projects, projectToResponse(project))
	}

	return gen.ListProjects200JSONResponse{Projects: projects}, nil
}

func (h *projectHandler) CreateProject(ctx context.Context, req gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	res, err := h.projectService.CreateProject(ctx, req.Body.Name)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	return gen.CreateProject201JSONResponse(projectToResponse(res)), nil
}

func projectToResponse(project domain.Project) gen.Project {
	return gen.Project{
		Id:        openapi_types.UUID(project.ID),
		Name:      project.Name,
		CreatedAt: project.CreatedAt,
	}
}
