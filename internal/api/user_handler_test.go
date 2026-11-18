package api

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/pkg/paging"
)

type fakeUserService struct {
	users         []domain.User
	nextPageToken string
	err           error

	listed        paging.Params
	lookedUp      paging.Params
	createdUser   services.CreateUserParams
	updatedUser   services.UpdateUserParams
	resetPassword string
}

func (f *fakeUserService) ListUsers(_ context.Context, params paging.Params) (paging.List[domain.User], error) {
	f.listed = params
	if f.err != nil {
		return paging.List[domain.User]{}, f.err
	}

	return paging.List[domain.User]{Items: f.users, NextPageToken: f.nextPageToken}, nil
}

func (f *fakeUserService) LookupUsers(_ context.Context, params paging.Params) (paging.List[domain.User], error) {
	f.lookedUp = params
	if f.err != nil {
		return paging.List[domain.User]{}, f.err
	}

	return paging.List[domain.User]{Items: f.users, NextPageToken: f.nextPageToken}, nil
}

func (f *fakeUserService) CreateUser(_ context.Context, params services.CreateUserParams) (domain.User, error) {
	f.createdUser = params
	return domain.User{ID: testUser.ID, Username: params.Username, Email: params.Email, IsServerAdmin: params.IsServerAdmin, CreatedAt: testTime}, f.err
}

func (f *fakeUserService) UpdateUser(_ context.Context, params services.UpdateUserParams) (domain.User, error) {
	f.updatedUser = params
	return domain.User{ID: params.ID, Username: "ana", DisabledAt: &testTime, CreatedAt: testTime}, f.err
}

func (f *fakeUserService) ResetPassword(_ context.Context, params services.ResetPasswordParams) error {
	f.resetPassword = params.NewPassword
	return f.err
}

func newUserAPI(t *testing.T, user domain.User, userService *fakeUserService) http.Handler {
	t.Helper()

	return newAPIServerWithServices(t, &fakeAccountService{user: user}, userService, &fakeProjectService{})
}

func TestListUsersPages(t *testing.T) {
	userService := &fakeUserService{
		users:         []domain.User{{ID: uuid.NewV7(), Username: "a", CreatedAt: testTime}},
		nextPageToken: "next",
	}
	h := newUserAPI(t, testAdmin, userService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/users?pageSize=1&pageToken=abc", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, paging.Params{PageSize: 1, PageToken: "abc"}, userService.listed)
	assert.Contains(t, rec.Body.String(), `"username":"a"`)
	assert.Contains(t, rec.Body.String(), `"nextPageToken":"next"`)

	userService.nextPageToken = ""

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/users", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, paging.Params{PageSize: paging.DefaultPageSize}, userService.listed, "the spec gives the default page size")
	assert.NotContains(t, rec.Body.String(), "nextPageToken")

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/users?pageToken=", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, userService.listed.PageToken, "an empty page token asks for the first page")
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

func TestLookupUsers(t *testing.T) {
	userService := &fakeUserService{
		users: []domain.User{{
			ID:            testAdmin.ID,
			Username:      "ana",
			Email:         new("ana@example.com"),
			PasswordHash:  "hash",
			IsServerAdmin: true,
			CreatedAt:     testTime,
		}},
		nextPageToken: "next",
	}
	h := newUserAPI(t, testUser, userService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/users/lookup?pageSize=1&pageToken=abc", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, paging.Params{PageSize: 1, PageToken: "abc"}, userService.lookedUp)
	assert.JSONEq(t, `{"users":[{"id":"`+testAdmin.ID.String()+`","username":"ana","email":"ana@example.com"}],"nextPageToken":"next"}`, rec.Body.String(),
		"a user who is not a server admin may look up users, and sees only usernames and emails")

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/users/lookup"})

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/users/lookup?pageSize=101", token: testToken})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	_, fields := decodeError(t, rec)
	assert.Equal(t, []string{"pageSize"}, fields)
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

	rec := do(t, h, call{method: http.MethodPut, path: path, token: testToken, body: `{"email":"ana@example.com","isServerAdmin":true,"disabled":true}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","isServerAdmin":false,"disabledAt":"2026-11-14T08:00:00Z","createdAt":"2026-11-14T08:00:00Z"}`, rec.Body.String())
	assert.Equal(t, services.UpdateUserParams{ID: testUser.ID, Email: new("ana@example.com"), IsServerAdmin: true, Disabled: true}, userService.updatedUser)

	rec = do(t, h, call{method: http.MethodPut, path: path, token: testToken, body: `{"email":null,"isServerAdmin":false,"disabled":false}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, services.UpdateUserParams{ID: testUser.ID}, userService.updatedUser)

	userService.err = apperr.Conflict(apperr.CodeLastServerAdmin, "the last active server admin cannot lose the role or be disabled")

	rec = do(t, h, call{method: http.MethodPut, path: path, token: testToken, body: `{"email":null,"isServerAdmin":false,"disabled":false}`})

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	code, _ := decodeError(t, rec)
	assert.Equal(t, "last_server_admin", code)
}

func TestUpdateUserInvalid(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantFields []string
	}{
		{name: "a missing field", body: `{"isServerAdmin":false,"disabled":false}`, wantFields: []string{"email"}},
		{name: "every field missing", body: `{}`, wantFields: []string{"email", "isServerAdmin", "disabled"}},
		{name: "an empty email", body: `{"email":"","isServerAdmin":false,"disabled":false}`, wantFields: []string{"email"}},
		{name: "a null flag", body: `{"email":null,"isServerAdmin":null,"disabled":false}`, wantFields: []string{"isServerAdmin"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userService := &fakeUserService{}
			h := newUserAPI(t, testAdmin, userService)

			rec := do(t, h, call{method: http.MethodPut, path: "/api/v1/users/" + testUser.ID.String(), token: testToken, body: tt.body})

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			_, fields := decodeError(t, rec)
			assert.ElementsMatch(t, tt.wantFields, fields, rec.Body.String())
			assert.Equal(t, services.UpdateUserParams{}, userService.updatedUser)
		})
	}
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
