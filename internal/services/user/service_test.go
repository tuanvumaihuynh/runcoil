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
)

type fakeUserStore struct {
	store.UserStore

	users []domain.User
	err   error

	created     domain.User
	listedAfter *uuid.UUID
	listedLimit int32
	updated     domain.UpdateUserParams
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

func (f *fakeUserStore) UpdateUser(_ context.Context, params domain.UpdateUserParams) (domain.User, error) {
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
		{name: "username taken", params: services.CreateUserParams{Username: "ana", Password: "Secret-1"}, storeErr: domain.ErrUsernameTaken, wantKind: apperr.KindConflict, wantCode: apperr.CodeAlreadyExists, wantField: "username"},
		{name: "email taken", params: services.CreateUserParams{Username: "ana", Password: "Secret-1"}, storeErr: domain.ErrEmailTaken, wantKind: apperr.KindConflict, wantCode: apperr.CodeAlreadyExists, wantField: "email"},
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
	after := uuid.NewV7()
	store := &fakeUserStore{users: []domain.User{ana}}

	users, err := New(store).ListUsers(context.Background(), &after, 11)

	require.NoError(t, err)
	assert.Equal(t, []domain.User{ana}, users)
	assert.Equal(t, &after, store.listedAfter)
	assert.Equal(t, int32(11), store.listedLimit)
}

func TestUpdateUser(t *testing.T) {
	id := uuid.NewV7()

	t.Run("lowercases the email", func(t *testing.T) {
		store := &fakeUserStore{}

		user, err := New(store).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("Ana@Example.com"), Disabled: new(true)})

		require.NoError(t, err)
		assert.Equal(t, new("ana@example.com"), user.Email)
		assert.Equal(t, domain.UpdateUserParams{ID: id, Email: new("ana@example.com"), Disabled: new(true)}, store.updated)
	})

	t.Run("removes the email", func(t *testing.T) {
		store := &fakeUserStore{}

		_, err := New(store).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, RemoveEmail: true})

		require.NoError(t, err)
		assert.Equal(t, domain.UpdateUserParams{ID: id, RemoveEmail: true}, store.updated)
	})

	t.Run("an email and its removal", func(t *testing.T) {
		store := &fakeUserStore{}

		_, err := New(store).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("ana@example.com"), RemoveEmail: true})

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "removeEmail")
		assert.Equal(t, domain.UpdateUserParams{}, store.updated)
	})

	t.Run("a bad email", func(t *testing.T) {
		_, err := New(&fakeUserStore{}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Email: new("ana")})

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "email")
	})

	t.Run("the last server admin", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: domain.ErrLastServerAdmin}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, IsServerAdmin: new(false)})

		assertAppError(t, err, apperr.KindConflict, apperr.CodeLastServerAdmin, "")
	})

	t.Run("no user", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: domain.ErrUserNotFound}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Disabled: new(true)})

		assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
	})

	t.Run("a store failure", func(t *testing.T) {
		_, err := New(&fakeUserStore{err: errors.New("connection refused")}).UpdateUser(context.Background(), services.UpdateUserParams{ID: id, Disabled: new(true)})

		assert.EqualError(t, err, "update user: connection refused")
	})
}

func TestResetPassword(t *testing.T) {
	id := uuid.NewV7()

	t.Run("sets the hash", func(t *testing.T) {
		store := &fakeUserStore{}

		require.NoError(t, New(store).ResetPassword(context.Background(), id, "Secret-1"))
		assert.Equal(t, id, store.resetID)

		ok, err := account.VerifyPassword(context.Background(), store.resetHash, "Secret-1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a weak password", func(t *testing.T) {
		store := &fakeUserStore{}

		err := New(store).ResetPassword(context.Background(), id, "secret")

		assertAppError(t, err, apperr.KindInvalid, apperr.CodeInvalidRequest, "newPassword")
		assert.Empty(t, store.resetHash)
	})

	t.Run("no user", func(t *testing.T) {
		err := New(&fakeUserStore{err: domain.ErrUserNotFound}).ResetPassword(context.Background(), id, "Secret-1")

		assertAppError(t, err, apperr.KindNotFound, apperr.CodeNotFound, "")
	})
}
