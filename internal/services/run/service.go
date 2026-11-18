package run

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

// The errors the service returns to clients.
var (
	ErrRunNotFound      = apperr.NotFound(apperr.CodeNotFound, "run not found")
	ErrInvalidPageToken = apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")
)

var _ services.RunService = (*Service)(nil)

// Service reads the Runs of a project.
type Service struct {
	runStore store.RunStore
}

// New creates the run service.
func New(runStore store.RunStore) *Service {
	return &Service{runStore: runStore}
}

// ListRuns returns one page of the project's Runs that match the filter, newest first.
func (s *Service) ListRuns(ctx context.Context, params services.ListRunsParams) (paging.List[domain.RunSummary], error) {
	afterID, err := params.Paging.AfterID()
	if err != nil {
		return paging.List[domain.RunSummary]{}, ErrInvalidPageToken
	}

	runs, err := s.runStore.ListRuns(ctx, store.ListRunsParams{
		ProjectID: params.ProjectID,
		Filter:    params.Filter,
		AfterID:   afterID,
		Limit:     params.Paging.Limit(),
	})
	if err != nil {
		return paging.List[domain.RunSummary]{}, fmt.Errorf("list runs: %w", err)
	}

	return paging.NewList(params.Paging, runs, func(run domain.RunSummary) uuid.UUID { return run.ID }), nil
}

// GetRun returns a Run of the project with its Steps and what it waits for.
func (s *Service) GetRun(ctx context.Context, params services.GetRunParams) (domain.RunDetail, error) {
	run, err := s.runStore.GetRunDetail(ctx, params.ProjectID, params.RunID)
	if err != nil {
		if errors.Is(err, domain.ErrRunNotFound) {
			return domain.RunDetail{}, ErrRunNotFound
		}

		return domain.RunDetail{}, fmt.Errorf("get run: %w", err)
	}

	return run, nil
}
