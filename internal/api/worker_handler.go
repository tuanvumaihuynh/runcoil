package api

import (
	"context"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type workerHandler struct {
	workerService services.WorkerService
}

func newWorkerHandler(workerService services.WorkerService) *workerHandler {
	return &workerHandler{workerService: workerService}
}

func (h *workerHandler) ListWorkers(ctx context.Context, req gen.ListWorkersRequestObject) (gen.ListWorkersResponseObject, error) {
	res, err := h.workerService.ListWorkers(ctx, services.ListWorkersParams{
		ProjectID: uuid.UUID(req.ProjectId),
		Ended:     req.Params.State != nil && *req.Params.State == gen.Ended,
		Paging:    pagingParamsFromRequest(req.Params.PageSize, req.Params.PageToken),
	})
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}

	workers := make([]gen.Worker, 0, len(res.Items))
	for _, worker := range res.Items {
		workers = append(workers, workerToResponse(worker))
	}

	var nextPageToken *string
	if res.NextPageToken != "" {
		nextPageToken = &res.NextPageToken
	}

	return gen.ListWorkers200JSONResponse{Workers: workers, NextPageToken: nextPageToken}, nil
}

func workerToResponse(worker domain.WorkerRecord) gen.Worker {
	tasks := make([]gen.WorkerTask, 0, len(worker.Tasks))
	for _, task := range worker.Tasks {
		tasks = append(tasks, gen.WorkerTask{Id: openapi_types.UUID(task.ID), Name: task.Name})
	}

	return gen.Worker{
		Id:          openapi_types.UUID(worker.ID),
		App:         worker.App,
		Slots:       int32(worker.Slots), //nolint:gosec // At most domain.MaxSlots.
		Tasks:       tasks,
		ConnectedAt: worker.ConnectedAt,
		HeartbeatAt: worker.HeartbeatAt,
		EndedAt:     worker.EndedAt,
		EndReason:   (*gen.WorkerEndReason)(worker.EndReason),
	}
}
