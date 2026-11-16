package project

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
)

type fakeStore struct {
	store.ProjectStore
	store.UserStore

	users    []domain.User
	projects []domain.Project
	members  []domain.Member
	err      error

	created       domain.Project
	addedMember   store.AddMemberParams
	updatedMember store.UpdateMemberRoleParams
}

func (f *fakeStore) CreateProject(_ context.Context, project domain.Project) (domain.Project, error) {
	f.created = project
	return project, f.err
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.Project, error) {
	return f.projects, nil
}

func (f *fakeStore) ListMembers(context.Context, uuid.UUID) ([]domain.Member, error) {
	return f.members, nil
}

func (f *fakeStore) AddMember(_ context.Context, params store.AddMemberParams) error {
	f.addedMember = params
	return f.err
}

func (f *fakeStore) UpdateMemberRole(_ context.Context, params store.UpdateMemberRoleParams) (domain.Member, error) {
	f.updatedMember = params
	return domain.Member{UserID: params.UserID, Role: params.Role}, f.err
}

func (f *fakeStore) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error {
	return f.err
}

func (f *fakeStore) GetUserByUsername(_ context.Context, username string) (domain.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			return u, nil
		}
	}

	return domain.User{}, domain.ErrUserNotFound
}

func newService(store *fakeStore) *Service {
	return New(store, store)
}

func assertAppError(t *testing.T, err error, kind apperr.Kind, code apperr.Code, field string) {
	t.Helper()

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, kind, appErr.Kind)
	assert.Equal(t, code, appErr.Code)
	assert.Equal(t, field, appErr.Details["field"])
}
