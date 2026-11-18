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
)

var testAdmin = domain.User{ID: testUser.ID, Username: "admin", IsServerAdmin: true}

type fakeProjectService struct {
	projects []domain.Project
	members  []domain.Member
	err      error

	addedMember   services.AddMemberParams
	updatedMember services.UpdateMemberParams
	removedMember uuid.UUID
}

func (f *fakeProjectService) ListProjects(context.Context) ([]domain.Project, error) {
	return f.projects, nil
}

func (f *fakeProjectService) CreateProject(_ context.Context, name string) (domain.Project, error) {
	return domain.Project{ID: testProjectID, Name: name, CreatedAt: testTime}, f.err
}

func (f *fakeProjectService) ListMembers(context.Context, uuid.UUID) ([]domain.Member, error) {
	return f.members, nil
}

func (f *fakeProjectService) AddMember(_ context.Context, params services.AddMemberParams) (domain.Member, error) {
	f.addedMember = params
	return domain.Member{UserID: testUser.ID, Username: params.Username, Role: params.Role}, f.err
}

func (f *fakeProjectService) UpdateMember(_ context.Context, params services.UpdateMemberParams) (domain.Member, error) {
	f.updatedMember = params
	return domain.Member{UserID: params.UserID, Username: "ana", Role: params.Role}, f.err
}

func (f *fakeProjectService) RemoveMember(_ context.Context, params services.RemoveMemberParams) error {
	f.removedMember = params.UserID
	return f.err
}

func TestListProjects(t *testing.T) {
	projectService := &fakeProjectService{projects: []domain.Project{{ID: testProjectID, Name: "default", CreatedAt: testTime}}}
	h := newAPIServerWithServices(t, &fakeAccountService{user: testAdmin}, &fakeUserService{}, projectService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/projects", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"projects":[{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f51","name":"default","createdAt":"2026-11-14T08:00:00Z"}]}`, rec.Body.String())

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/projects"})

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	h = newAPIServerWithServices(t, &fakeAccountService{user: testUser}, &fakeUserService{}, projectService)

	rec = do(t, h, call{method: http.MethodGet, path: "/api/v1/projects", token: testToken})

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestCreateProject(t *testing.T) {
	h := newAPIServer(t, &fakeAccountService{user: testAdmin})

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/projects", token: testToken, body: `{"name":"billing"}`})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f51","name":"billing","createdAt":"2026-11-14T08:00:00Z"}`, rec.Body.String())
}

func TestCreateProjectErrors(t *testing.T) {
	tests := []struct {
		name       string
		user       domain.User
		body       string
		err        error
		wantCode   int
		wantError  string
		wantFields []string
	}{
		{name: "not a server admin", user: testUser, body: `{"name":"billing"}`, wantCode: http.StatusForbidden, wantError: "permission_denied"},
		{name: "empty name", user: testAdmin, body: `{"name":""}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"name"}},
		{name: "name taken", user: testAdmin, body: `{"name":"billing"}`, err: apperr.Conflict(apperr.CodeAlreadyExists, "project name is taken").WithDetail("field", "name"), wantCode: http.StatusConflict, wantError: "already_exists", wantFields: []string{"name"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAPIServerWithServices(t, &fakeAccountService{user: tt.user}, &fakeUserService{}, &fakeProjectService{err: tt.err})

			rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/projects", token: testToken, body: tt.body})

			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			code, fields := decodeError(t, rec)
			assert.Equal(t, tt.wantError, code)
			assert.Equal(t, tt.wantFields, fields)
		})
	}
}

func TestProjectOperationsAskTheAuthorizer(t *testing.T) {
	accountService := &fakeAccountService{user: testUser, authorizeErr: apperr.NotFound(apperr.CodeNotFound, "project not found")}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/projects/" + testProjectID.String() + "/members", token: testToken})

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	code, _ := decodeError(t, rec)
	assert.Equal(t, "not_found", code)
	assert.Equal(t, testProjectID, accountService.authorizedProject)
	assert.Equal(t, domain.PermissionMembersRead, accountService.authorizedPermission)
}

func TestMalformedPathIDs(t *testing.T) {
	h := newAPIServer(t, &fakeAccountService{user: testAdmin})
	member := "/api/v1/projects/" + testProjectID.String() + "/members/" + testUser.ID.String()

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		wantField string
	}{
		{name: "project of listMembers", method: http.MethodGet, path: "/api/v1/projects/x/members", wantField: "projectId"},
		{name: "user of removeMember", method: http.MethodDelete, path: "/api/v1/projects/" + testProjectID.String() + "/members/x", wantField: "userId"},
		{name: "project of updateMember", method: http.MethodPut, path: "/api/v1/projects/x/members/" + testUser.ID.String(), body: `{"role":"admin"}`, wantField: "projectId"},
		{name: "user of updateUser", method: http.MethodPut, path: "/api/v1/users/x", body: `{"email":null,"isServerAdmin":false,"disabled":true}`, wantField: "userId"},
		{name: "a valid ID is not refused", method: http.MethodDelete, path: member},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, call{method: tt.method, path: tt.path, token: testToken, body: tt.body})

			if tt.wantField == "" {
				require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
				return
			}

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.JSONEq(t, `{"code":"invalid_request","message":"`+tt.wantField+` is not valid","params":[{"field":"`+tt.wantField+`","message":"`+tt.wantField+` is not valid"}]}`, rec.Body.String())
		})
	}
}
