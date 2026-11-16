package task

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

type fakeTaskStore struct {
	tasks []domain.TaskSummary
	task  domain.TaskDetail
	err   error

	listed store.ListTasksParams
	gotIDs [2]uuid.UUID
}

func (f *fakeTaskStore) ListTasks(_ context.Context, params store.ListTasksParams) ([]domain.TaskSummary, error) {
	f.listed = params
	return f.tasks, f.err
}

func (f *fakeTaskStore) GetTaskDetail(_ context.Context, projectID, taskID uuid.UUID) (domain.TaskDetail, error) {
	f.gotIDs = [2]uuid.UUID{projectID, taskID}
	return f.task, f.err
}

func TestListTasks(t *testing.T) {
	projectID := uuid.NewV7()
	tasks := []domain.TaskSummary{{ID: uuid.NewV7()}, {ID: uuid.NewV7()}, {ID: uuid.NewV7()}}
	taskStore := &fakeTaskStore{tasks: tasks}

	page, err := New(taskStore).ListTasks(context.Background(), projectID, paging.Params{PageSize: 2})
	require.NoError(t, err)

	assert.Equal(t, tasks[:2], page.Items)
	assert.Equal(t, store.ListTasksParams{ProjectID: projectID, Limit: 3}, taskStore.listed)

	_, err = New(taskStore).ListTasks(context.Background(), projectID, paging.Params{PageSize: 2, PageToken: page.NextPageToken})
	require.NoError(t, err)

	assert.Equal(t, &tasks[1].ID, taskStore.listed.AfterID)
}

func TestListTasksRefused(t *testing.T) {
	_, err := New(&fakeTaskStore{}).ListTasks(context.Background(), uuid.NewV7(), paging.Params{PageToken: "not a token"})

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, apperr.KindInvalid, appErr.Kind)
	assert.Equal(t, "pageToken", appErr.Details["field"])

	storeErr := errors.New("down")
	_, err = New(&fakeTaskStore{err: storeErr}).ListTasks(context.Background(), uuid.NewV7(), paging.Params{})

	require.ErrorIs(t, err, storeErr)
}

func TestGetTask(t *testing.T) {
	projectID, taskID := uuid.NewV7(), uuid.NewV7()
	taskStore := &fakeTaskStore{task: domain.TaskDetail{TaskSummary: domain.TaskSummary{ID: taskID}}}

	task, err := New(taskStore).GetTask(context.Background(), projectID, taskID)
	require.NoError(t, err)

	assert.Equal(t, taskID, task.ID)
	assert.Equal(t, [2]uuid.UUID{projectID, taskID}, taskStore.gotIDs)

	_, err = New(&fakeTaskStore{err: domain.ErrTaskNotFound}).GetTask(context.Background(), projectID, taskID)

	require.ErrorIs(t, err, ErrTaskNotFound)

	storeErr := errors.New("down")
	_, err = New(&fakeTaskStore{err: storeErr}).GetTask(context.Background(), projectID, taskID)

	require.ErrorIs(t, err, storeErr)
}
