package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type fakeUserService struct {
	users []domain.User
	err   error

	listedAfter   *uuid.UUID
	listedLimit   int32
	createdUser   services.CreateUserParams
	updatedUser   services.UpdateUserParams
	resetPassword string
}

func (f *fakeUserService) ListUsers(_ context.Context, afterID *uuid.UUID, limit int32) ([]domain.User, error) {
	f.listedAfter = afterID
	f.listedLimit = limit

	return f.users[:min(len(f.users), int(limit))], nil
}

func (f *fakeUserService) CreateUser(_ context.Context, params services.CreateUserParams) (domain.User, error) {
	f.createdUser = params
	return domain.User{ID: testUser.ID, Username: params.Username, Email: params.Email, IsServerAdmin: params.IsServerAdmin, CreatedAt: testTime}, f.err
}

func (f *fakeUserService) UpdateUser(_ context.Context, params services.UpdateUserParams) (domain.User, error) {
	f.updatedUser = params
	return domain.User{ID: params.ID, Username: "ana", DisabledAt: &testTime, CreatedAt: testTime}, f.err
}

func (f *fakeUserService) ResetPassword(_ context.Context, _ uuid.UUID, newPassword string) error {
	f.resetPassword = newPassword
	return f.err
}

func newUserAPI(t *testing.T, user domain.User, userService *fakeUserService) http.Handler {
	t.Helper()

	return newAPIServerWithServices(t, &fakeAccountService{user: user}, userService, &fakeProjectService{})
}

func TestListUsersPages(t *testing.T) {
	users := []domain.User{
		{ID: uuid.NewV7(), Username: "a", CreatedAt: testTime},
		{ID: uuid.NewV7(), Username: "b", CreatedAt: testTime},
		{ID: uuid.NewV7(), Username: "c", CreatedAt: testTime},
	}
	userService := &fakeUserService{users: users}
	h := newUserAPI(t, testAdmin, userService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/users?pageSize=2", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int32(3), userService.listedLimit)
	assert.Nil(t, userService.listedAfter)

	var page struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
		NextPageToken *string `json:"nextPageToken"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Users, 2)
	assert.Equal(t, "b", page.Users[1].Username)
	require.NotNil(t, page.NextPageToken)

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/users?pageToken=" + *page.NextPageToken, token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, &users[1].ID, userService.listedAfter)
	assert.Equal(t, int32(defaultPageSize+1), userService.listedLimit)
	assert.NotContains(t, rec.Body.String(), "nextPageToken")
}

func TestListUsersErrors(t *testing.T) {
	tests := []struct {
		name       string
		user       domain.User
		query      string
		wantCode   int
		wantError  string
		wantFields []string
	}{
		{name: "not a server admin", user: testUser, wantCode: http.StatusForbidden, wantError: "permission_denied"},
		{name: "page size zero", user: testAdmin, query: "?pageSize=0", wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"pageSize"}},
		{name: "page size too large", user: testAdmin, query: "?pageSize=101", wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"pageSize"}},
		{name: "bad page token", user: testAdmin, query: "?pageToken=abc", wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"pageToken"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newUserAPI(t, tt.user, &fakeUserService{})

			rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/users" + tt.query, token: testToken})

			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			code, fields := decodeError(t, rec)
			assert.Equal(t, tt.wantError, code)
			assert.Equal(t, tt.wantFields, fields)
		})
	}
}

func TestCreateUser(t *testing.T) {
	userService := &fakeUserService{}
	h := newUserAPI(t, testAdmin, userService)

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/users", token: testToken, body: `{"username":"ana","email":"ana@example.com","password":"Secret-1","isServerAdmin":false}`})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","email":"ana@example.com","isServerAdmin":false,"createdAt":"2026-11-14T08:00:00Z"}`, rec.Body.String())
	assert.Equal(t, services.CreateUserParams{Username: "ana", Email: new("ana@example.com"), Password: "Secret-1"}, userService.createdUser)

	rec = do(t, h, call{method: http.MethodPost, path: "/api/v1/users", token: testToken, body: `{"username":"ana","password":"Secret-1"}`})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	_, fields := decodeError(t, rec)
	assert.Equal(t, []string{"isServerAdmin"}, fields)
}

func TestUpdateUser(t *testing.T) {
	userService := &fakeUserService{}
	h := newUserAPI(t, testAdmin, userService)
	path := "/api/v1/users/" + testUser.ID.String()

	rec := do(t, h, call{method: http.MethodPatch, path: path, token: testToken, body: `{"disabled":true}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","isServerAdmin":false,"disabledAt":"2026-11-14T08:00:00Z","createdAt":"2026-11-14T08:00:00Z"}`, rec.Body.String())
	assert.Equal(t, services.UpdateUserParams{ID: testUser.ID, Disabled: new(true)}, userService.updatedUser)

	rec = do(t, h, call{method: http.MethodPatch, path: path, token: testToken, body: `{"removeEmail":true}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, services.UpdateUserParams{ID: testUser.ID, RemoveEmail: true}, userService.updatedUser)

	rec = do(t, h, call{method: http.MethodPatch, path: path, token: testToken, body: `{"email":""}`})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	userService.err = apperr.Conflict(apperr.CodeLastServerAdmin, "the last active server admin cannot lose the role or be disabled")

	rec = do(t, h, call{method: http.MethodPatch, path: path, token: testToken, body: `{"isServerAdmin":false}`})

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	code, _ := decodeError(t, rec)
	assert.Equal(t, "last_server_admin", code)
}

func TestResetUserPassword(t *testing.T) {
	userService := &fakeUserService{}
	h := newUserAPI(t, testAdmin, userService)

	rec := do(t, h, call{method: http.MethodPut, path: "/api/v1/users/" + testUser.ID.String() + "/password", token: testToken, body: `{"newPassword":"Secret-1"}`})

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "Secret-1", userService.resetPassword)

	userService.err = apperr.NotFound(apperr.CodeNotFound, "user not found")

	rec = do(t, h, call{method: http.MethodPut, path: "/api/v1/users/" + uuid.NewV7().String() + "/password", token: testToken, body: `{"newPassword":"Secret-1"}`})

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestPageToken(t *testing.T) {
	id := uuid.NewV7()

	got, err := pageTokenToID(idToPageToken(id))
	require.NoError(t, err)
	assert.Equal(t, &id, got)

	got, err = pageTokenToID(nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	for _, token := range []string{"", "!!", "AAAA"} {
		_, err := pageTokenToID(&token)
		assert.Equal(t, errInvalidPageToken, err, token)
	}
}
