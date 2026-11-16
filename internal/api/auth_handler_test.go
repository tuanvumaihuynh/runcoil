package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/pkg/healthutil"
)

const testToken = "token-1"

var testProjectID = uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f51")

var testUser = domain.User{ID: uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f50"), Username: "ana"}

type fakeAccountService struct {
	user   domain.User
	access services.Access

	authenticated int

	loginErr    error
	changeErr   error
	loggedOut   []byte
	newPassword string

	authorizeErr         error
	authorizedProject    uuid.UUID
	authorizedPermission domain.Permission
}

var testTime = time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)

func (f *fakeAccountService) Authorize(_ context.Context, params services.AuthorizeParams) error {
	f.authorizedProject = params.ProjectID
	f.authorizedPermission = params.Permission

	return f.authorizeErr
}

func (f *fakeAccountService) Login(_ context.Context, params services.LoginParams) (services.LoginResult, error) {
	if f.loginErr != nil {
		return services.LoginResult{}, f.loginErr
	}

	return services.LoginResult{
		Token:                   testToken,
		ExpiresAt:               time.Date(2026, 12, 13, 0, 0, 0, 0, time.UTC),
		User:                    f.user,
		PasswordChangeSuggested: params.Login == "admin" && params.Password == "admin",
	}, nil
}

func (f *fakeAccountService) Authenticate(_ context.Context, credential domain.Credential) (domain.Identity, error) {
	f.authenticated++

	if credential.Secret != testToken {
		return domain.Identity{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "not signed in")
	}

	permissions := []domain.Permission{}
	if f.user.IsServerAdmin {
		permissions = domain.Permissions
	}

	return domain.Identity{Type: domain.IdentityUser, UserID: f.user.ID, SessionID: []byte(testToken), Permissions: permissions}, nil
}

func (f *fakeAccountService) Logout(_ context.Context, identity domain.Identity) error {
	f.loggedOut = identity.SessionID
	return nil
}

func (f *fakeAccountService) GetUser(context.Context, uuid.UUID) (domain.User, error) {
	return f.user, nil
}

func (f *fakeAccountService) ChangePassword(_ context.Context, params services.ChangePasswordParams) error {
	f.newPassword = params.NewPassword
	return f.changeErr
}

func (f *fakeAccountService) GetUserAccess(context.Context, domain.User) (services.Access, error) {
	return f.access, nil
}

func newAPIServer(t *testing.T, accountService *fakeAccountService) http.Handler {
	t.Helper()

	return newAPIServerWithConfig(t, Config{Port: 8080}, accountService)
}

func newAPIServerWithConfig(t *testing.T, cfg Config, accountService *fakeAccountService) http.Handler {
	t.Helper()

	return newTestServer(t, cfg, testServices{account: accountService})
}

func newAPIServerWithServices(t *testing.T, accountService *fakeAccountService, userService *fakeUserService, projectService *fakeProjectService) http.Handler {
	t.Helper()

	return newTestServer(t, Config{Port: 8080}, testServices{account: accountService, user: userService, project: projectService})
}

func newAPIServerWithAPIKeys(t *testing.T, accountService *fakeAccountService, apiKeyService *fakeAPIKeyService) http.Handler {
	t.Helper()

	return newTestServer(t, Config{Port: 8080}, testServices{account: accountService, apiKey: apiKeyService})
}

// testServices holds the fakes a test server calls. A nil fake is replaced by an empty one.
type testServices struct {
	account *fakeAccountService
	user    *fakeUserService
	project *fakeProjectService
	apiKey  *fakeAPIKeyService
	worker  *fakeWorkerService
	run     *fakeRunService
	task    *fakeTaskService
}

func newTestServer(t *testing.T, cfg Config, svc testServices) http.Handler {
	t.Helper()

	if svc.account == nil {
		svc.account = &fakeAccountService{user: testUser}
	}

	if svc.user == nil {
		svc.user = &fakeUserService{}
	}

	if svc.project == nil {
		svc.project = &fakeProjectService{}
	}

	if svc.apiKey == nil {
		svc.apiKey = &fakeAPIKeyService{}
	}

	if svc.worker == nil {
		svc.worker = &fakeWorkerService{}
	}

	if svc.run == nil {
		svc.run = &fakeRunService{}
	}

	if svc.task == nil {
		svc.task = &fakeTaskService{}
	}

	s, err := New(cfg, slog.New(slog.DiscardHandler), healthutil.New(), svc.account, svc.user, svc.project, svc.apiKey, svc.worker, svc.run, svc.task)
	require.NoError(t, err)

	return s.newHTTPServer().Handler
}

type call struct {
	method string
	path   string
	body   string
	token  string
	origin string
}

func do(t *testing.T, h http.Handler, c call) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	if c.body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	if c.token != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: c.token})
	}

	if c.origin != "" {
		r.Header.Set("Origin", c.origin)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Params  []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"params"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (code string, fields []string) {
	t.Helper()

	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.NotEmpty(t, body.Message, rec.Body.String())

	for _, param := range body.Params {
		fields = append(fields, param.Field)
	}

	return body.Code, fields
}

func TestLogin(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/login", body: `{"login":"admin","password":"admin"}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"user":{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f50","username":"ana"},"passwordChangeSuggested":true}`, rec.Body.String())

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, sessionCookieName, cookies[0].Name)
	assert.Equal(t, testToken, cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	assert.Equal(t, "/", cookies[0].Path)
	assert.False(t, cookies[0].Secure)
}

func TestLoginSetsASecureCookieWhenConfigured(t *testing.T) {
	h := newAPIServerWithConfig(t, Config{Port: 8080, SecureCookie: true}, &fakeAccountService{user: testUser})

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/login", body: `{"login":"admin","password":"admin"}`})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure)
}

func TestLoginErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		loginErr   error
		wantCode   int
		wantError  string
		wantFields []string
	}{
		{name: "wrong password", body: `{"login":"ana","password":"x"}`, loginErr: apperr.Unauthenticated(apperr.CodeUnauthenticated, "wrong login or password"), wantCode: http.StatusUnauthorized, wantError: "unauthenticated"},
		{name: "missing password", body: `{"login":"ana"}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"password"}},
		{name: "missing both", body: `{}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"login", "password"}},
		{name: "empty login", body: `{"login":"","password":"x"}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"login"}},
		{name: "password too long", body: `{"login":"ana","password":"` + strings.Repeat("a", 129) + `"}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"password"}},
		{name: "unknown field", body: `{"login":"ana","password":"x","extra":1}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"extra"}},
		{name: "not JSON", body: `{`, wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{name: "store failure", body: `{"login":"ana","password":"x"}`, loginErr: assert.AnError, wantCode: http.StatusInternalServerError, wantError: "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAPIServer(t, &fakeAccountService{loginErr: tt.loginErr})

			rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/login", body: tt.body})

			assert.Equal(t, tt.wantCode, rec.Code)
			code, fields := decodeError(t, rec)
			assert.Equal(t, tt.wantError, code, rec.Body.String())
			assert.ElementsMatch(t, tt.wantFields, fields, rec.Body.String())
		})
	}
}

func TestLogout(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/logout", token: testToken})

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []byte(testToken), accountService.loggedOut)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, sessionCookieName, cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestCrossOriginWriteIsRefusedBeforeTheSessionLookup(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/logout", token: testToken, origin: "https://evil.example"})

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, accountService.authenticated)
	assert.Nil(t, accountService.loggedOut)
}

func TestLogoutWithoutALiveSession(t *testing.T) {
	for _, token := range []string{"", "ended"} {
		accountService := &fakeAccountService{user: testUser}
		h := newAPIServer(t, accountService)

		rec := do(t, h, call{method: http.MethodPost, path: "/api/v1/auth/logout", token: token})

		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.Nil(t, accountService.loggedOut)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, -1, cookies[0].MaxAge)
	}
}

func TestGetMe(t *testing.T) {
	projectID := uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f51")
	accountService := &fakeAccountService{
		user: testUser,
		access: services.Access{
			Permissions: []domain.Permission{domain.PermissionProjectsCreate},
			Projects: []services.ProjectAccess{{
				ProjectRole: domain.ProjectRole{ProjectID: projectID, ProjectName: "default", Role: domain.RoleViewer},
				Permissions: []domain.Permission{domain.PermissionRunsRead},
			}},
		},
	}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/me", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"user": {"id": "019a5b1e-7c2d-7000-8000-4b1d2c3e4f50", "username": "ana"},
		"permissions": ["projects:create"],
		"projects": [{"id": "019a5b1e-7c2d-7000-8000-4b1d2c3e4f51", "name": "default", "role": "viewer", "permissions": ["runs:read"]}]
	}`, rec.Body.String())
}

func TestGetMeWithNoAccess(t *testing.T) {
	h := newAPIServer(t, &fakeAccountService{user: testUser})

	rec := do(t, h, call{method: http.MethodGet, path: "/api/v1/me", token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"user": {"id": "019a5b1e-7c2d-7000-8000-4b1d2c3e4f50", "username": "ana"}, "permissions": [], "projects": []}`, rec.Body.String())
}

func TestChangeMyPassword(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodPut, path: "/api/v1/me/password", token: testToken, body: `{"currentPassword":"admin","newPassword":"Secret-123"}`})

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "Secret-123", accountService.newPassword)
}

func TestChangeMyPasswordRefused(t *testing.T) {
	accountService := &fakeAccountService{
		user:      testUser,
		changeErr: apperr.Invalid(apperr.CodeInvalidRequest, "current password is wrong"),
	}
	h := newAPIServer(t, accountService)

	rec := do(t, h, call{method: http.MethodPut, path: "/api/v1/me/password", token: testToken, body: `{"currentPassword":"x","newPassword":"Secret-123"}`})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"code": "invalid_request", "message": "current password is wrong"}`, rec.Body.String())

	accountService.changeErr = apperr.Invalid(apperr.CodeInvalidRequest, "new password must be at least 8 characters").WithDetail("field", "newPassword")

	rec = do(t, h, call{method: http.MethodPut, path: "/api/v1/me/password", token: testToken, body: `{"currentPassword":"x","newPassword":"Secret-123"}`})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{
		"code": "invalid_request",
		"message": "new password must be at least 8 characters",
		"params": [{"field": "newPassword", "message": "new password must be at least 8 characters"}]
	}`, rec.Body.String())
}

func TestRequestsRefusedBeforeTheHandler(t *testing.T) {
	tests := []struct {
		name      string
		call      call
		wantCode  int
		wantError string
	}{
		{name: "no session", call: call{method: http.MethodGet, path: "/api/v1/me"}, wantCode: http.StatusUnauthorized, wantError: "unauthenticated"},
		{name: "ended session", call: call{method: http.MethodGet, path: "/api/v1/me", token: "old"}, wantCode: http.StatusUnauthorized, wantError: "unauthenticated"},
		{name: "cross-origin write", call: call{method: http.MethodPost, path: "/api/v1/auth/logout", token: testToken, origin: "https://evil.example"}, wantCode: http.StatusForbidden, wantError: "permission_denied"},
		{name: "unknown path", call: call{method: http.MethodGet, path: "/api/v1/nothing", token: testToken}, wantCode: http.StatusNotFound, wantError: "not_found"},
		{name: "wrong method", call: call{method: http.MethodDelete, path: "/api/v1/me", token: testToken}, wantCode: http.StatusMethodNotAllowed, wantError: "invalid_request"},
		{name: "body too large", call: call{method: http.MethodPost, path: "/api/v1/auth/login", body: `{"login":"` + strings.Repeat("a", maxBodyBytes) + `","password":"x"}`}, wantCode: http.StatusRequestEntityTooLarge, wantError: "payload_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAPIServer(t, &fakeAccountService{user: testUser})

			rec := do(t, h, tt.call)

			assert.Equal(t, tt.wantCode, rec.Code)
			code, _ := decodeError(t, rec)
			assert.Equal(t, tt.wantError, code, rec.Body.String())
		})
	}
}

func TestHandlersRefuseAContextWithoutAnIdentity(t *testing.T) {
	h := newAuthHandler(&fakeAccountService{user: testUser}, false)
	ctx := context.Background()

	_, err := h.GetMe(ctx, gen.GetMeRequestObject{})
	assert.Error(t, err)

	_, err = h.ChangeMyPassword(ctx, gen.ChangeMyPasswordRequestObject{Body: &gen.ChangeMyPasswordJSONRequestBody{}})
	assert.Error(t, err)
}
