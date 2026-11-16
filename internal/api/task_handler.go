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

type taskHandler struct {
	taskService services.TaskService
}

func newTaskHandler(taskService services.TaskService) *taskHandler {
	return &taskHandler{taskService: taskService}
}

func (h *taskHandler) ListTasks(ctx context.Context, req gen.ListTasksRequestObject) (gen.ListTasksResponseObject, error) {
	res, err := h.taskService.ListTasks(ctx, uuid.UUID(req.ProjectId), pagingParamsFromRequest(req.Params.PageSize, req.Params.PageToken))
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]gen.TaskSummary, 0, len(res.Items))
	for _, task := range res.Items {
		tasks = append(tasks, taskSummaryToResponse(task))
	}

	var nextPageToken *string
	if res.NextPageToken != "" {
		nextPageToken = &res.NextPageToken
	}

	return gen.ListTasks200JSONResponse{Tasks: tasks, NextPageToken: nextPageToken}, nil
}

func (h *taskHandler) GetTask(ctx context.Context, req gen.GetTaskRequestObject) (gen.GetTaskResponseObject, error) {
	res, err := h.taskService.GetTask(ctx, uuid.UUID(req.ProjectId), uuid.UUID(req.TaskId))
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	return gen.GetTask200JSONResponse(taskToResponse(res)), nil
}

func taskScheduleToResponse(schedule *domain.TaskSchedule) *gen.TaskSchedule {
	if schedule == nil {
		return nil
	}

	return &gen.TaskSchedule{
		Cron:       schedule.Cron,
		Overlap:    gen.TaskScheduleOverlap(schedule.Overlap),
		NextTickAt: schedule.NextTickAt,
		PausedAt:   schedule.PausedAt,
	}
}

func lastRunToResponse(run *domain.LastRun) *gen.TaskLastRun {
	if run == nil {
		return nil
	}

	return &gen.TaskLastRun{Id: openapi_types.UUID(run.ID), Status: gen.RunStatus(run.Status), CreatedAt: run.CreatedAt}
}

func taskSummaryToResponse(task domain.TaskSummary) gen.TaskSummary {
	return gen.TaskSummary{
		Id:               openapi_types.UUID(task.ID),
		App:              task.App,
		Name:             task.Name,
		Schedule:         taskScheduleToResponse(task.Schedule),
		LastRun:          lastRunToResponse(task.LastRun),
		LiveWorkerCount:  int32(task.LiveWorkerCount), //nolint:gosec // A count of live workers.
		LastRegisteredAt: task.LastRegisteredAt,
	}
}

func taskToResponse(task domain.TaskDetail) gen.Task {
	res := gen.Task{
		Id:               openapi_types.UUID(task.ID),
		App:              task.App,
		Name:             task.Name,
		Schedule:         taskScheduleToResponse(task.Schedule),
		LastRun:          lastRunToResponse(task.LastRun),
		Workers:          make([]gen.TaskWorker, 0, len(task.Workers)),
		CreatedAt:        task.CreatedAt,
		LastRegisteredAt: task.LastRegisteredAt,
	}

	if task.Retry != nil {
		res.Retry = retryPolicyToResponse(*task.Retry)
	}

	if task.StartDeadline != nil {
		res.StartDeadline = new(task.StartDeadline.String())
	}

	for _, worker := range task.Workers {
		res.Workers = append(res.Workers, gen.TaskWorker{
			Id:          openapi_types.UUID(worker.ID),
			Slots:       int32(worker.Slots), //nolint:gosec // At most domain.MaxSlots.
			ConnectedAt: worker.ConnectedAt,
			HeartbeatAt: worker.HeartbeatAt,
		})
	}

	return res
}
