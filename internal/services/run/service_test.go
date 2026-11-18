package run

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

type fakeRunStore struct {
	store.RunStore
	runs []domain.RunSummary
	run  domain.RunDetail
	err  error

	listed store.ListRunsParams
	gotIDs [2]uuid.UUID
}

func (f *fakeRunStore) ListRuns(_ context.Context, params store.ListRunsParams) ([]domain.RunSummary, error) {
	f.listed = params
	return f.runs, f.err
}

func (f *fakeRunStore) GetRunDetail(_ context.Context, projectID, runID uuid.UUID) (domain.RunDetail, error) {
	f.gotIDs = [2]uuid.UUID{projectID, runID}
	return f.run, f.err
}

func TestListRuns(t *testing.T) {
	projectID := uuid.NewV7()
	taskID := uuid.NewV7()
	runs := []domain.RunSummary{{ID: uuid.NewV7()}, {ID: uuid.NewV7()}, {ID: uuid.NewV7()}}
	filter := domain.RunFilter{Statuses: []domain.RunStatus{domain.RunStatusFailed}, TaskID: &taskID}
	runStore := &fakeRunStore{runs: runs}

	page, err := New(runStore).ListRuns(context.Background(), services.ListRunsParams{
		ProjectID: projectID,
		Filter:    filter,
		Paging:    paging.Params{PageSize: 2},
	})
	require.NoError(t, err)

	assert.Equal(t, runs[:2], page.Items)
	assert.Equal(t, store.ListRunsParams{ProjectID: projectID, Filter: filter, Limit: 3}, runStore.listed)

	_, err = New(runStore).ListRuns(context.Background(), services.ListRunsParams{
		ProjectID: projectID,
		Paging:    paging.Params{PageSize: 2, PageToken: page.NextPageToken},
	})
	require.NoError(t, err)

	assert.Equal(t, &runs[1].ID, runStore.listed.AfterID)
}

func TestListRunsRefused(t *testing.T) {
	_, err := New(&fakeRunStore{}).ListRuns(context.Background(), services.ListRunsParams{
		Paging: paging.Params{PageToken: "not a token"},
	})

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, apperr.KindInvalid, appErr.Kind)
	assert.Equal(t, "pageToken", appErr.Details["field"])

	storeErr := errors.New("down")
	_, err = New(&fakeRunStore{err: storeErr}).ListRuns(context.Background(), services.ListRunsParams{})

	require.ErrorIs(t, err, storeErr)
}

func TestGetRun(t *testing.T) {
	projectID, runID := uuid.NewV7(), uuid.NewV7()
	runStore := &fakeRunStore{run: domain.RunDetail{RunSummary: domain.RunSummary{ID: runID}}}

	run, err := New(runStore).GetRun(context.Background(), services.GetRunParams{ProjectID: projectID, RunID: runID})
	require.NoError(t, err)

	assert.Equal(t, runID, run.ID)
	assert.Equal(t, [2]uuid.UUID{projectID, runID}, runStore.gotIDs)

	_, err = New(&fakeRunStore{err: domain.ErrRunNotFound}).GetRun(context.Background(), services.GetRunParams{ProjectID: projectID, RunID: runID})

	require.ErrorIs(t, err, ErrRunNotFound)

	storeErr := errors.New("down")
	_, err = New(&fakeRunStore{err: storeErr}).GetRun(context.Background(), services.GetRunParams{ProjectID: projectID, RunID: runID})

	require.ErrorIs(t, err, storeErr)
}
