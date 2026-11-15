package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

var membersPath = "/api/v1/projects/" + testProjectID.String() + "/members"

func TestListMembers(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	projectService := &fakeProjectService{members: []domain.Member{{UserID: testUser.ID, Username: "ana", Email: new("ana@example.com"), Role: domain.RoleAdmin}}}
	h := newAPIServerWithServices(t, accountService, &fakeUserService{}, projectService)

	rec := do(t, h, call{method: http.MethodGet, path: membersPath, token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"members":[{"userId":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","email":"ana@example.com","role":"admin"}]}`, rec.Body.String())
	assert.Equal(t, domain.PermissionMembersRead, accountService.authorizedPermission)
}

func TestAddMember(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	projectService := &fakeProjectService{}
	h := newAPIServerWithServices(t, accountService, &fakeUserService{}, projectService)

	rec := do(t, h, call{method: http.MethodPost, path: membersPath, token: testToken, body: `{"username":"ana","role":"editor"}`})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"userId":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","role":"editor"}`, rec.Body.String())
	assert.Equal(t, domain.PermissionMembersWrite, accountService.authorizedPermission)
	assert.Equal(t, services.AddMemberParams{ProjectID: testProjectID, Username: "ana", Role: domain.RoleEditor}, projectService.addedMember)

	rec = do(t, h, call{method: http.MethodPost, path: membersPath, token: testToken, body: `{"username":"ana","role":"owner"}`})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	_, fields := decodeError(t, rec)
	assert.Equal(t, []string{"role"}, fields)

	rec = do(t, h, call{method: http.MethodPost, path: membersPath, token: testToken, body: `{}`})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	_, fields = decodeError(t, rec)
	assert.Equal(t, []string{"username", "role"}, fields)

	projectService.err = apperr.Conflict(apperr.CodeAlreadyExists, "user is already a member of the project").WithDetail("field", "username")

	rec = do(t, h, call{method: http.MethodPost, path: membersPath, token: testToken, body: `{"username":"ana","role":"editor"}`})

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	code, fields := decodeError(t, rec)
	assert.Equal(t, "already_exists", code)
	assert.Equal(t, []string{"username"}, fields)
}

func TestUpdateMember(t *testing.T) {
	projectService := &fakeProjectService{}
	h := newAPIServerWithServices(t, &fakeAccountService{user: testUser}, &fakeUserService{}, projectService)

	rec := do(t, h, call{method: http.MethodPatch, path: membersPath + "/" + testUser.ID.String(), token: testToken, body: `{"role":"viewer"}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"userId":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana","role":"viewer"}`, rec.Body.String())
	assert.Equal(t, services.UpdateMemberParams{ProjectID: testProjectID, UserID: testUser.ID, Role: domain.RoleViewer}, projectService.updatedMember)
}

func TestRemoveMember(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	projectService := &fakeProjectService{}
	h := newAPIServerWithServices(t, accountService, &fakeUserService{}, projectService)

	rec := do(t, h, call{method: http.MethodDelete, path: membersPath + "/" + testUser.ID.String(), token: testToken, origin: "http://evil.test"})

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = do(t, h, call{method: http.MethodDelete, path: membersPath + "/" + testUser.ID.String(), token: testToken})

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, testUser.ID, projectService.removedMember)
	assert.Equal(t, domain.PermissionMembersWrite, accountService.authorizedPermission)

	projectService.err = apperr.NotFound(apperr.CodeNotFound, "user is not a member of the project")

	rec = do(t, h, call{method: http.MethodDelete, path: membersPath + "/" + testUser.ID.String(), token: testToken})

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
