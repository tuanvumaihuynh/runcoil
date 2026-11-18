package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/pkg/paging"
)

// RunService reads the Runs of a project.
type RunService interface {
	ListRuns(ctx context.Context, params ListRunsParams) (paging.List[domain.RunSummary], error)
	GetRun(ctx context.Context, params GetRunParams) (domain.RunDetail, error)
}

// ListRunsParams asks for one page of a project's Runs that match Filter, newest first.
type ListRunsParams struct {
	ProjectID uuid.UUID
	Filter    domain.RunFilter
	Paging    paging.Params
}

// GetRunParams names one Run of a project.
type GetRunParams struct {
	ProjectID uuid.UUID
	RunID     uuid.UUID
}
