package services

import (
	"context"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/pkg/paging"
)

// WorkerService lists the worker sessions of a project.
type WorkerService interface {
	ListWorkers(ctx context.Context, params ListWorkersParams) (paging.List[domain.WorkerRecord], error)
}

// ListWorkersParams asks for one page of a project's workers, newest first: the live ones,
// or the ended ones when Ended is set.
type ListWorkersParams struct {
	ProjectID uuid.UUID
	Ended     bool
	Paging    paging.Params
}
