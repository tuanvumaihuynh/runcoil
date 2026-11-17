package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/pkg/paging"
)

var (
	runsPath  = "/api/v1/projects/" + testProjectID.String() + "/runs"
	testRunID = uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f55")
)

type fakeRunService struct {
	runs []domain.RunSummary
	run  domain.RunDetail
	err  error

	listed     services.ListRunsParams
	gotProject uuid.UUID
	gotRun     uuid.UUID
}

func (f *fakeRunService) ListRuns(_ context.Context, params services.ListRunsParams) (paging.List[domain.RunSummary], error) {
	f.listed = params
	return paging.List[domain.RunSummary]{Items: f.runs}, f.err
}

func (f *fakeRunService) GetRun(_ context.Context, projectID, runID uuid.UUID) (domain.RunDetail, error) {
	f.gotProject = projectID
	f.gotRun = runID
	return f.run, f.err
}

func testRunSummary() domain.RunSummary {
	endedAt := testTime.Add(9 * time.Second)

	return domain.RunSummary{
		ID:        testRunID,
		Status:    domain.RunStatusFailed,
		TaskID:    uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f54"),
		TaskApp:   "shop",
		TaskName:  "nightly-report",
		Failure:   &domain.Failure{Code: domain.FailureCodeStepFailed, Message: "totals service timed out"},
		CreatedAt: testTime,
		StartedAt: &testTime,
		EndedAt:   &endedAt,
	}
}

func TestListRuns(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	runService := &fakeRunService{runs: []domain.RunSummary{testRunSummary()}}
	h := newTestServer(t, Config{Port: 8080}, testServices{account: accountService, run: runService})

	rec := do(t, h, call{method: http.MethodGet, path: runsPath + "?status=running&status=failed&taskId=019a5b1e-7c2d-7000-8000-4b1d2c3e4f54", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"runs":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f55","status":"failed","task":{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f54","app":"shop","name":"nightly-report"},"errorCode":"step_failed","createdAt":"2026-11-14T08:00:00Z","startedAt":"2026-11-14T08:00:00Z","endedAt":"2026-11-14T08:00:09Z"}]}`, rec.Body.String())
	assert.Equal(t, domain.PermissionRunsRead, accountService.authorizedPermission)

	taskID := uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f54")
	assert.Equal(t, services.ListRunsParams{
		ProjectID: testProjectID,
		Filter:    domain.RunFilter{Statuses: []domain.RunStatus{domain.RunStatusRunning, domain.RunStatusFailed}, TaskID: &taskID},
		Paging:    paging.Params{PageSize: 50},
	}, runService.listed)
}

func TestListRunsRefused(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
	}{
		{name: "bad status", query: "?status=paused", field: "status"},
		{name: "bad task", query: "?taskId=charge", field: "taskId"},
		{name: "bad page size", query: "?pageSize=0", field: "pageSize"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(t, Config{Port: 8080}, testServices{})

			rec := do(t, h, call{method: http.MethodGet, path: runsPath + tt.query, token: testToken})

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			_, fields := decodeError(t, rec)
			assert.Equal(t, []string{tt.field}, fields)
		})
	}
}

func TestGetRun(t *testing.T) {
	summary := testRunSummary()
	summary.Status = domain.RunStatusRunning
	summary.Failure = nil
	summary.EndedAt = nil
	retryStep := "sum-totals"
	runService := &fakeRunService{run: domain.RunDetail{
		RunSummary: summary,
		Input:      []byte(`{"orderId":12345678901234567890}`),
		Retry:      &domain.RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{2 * time.Second}},
		Waiting:    &domain.RunWaiting{Reason: domain.RunWaitRetry, ReadyAt: testTime.Add(4 * time.Second), RetryStepID: &retryStep, RetryCount: 1},
		Steps: []domain.StepDetail{
			{StepRecord: domain.StepRecord{StepID: "load-orders", Kind: domain.StepKindDo, Output: []byte(`142`)}, TryCount: 1, RecordedAt: testTime.Add(time.Second)},
			{StepRecord: domain.StepRecord{StepID: "notify", Kind: domain.StepKindDo, Failure: &domain.Failure{Code: domain.FailureCodeError, Message: "mail down"}}, TryCount: 3, RecordedAt: testTime.Add(2 * time.Second)},
			{StepRecord: domain.StepRecord{StepID: "raw", Kind: domain.StepKindDo, Output: []byte(`not json`)}, TryCount: 1, RecordedAt: testTime.Add(3 * time.Second)},
		},
	}}
	h := newTestServer(t, Config{Port: 8080}, testServices{run: runService})

	rec := do(t, h, call{method: http.MethodGet, path: runsPath + "/" + testRunID.String(), token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f55","status":"running",
		"task":{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f54","app":"shop","name":"nightly-report"},
		"createdAt":"2026-11-14T08:00:00Z","startedAt":"2026-11-14T08:00:00Z",
		"input":{"orderId":12345678901234567890},
		"config":{"retry":{"maxAttempts":3,"delays":["2s"]}},
		"waiting":{"reason":"retry","readyAt":"2026-11-14T08:00:04Z","retryStepId":"sum-totals","retryCount":1},
		"steps":[
			{"id":"load-orders","kind":"do","status":"succeeded","output":142,"tryCount":1,"recordedAt":"2026-11-14T08:00:01Z"},
			{"id":"notify","kind":"do","status":"failed","error":{"code":"error","message":"mail down"},"tryCount":3,"recordedAt":"2026-11-14T08:00:02Z"},
			{"id":"raw","kind":"do","status":"succeeded","output":"not json","tryCount":1,"recordedAt":"2026-11-14T08:00:03Z"}
		]
	}`, rec.Body.String())
	var body struct {
		Input struct {
			OrderID json.Number `json:"orderId"`
		} `json:"input"`
	}
	dec := json.NewDecoder(rec.Body)
	dec.UseNumber()
	require.NoError(t, dec.Decode(&body))
	assert.Equal(t, json.Number("12345678901234567890"), body.Input.OrderID, "input keeps its digits")
	assert.Equal(t, testProjectID, runService.gotProject)
	assert.Equal(t, testRunID, runService.gotRun)
}

func TestGetRunAttemptAndConfig(t *testing.T) {
	workerID := uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f53")
	summary := testRunSummary()
	summary.Status = domain.RunStatusRunning
	summary.Failure = nil
	summary.EndedAt = nil
	runService := &fakeRunService{run: domain.RunDetail{
		RunSummary: summary,
		Attempt:    &domain.RunAttempt{WorkerID: workerID, StartedAt: testTime},
	}}
	h := newTestServer(t, Config{Port: 8080}, testServices{run: runService})

	rec := do(t, h, call{method: http.MethodGet, path: runsPath + "/" + testRunID.String(), token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"config":{}`)
	assert.Contains(t, rec.Body.String(), `"attempt":{"workerId":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","startedAt":"2026-11-14T08:00:00Z"}`)
	assert.Contains(t, rec.Body.String(), `"steps":[]`)
}

func TestGetRunNotFound(t *testing.T) {
	runService := &fakeRunService{err: apperr.NotFound(apperr.CodeNotFound, "run not found")}
	h := newTestServer(t, Config{Port: 8080}, testServices{run: runService})

	rec := do(t, h, call{method: http.MethodGet, path: runsPath + "/" + testRunID.String(), token: testToken})

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	code, _ := decodeError(t, rec)
	assert.Equal(t, "not_found", code)
}
