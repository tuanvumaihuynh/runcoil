package api

import (
	"context"
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
	tasksPath  = "/api/v1/projects/" + testProjectID.String() + "/tasks"
	testTaskID = uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f54")
)

type fakeTaskService struct {
	tasks []domain.TaskSummary
	task  domain.TaskDetail
	err   error

	listedProject uuid.UUID
	listedPaging  paging.Params
	gotProject    uuid.UUID
	gotTask       uuid.UUID
}

func (f *fakeTaskService) ListTasks(_ context.Context, params services.ListTasksParams) (paging.List[domain.TaskSummary], error) {
	f.listedProject = params.ProjectID
	f.listedPaging = params.Paging
	return paging.List[domain.TaskSummary]{Items: f.tasks}, f.err
}

func (f *fakeTaskService) GetTask(_ context.Context, params services.GetTaskParams) (domain.TaskDetail, error) {
	f.gotProject = params.ProjectID
	f.gotTask = params.TaskID
	return f.task, f.err
}

func TestListTasks(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	taskService := &fakeTaskService{tasks: []domain.TaskSummary{
		{
			ID:               testTaskID,
			App:              "shop",
			Name:             "nightly-report",
			Schedule:         &domain.TaskSchedule{Cron: "0 2 * * *", Overlap: domain.OverlapSkip, NextTickAt: testTime.Add(time.Hour)},
			LastRun:          &domain.LastRun{ID: testRunID, Status: domain.RunStatusSucceeded, CreatedAt: testTime},
			LiveWorkerCount:  2,
			LastRegisteredAt: testTime,
		},
		{ID: testRunID, App: "shop", Name: "charge", LastRegisteredAt: testTime},
	}}
	h := newTestServer(t, Config{Port: 8080}, testServices{account: accountService, task: taskService})

	rec := do(t, h, call{method: http.MethodGet, path: tasksPath + "?pageSize=2", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"tasks":[
		{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f54","app":"shop","name":"nightly-report",
		 "schedule":{"cron":"0 2 * * *","overlap":"skip","nextTickAt":"2026-11-14T09:00:00Z"},
		 "lastRun":{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f55","status":"succeeded","createdAt":"2026-11-14T08:00:00Z"},
		 "liveWorkerCount":2,"lastRegisteredAt":"2026-11-14T08:00:00Z"},
		{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f55","app":"shop","name":"charge","liveWorkerCount":0,"lastRegisteredAt":"2026-11-14T08:00:00Z"}
	]}`, rec.Body.String())
	assert.Equal(t, domain.PermissionTasksRead, accountService.authorizedPermission)
	assert.Equal(t, testProjectID, taskService.listedProject)
	assert.Equal(t, paging.Params{PageSize: 2}, taskService.listedPaging)
}

func TestGetTask(t *testing.T) {
	deadline := 5 * time.Minute
	pausedAt := testTime
	taskService := &fakeTaskService{task: domain.TaskDetail{
		TaskSummary: domain.TaskSummary{
			ID:               testTaskID,
			App:              "shop",
			Name:             "nightly-report",
			Schedule:         &domain.TaskSchedule{Cron: "*/20 * * * * *", Overlap: domain.OverlapAllow, NextTickAt: testTime, PausedAt: &pausedAt},
			LiveWorkerCount:  1,
			LastRegisteredAt: testTime,
		},
		Retry:         &domain.RetryPolicy{MaxAttempts: 3},
		StartDeadline: &deadline,
		Workers:       []domain.TaskWorker{{ID: testRunID, Slots: 4, ConnectedAt: testTime, HeartbeatAt: testTime}},
		CreatedAt:     testTime,
	}}
	h := newTestServer(t, Config{Port: 8080}, testServices{task: taskService})

	rec := do(t, h, call{method: http.MethodGet, path: tasksPath + "/" + testTaskID.String(), token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f54","app":"shop","name":"nightly-report",
		"retry":{"maxAttempts":3},"startDeadline":"5m0s",
		"schedule":{"cron":"*/20 * * * * *","overlap":"allow","nextTickAt":"2026-11-14T08:00:00Z","pausedAt":"2026-11-14T08:00:00Z"},
		"workers":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f55","slots":4,"connectedAt":"2026-11-14T08:00:00Z","heartbeatAt":"2026-11-14T08:00:00Z"}],
		"createdAt":"2026-11-14T08:00:00Z","lastRegisteredAt":"2026-11-14T08:00:00Z"}`, rec.Body.String())
	assert.Equal(t, testProjectID, taskService.gotProject)
	assert.Equal(t, testTaskID, taskService.gotTask)
}

func TestGetTaskRefused(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		taskService := &fakeTaskService{err: apperr.NotFound(apperr.CodeNotFound, "task not found")}
		h := newTestServer(t, Config{Port: 8080}, testServices{task: taskService})

		rec := do(t, h, call{method: http.MethodGet, path: tasksPath + "/" + testTaskID.String(), token: testToken})

		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("bad task ID", func(t *testing.T) {
		h := newTestServer(t, Config{Port: 8080}, testServices{})

		rec := do(t, h, call{method: http.MethodGet, path: tasksPath + "/charge", token: testToken})

		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}
