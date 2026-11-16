package worker

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

type fakeWorkerStore struct {
	store.WorkerStore
	workers []domain.WorkerRecord
	err     error

	listed      store.ListWorkersParams
	listedEnded bool
}

func (f *fakeWorkerStore) ListLiveWorkers(_ context.Context, params store.ListWorkersParams) ([]domain.WorkerRecord, error) {
	f.listed = params
	return f.workers, f.err
}

func (f *fakeWorkerStore) ListEndedWorkers(_ context.Context, params store.ListWorkersParams) ([]domain.WorkerRecord, error) {
	f.listed = params
	f.listedEnded = true
	return f.workers, f.err
}

func TestListWorkers(t *testing.T) {
	projectID := uuid.NewV7()
	workers := []domain.WorkerRecord{{ID: uuid.NewV7()}, {ID: uuid.NewV7()}, {ID: uuid.NewV7()}}

	tests := []struct {
		name  string
		ended bool
	}{
		{name: "live"},
		{name: "ended", ended: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workerStore := &fakeWorkerStore{workers: workers}

			page, err := New(workerStore).ListWorkers(context.Background(), services.ListWorkersParams{
				ProjectID: projectID,
				Ended:     tt.ended,
				Paging:    paging.Params{PageSize: 2},
			})
			require.NoError(t, err)

			assert.Equal(t, workers[:2], page.Items)
			assert.NotEmpty(t, page.NextPageToken)
			assert.Equal(t, tt.ended, workerStore.listedEnded)
			assert.Equal(t, store.ListWorkersParams{ProjectID: projectID, Limit: 3}, workerStore.listed)

			_, err = New(workerStore).ListWorkers(context.Background(), services.ListWorkersParams{
				ProjectID: projectID,
				Ended:     tt.ended,
				Paging:    paging.Params{PageSize: 2, PageToken: page.NextPageToken},
			})
			require.NoError(t, err)

			assert.Equal(t, &workers[1].ID, workerStore.listed.AfterID)
		})
	}
}

func TestListWorkersRefused(t *testing.T) {
	_, err := New(&fakeWorkerStore{}).ListWorkers(context.Background(), services.ListWorkersParams{
		Paging: paging.Params{PageToken: "not a token"},
	})

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, apperr.KindInvalid, appErr.Kind)
	assert.Equal(t, "pageToken", appErr.Details["field"])

	storeErr := errors.New("down")
	_, err = New(&fakeWorkerStore{err: storeErr}).ListWorkers(context.Background(), services.ListWorkersParams{})

	require.ErrorIs(t, err, storeErr)
}
