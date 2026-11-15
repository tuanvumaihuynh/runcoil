package project

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

func TestListMembers(t *testing.T) {
	members := []domain.Member{{UserID: uuid.NewV7(), Username: "ana", Role: domain.RoleViewer}}

	got, err := newService(&fakeStore{members: members}).ListMembers(context.Background(), uuid.NewV7())

	require.NoError(t, err)
	assert.Equal(t, members, got)
}

func TestAddMember(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana", Email: new("ana@example.com")}
	projectID := uuid.NewV7()

	t.Run("by username, in any case", func(t *testing.T) {
		store := &fakeStore{users: []domain.User{ana}}

		member, err := newService(store).AddMember(context.Background(), services.AddMemberParams{ProjectID: projectID, Username: "Ana", Role: domain.RoleEditor})

		require.NoError(t, err)
		assert.Equal(t, domain.Member{UserID: ana.ID, Username: "ana", Email: ana.Email, Role: domain.RoleEditor}, member)
		assert.Equal(t, domain.AddMemberParams{ProjectID: projectID, UserID: ana.ID, Role: domain.RoleEditor}, store.addedMember)
	})

	t.Run("no such username", func(t *testing.T) {
		_, err := newService(&fakeStore{}).AddMember(context.Background(), services.AddMemberParams{ProjectID: projectID, Username: "bo", Role: domain.RoleEditor})

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "username")
	})

	t.Run("already a member", func(t *testing.T) {
		store := &fakeStore{users: []domain.User{ana}, err: domain.ErrMemberExists}

		_, err := newService(store).AddMember(context.Background(), services.AddMemberParams{ProjectID: projectID, Username: "ana", Role: domain.RoleEditor})

		assertAppError(t, err, apperr.KindConflict, apperr.CodeAlreadyExists, "username")
	})
}

func TestUpdateMember(t *testing.T) {
	params := services.UpdateMemberParams{ProjectID: uuid.NewV7(), UserID: uuid.NewV7(), Role: domain.RoleAdmin}
	store := &fakeStore{}

	member, err := newService(store).UpdateMember(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, domain.Member{UserID: params.UserID, Role: domain.RoleAdmin}, member)
	assert.Equal(t, domain.UpdateMemberRoleParams(params), store.updatedMember)

	_, err = newService(&fakeStore{err: domain.ErrMemberNotFound}).UpdateMember(context.Background(), params)
	assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
}

func TestRemoveMember(t *testing.T) {
	require.NoError(t, newService(&fakeStore{}).RemoveMember(context.Background(), uuid.NewV7(), uuid.NewV7()))

	err := newService(&fakeStore{err: domain.ErrMemberNotFound}).RemoveMember(context.Background(), uuid.NewV7(), uuid.NewV7())
	assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
}
