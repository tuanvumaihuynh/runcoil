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

var workersPath = "/api/v1/projects/" + testProjectID.String() + "/workers"

type fakeWorkerService struct {
	workers       []domain.WorkerRecord
	nextPageToken string
	err           error

	listed services.ListWorkersParams
}

func (f *fakeWorkerService) ListWorkers(_ context.Context, params services.ListWorkersParams) (paging.List[domain.WorkerRecord], error) {
	f.listed = params
	return paging.List[domain.WorkerRecord]{Items: f.workers, NextPageToken: f.nextPageToken}, f.err
}

func TestListWorkers(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	workerService := &fakeWorkerService{
		workers: []domain.WorkerRecord{{
			ID:          uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f53"),
			App:         "shop",
			Slots:       10,
			Tasks:       []domain.TaskRef{{ID: uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f54"), Name: "charge"}},
			ConnectedAt: testTime,
			HeartbeatAt: testTime.Add(time.Minute),
		}},
		nextPageToken: "AZpbHnwtcACAAEsdLD5PUg",
	}
	h := newTestServer(t, Config{Port: 8080}, testServices{account: accountService, worker: workerService})

	rec := do(t, h, call{method: http.MethodGet, path: workersPath + "?pageSize=1", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"workers":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","app":"shop","slots":10,"tasks":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f54","name":"charge"}],"connectedAt":"2026-11-14T08:00:00Z","heartbeatAt":"2026-11-14T08:01:00Z"}],"nextPageToken":"AZpbHnwtcACAAEsdLD5PUg"}`, rec.Body.String())
	assert.Equal(t, domain.PermissionWorkersRead, accountService.authorizedPermission)
	assert.Equal(t, services.ListWorkersParams{ProjectID: testProjectID, Paging: paging.Params{PageSize: 1}}, workerService.listed)
}

func TestListEndedWorkers(t *testing.T) {
	endedAt := testTime.Add(time.Hour)
	workerService := &fakeWorkerService{
		workers: []domain.WorkerRecord{{
			ID:          uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f53"),
			App:         "shop",
			Slots:       10,
			ConnectedAt: testTime,
			HeartbeatAt: testTime,
			EndedAt:     &endedAt,
			EndReason:   new(domain.WorkerEndReasonHeartbeatTimeout),
		}},
	}
	h := newTestServer(t, Config{Port: 8080}, testServices{worker: workerService})

	rec := do(t, h, call{method: http.MethodGet, path: workersPath + "?state=ended&pageToken=abc", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"workers":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","app":"shop","slots":10,"tasks":[],"connectedAt":"2026-11-14T08:00:00Z","heartbeatAt":"2026-11-14T08:00:00Z","endedAt":"2026-11-14T09:00:00Z","endReason":"heartbeat_timeout"}]}`, rec.Body.String())
	assert.Equal(t, services.ListWorkersParams{ProjectID: testProjectID, Ended: true, Paging: paging.Params{PageSize: 50, PageToken: "abc"}}, workerService.listed)
}

func TestListWorkersRefused(t *testing.T) {
	t.Run("bad state", func(t *testing.T) {
		h := newTestServer(t, Config{Port: 8080}, testServices{})

		rec := do(t, h, call{method: http.MethodGet, path: workersPath + "?state=gone", token: testToken})

		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		_, fields := decodeError(t, rec)
		assert.Equal(t, []string{"state"}, fields)
	})

	t.Run("bad page token", func(t *testing.T) {
		workerService := &fakeWorkerService{err: apperr.Invalid(apperr.CodeInvalidRequest, "page token is not valid").WithDetail("field", "pageToken")}
		h := newTestServer(t, Config{Port: 8080}, testServices{worker: workerService})

		rec := do(t, h, call{method: http.MethodGet, path: workersPath + "?pageToken=abc", token: testToken})

		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		_, fields := decodeError(t, rec)
		assert.Equal(t, []string{"pageToken"}, fields)
	})

	t.Run("refused in the project", func(t *testing.T) {
		accountService := &fakeAccountService{user: testUser, authorizeErr: apperr.NotFound(apperr.CodeNotFound, "project not found")}
		h := newTestServer(t, Config{Port: 8080}, testServices{account: accountService})

		rec := do(t, h, call{method: http.MethodGet, path: workersPath, token: testToken})

		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})
}
