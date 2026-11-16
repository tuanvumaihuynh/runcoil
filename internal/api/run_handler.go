package api

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type runHandler struct {
	runService services.RunService
}

func newRunHandler(runService services.RunService) *runHandler {
	return &runHandler{runService: runService}
}

func (h *runHandler) ListRuns(ctx context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	var filter domain.RunFilter
	if req.Params.Status != nil {
		for _, status := range *req.Params.Status {
			filter.Statuses = append(filter.Statuses, domain.RunStatus(status))
		}
	}

	if req.Params.TaskId != nil {
		taskID := uuid.UUID(*req.Params.TaskId)
		filter.TaskID = &taskID
	}

	res, err := h.runService.ListRuns(ctx, services.ListRunsParams{
		ProjectID: uuid.UUID(req.ProjectId),
		Filter:    filter,
		Paging:    pagingParamsFromRequest(req.Params.PageSize, req.Params.PageToken),
	})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	runs := make([]gen.RunSummary, 0, len(res.Items))
	for _, run := range res.Items {
		runs = append(runs, runSummaryToResponse(run))
	}

	var nextPageToken *string
	if res.NextPageToken != "" {
		nextPageToken = &res.NextPageToken
	}

	return gen.ListRuns200JSONResponse{Runs: runs, NextPageToken: nextPageToken}, nil
}

func (h *runHandler) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	res, err := h.runService.GetRun(ctx, uuid.UUID(req.ProjectId), uuid.UUID(req.RunId))
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	return gen.GetRun200JSONResponse(runToResponse(res)), nil
}

func runTaskToResponse(run domain.RunSummary) gen.RunTask {
	return gen.RunTask{Id: openapi_types.UUID(run.TaskID), App: run.TaskApp, Name: run.TaskName}
}

func runSummaryToResponse(run domain.RunSummary) gen.RunSummary {
	summary := gen.RunSummary{
		Id:        openapi_types.UUID(run.ID),
		Status:    gen.RunStatus(run.Status),
		Task:      runTaskToResponse(run),
		CreatedAt: run.CreatedAt,
		StartedAt: run.StartedAt,
		EndedAt:   run.EndedAt,
	}
	if run.Failure != nil {
		summary.ErrorCode = new(string(run.Failure.Code))
	}

	return summary
}

func runToResponse(run domain.RunDetail) gen.Run {
	res := gen.Run{
		Id:        openapi_types.UUID(run.ID),
		Status:    gen.RunStatus(run.Status),
		Task:      runTaskToResponse(run.RunSummary),
		CreatedAt: run.CreatedAt,
		StartedAt: run.StartedAt,
		EndedAt:   run.EndedAt,
		Input:     jsonToResponse(run.Input),
		Output:    jsonToResponse(run.Output),
		Error:     failureToResponse(run.Failure),
		Steps:     make([]gen.Step, 0, len(run.Steps)),
	}

	if run.Retry != nil {
		res.Config.Retry = retryPolicyToResponse(*run.Retry)
	}

	if run.Waiting != nil {
		res.Waiting = &gen.RunWaiting{
			Reason:          gen.RunWaitingReason(run.Waiting.Reason),
			ReadyAt:         run.Waiting.ReadyAt,
			RetryStepId:     run.Waiting.RetryStepID,
			RetryCount:      int32(run.Waiting.RetryCount), //nolint:gosec // A smallint in storage.
			StartDeadlineAt: run.Waiting.StartDeadlineAt,
		}
	}

	if run.Attempt != nil {
		res.Attempt = &gen.RunAttempt{WorkerId: openapi_types.UUID(run.Attempt.WorkerID), StartedAt: run.Attempt.StartedAt}
	}

	for _, step := range run.Steps {
		status := gen.StepStatusSucceeded
		if step.Failure != nil {
			status = gen.StepStatusFailed
		}

		res.Steps = append(res.Steps, gen.Step{
			Id:         step.StepID,
			Kind:       gen.StepKind(step.Kind),
			Status:     status,
			Output:     jsonToResponse(step.Output),
			Error:      failureToResponse(step.Failure),
			TryCount:   int32(step.TryCount), //nolint:gosec // At most domain.MaxRetryAttempts.
			RecordedAt: step.RecordedAt,
		})
	}

	return res
}

func failureToResponse(failure *domain.Failure) *gen.Failure {
	if failure == nil {
		return nil
	}

	return &gen.Failure{Code: string(failure.Code), Message: failure.Message}
}

func retryPolicyToResponse(policy domain.RetryPolicy) *gen.RetryPolicy {
	res := &gen.RetryPolicy{}
	if policy.MaxAttempts > 0 {
		res.MaxAttempts = new(int32(policy.MaxAttempts)) //nolint:gosec // At most domain.MaxRetryAttempts.
	}

	if len(policy.Delays) > 0 {
		delays := make([]string, 0, len(policy.Delays))
		for _, d := range policy.Delays {
			delays = append(delays, d.String())
		}

		res.Delays = &delays
	}

	return res
}

// jsonToResponse returns stored JSON as it is. A worker's bytes that are not JSON are
// sent as a JSON string, so one bad value does not fail the whole response.
func jsonToResponse(b []byte) *json.RawMessage {
	if b == nil {
		return nil
	}

	raw := json.RawMessage(b)
	if !json.Valid(b) {
		quoted, err := json.Marshal(string(b))
		if err != nil {
			return nil
		}

		raw = quoted
	}

	return &raw
}
