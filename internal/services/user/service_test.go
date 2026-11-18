package user

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
	"github.com/runcoil/runcoil/internal/services/account"
	"github.com/runcoil/runcoil/internal/store"
	"github.com/runcoil/runcoil/pkg/paging"
)

type fakeUserStore struct {
	store.UserStore

	users []domain.User
	err   error

	created     domain.User
	listedAfter *uuid.UUID
	listedLimit int32
	activeOnly  bool
	updated     store.UpdateUserParams
	resetID     uuid.UUID
	resetHash   string
}

func (f *fakeUserStore) CreateUser(_ context.Context, user domain.User) (domain.User, error) {
	f.created = user
	return user, f.err
}

func (f *fakeUserStore) ListUsers(_ context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error) {
	f.listedAfter = afterID
	f.listedLimit = limit

	return f.users, nil
}

func (f *fakeUserStore) ListActiveUsers(_ context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error) {
	f.activeOnly = true
	f.listedAfter = afterID
	f.listedLimit = limit

	return f.users, f.err
}

func (f *fakeUserStore) UpdateUser(_ context.Context, params store.UpdateUserParams) (domain.User, error) {
	f.updated = params
	return domain.User{ID: params.ID, Email: params.Email}, f.err
}

func (f *fakeUserStore) SetPasswordAndDeleteSessions(_ context.Context, id uuid.UUID, passwordHash string) error {
	f.resetID = id
	f.resetHash = passwordHash

	return f.err
}

func assertAppError(t *testing.T, err error, kind apperr.Kind, code apperr.Code, field string) {
	t.Helper()

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, kind, appErr.Kind)
	assert.Equal(t, code, appErr.Code)
	assert.Equal(t, field, appErr.Details["field"])
}

func TestCreateUser(t *testing.T) {
	store := &fakeUserStore{}

	user, err := New(store).CreateUser(context.Background(), services.CreateUserParams{
		Username:      "Ana",
		Email:         new("Ana@Example.com"),
		Password:      "Secret-1",
		IsServerAdmin: true,
	})

	require.NoError(t, err)
	assert.Equal(t, store.created, user)
	assert.Equal(t, "ana", user.Username)
	assert.Equal(t, new("ana@example.com"), user.Email)
	assert.True(t, user.IsServerAdmin)
	assert.NotEqual(t, uuid.Nil(), user.ID)

	ok, err := account.VerifyPassword(context.Background(), user.PasswordHash, "Secret-1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestCreateUserRefused(t *testing.T) {
	tests := []struct {
		name      string
		params    services.CreateUserParams
		storeErr  error
		wantKind  apperr.Kind
		wantCode  apperr.Code
		wantField string
	}{
		{name: "bad username", params: services.CreateUserParams{Username: "ana!", Password: "Secret-1"}, wantKind: apperr.KindInvalid, wantCode: apperr.CodeInvalidRequest, wantField: "username"},
		{name: "bad email", params: services.CreateUserParams{Username: "ana", Email: new("ana"), Password: "Secret-1"}, wantKind: apperr.KindInvalid, wantCode: apperr.CodeInvalidRequest, wantField: "email"},
		{name: "weak password", params: services.CreateUserParams{Username: "ana", Password: "secret"}, wantKind: apperr.KindInvalid, wantCode: apperr.CodeInvalidRequest, wantField: "password"},
		{name: "username taken", params: services.CreateUserParams{Username: "ana", Password: "Secret-1"}, storeErr: domain.ErrUsernameTaken, wantKind: apperr.KindConflict, wantCode: apperr.CodeAlreadyExists},
		{name: "email taken", params: services.CreateUserParams{Username: "ana", Password: "Secret-1"}, storeErr: domain.ErrEmailTaken, wantKind: apperr.KindConflict, wantCode: apperr.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(&fakeUserStore{err: tt.storeErr}).CreateUser(context.Background(), tt.params)

			assertAppError(t, err, tt.wantKind, tt.wantCode, tt.wantField)
		})
	}
}

func TestListUsers(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana"}
	bob := domain.User{ID: uuid.NewV7(), Username: "bob"}
	userStore := &fakeUserStore{users: []domain.User{ana, bob}}

	first, err := New(userStore).ListUsers(context.Background(), paging.Params{PageSize: 1})

	require.NoError(t, err)
	assert.Equal(t, []domain.User{ana}, first.Items)
	assert.Nil(t, userStore.listedAfter)
	assert.Equal(t, int32(2), userStore.listedLimit)
	require.NotEmpty(t, first.NextPageToken)

	userStore.users = []domain.User{bob}

	second, err := New(userStore).ListUsers(context.Background(), paging.Params{PageToken: first.NextPageToken})

	require.NoError(t, err)
	assert.Equal(t, []domain.User{bob}, second.Items)
	assert.Equal(t, &ana.ID, userStore.listedAfter)
	assert.Equal(t, int32(paging.DefaultPageSize+1), userStore.listedLimit)
	assert.Empty(t, second.NextPageToken)
}

func TestListUsersBadPageToken(t *testing.T) {
	userStore := &fakeUserStore{}

	_, err := New(userStore).ListUsers(context.Background(), paging.Params{PageToken: "abc"})

	assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "pageToken")
	assert.Zero(t, userStore.listedLimit)
}

func TestLookupUsers(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana"}
	bob := domain.User{ID: uuid.NewV7(), Username: "bob"}
	userStore := &fakeUserStore{users: []domain.User{ana, bob}}

	first, err := New(userStore).LookupUsers(context.Background(), paging.Params{PageSize: 1})

	require.NoError(t, err)
	assert.True(t, userStore.activeOnly, "a lookup lists only users who can sign in")
	assert.Equal(t, []domain.User{ana}, first.Items)
	assert.Nil(t, userStore.listedAfter)
	assert.Equal(t, int32(2), userStore.listedLimit)
	require.NotEmpty(t, first.NextPageToken)

	userStore.users = []domain.User{bob}

	second, err := New(userStore).LookupUsers(context.Background(), paging.Params{PageToken: first.NextPageToken})

	require.NoError(t, err)
	assert.Equal(t, []domain.User{bob}, second.Items)
	assert.Equal(t, &ana.ID, userStore.listedAfter)
	assert.Empty(t, second.NextPageToken)

	_, err = New(userStore).LookupUsers(context.Background(), paging.Params{PageToken: "!"})

	assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "pageToken")
}

func TestUpdateUser(t *testing.T) {
	id := uuid.NewV7()

	t.Run("lowercases the email", func(t *testing.T) {
		userStore := &fakeUserStore{}

		user, err := New(userStore).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("Ana@Example.com"), Disabled: true})

		require.NoError(t, err)
		assert.Equal(t, new("ana@example.com"), user.Email)
		assert.Equal(t, store.UpdateUserParams{ID: id, Email: new("ana@example.com"), Disabled: true}, userStore.updated)
	})

	t.Run("removes the email", func(t *testing.T) {
		userStore := &fakeUserStore{}

		_, err := New(userStore).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, IsServerAdmin: true})

		require.NoError(t, err)
		assert.Equal(t, store.UpdateUserParams{ID: id, IsServerAdmin: true}, userStore.updated)
	})

	t.Run("a bad email", func(t *testing.T) {
		userStore := &fakeUserStore{}

		_, err := New(userStore).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("ana")})

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "email")
		assert.Equal(t, store.UpdateUserParams{}, userStore.updated)
	})

	t.Run("the email of another user", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: domain.ErrEmailTaken}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("ana@example.com")})

		assertAppError(t, err, apperr.KindConflict, apperr.CodeAlreadyExists, "")
	})

	t.Run("the last server admin", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: domain.ErrLastServerAdmin}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id})

		assertAppError(t, err, apperr.KindConflict, apperr.CodeLastServerAdmin, "")
	})

	t.Run("no user", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: domain.ErrUserNotFound}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Disabled: true})

		assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
	})

	t.Run("a store failure", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: errors.New("connection refused")}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Disabled: true})

		assert.EqualError(t, err, "update user: connection refused")
	})
}

func TestResetPassword(t *testing.T) {
	id := uuid.NewV7()

	t.Run("sets the hash", func(t *testing.T) {
		store := &fakeUserStore{}

		require.NoError(t, New(store).ResetPassword(context.Background(), services.ResetPasswordParams{ID: id, NewPassword: "Secret-1"}))
		assert.Equal(t, id, store.resetID)

		ok, err := account.VerifyPassword(context.Background(), store.resetHash, "Secret-1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a weak password", func(t *testing.T) {
		store := &fakeUserStore{}

		err := New(store).ResetPassword(context.Background(), services.ResetPasswordParams{ID: id, NewPassword: "secret"})

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "newPassword")
		assert.Empty(t, store.resetHash)
	})

	t.Run("no user", func(t *testing.T) {
		err := New(&fakeUserStore{err: domain.ErrUserNotFound}).ResetPassword(context.Background(), services.ResetPasswordParams{ID: id, NewPassword: "Secret-1"})

		assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
	})
}
