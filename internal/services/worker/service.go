package worker

import (
	"context"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

// ErrInvalidPageToken is returned for a page token that no list gave.
var ErrInvalidPageToken = apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")

var _ services.WorkerService = (*Service)(nil)

// Service lists the worker sessions of a project.
type Service struct {
	workerStore store.WorkerStore
}

// New creates the worker service.
func New(workerStore store.WorkerStore) *Service {
	return &Service{workerStore: workerStore}
}

// ListWorkers returns one page of the project's live or ended workers, newest first.
func (s *Service) ListWorkers(ctx context.Context, params services.ListWorkersParams) (paging.List[domain.WorkerRecord], error) {
	afterID, err := params.Paging.AfterID()
	if err != nil {
		return paging.List[domain.WorkerRecord]{}, ErrInvalidPageToken
	}

	storeParams := store.ListWorkersParams{
		ProjectID: params.ProjectID,
		AfterID:   afterID,
		Limit:     params.Paging.Limit(),
	}

	var workers []domain.WorkerRecord
	if params.Ended {
		workers, err = s.workerStore.ListEndedWorkers(ctx, storeParams)
	} else {
		workers, err = s.workerStore.ListLiveWorkers(ctx, storeParams)
	}

	if err != nil {
		return paging.List[domain.WorkerRecord]{}, fmt.Errorf("list workers: %w", err)
	}

	return paging.NewList(params.Paging, workers, func(worker domain.WorkerRecord) uuid.UUID { return worker.ID }), nil
}
