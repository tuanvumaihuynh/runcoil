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

var (
	apiKeysPath  = "/api/v1/projects/" + testProjectID.String() + "/api-keys"
	testAPIKeyID = uuid.MustParse("019a5b1e-7c2d-7000-8000-4b1d2c3e4f53")
)

type fakeAPIKeyService struct {
	keys []domain.APIKey
	err  error

	created services.CreateAPIKeyParams
	revoked uuid.UUID
}

func (f *fakeAPIKeyService) ListAPIKeys(context.Context, uuid.UUID) ([]domain.APIKey, error) {
	return f.keys, f.err
}

func (f *fakeAPIKeyService) CreateAPIKey(_ context.Context, params services.CreateAPIKeyParams) (services.CreateAPIKeyResult, error) {
	f.created = params

	return services.CreateAPIKeyResult{
		APIKey: domain.APIKey{ID: testAPIKeyID, Name: params.Name, KeyHint: "bQlp", CreatedBy: "ana", CreatedAt: testTime},
		Key:    "rc_api_lUcxC1pN20eawXirnLO0Grrx1i2yG6O6oyilcOTbQlp",
	}, f.err
}

func (f *fakeAPIKeyService) RevokeAPIKey(_ context.Context, params services.RevokeAPIKeyParams) error {
	f.revoked = params.APIKeyID
	return f.err
}

func TestListAPIKeys(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	apiKeyService := &fakeAPIKeyService{keys: []domain.APIKey{
		{ID: testAPIKeyID, Name: "production workers", KeyHint: "bQlp", CreatedBy: "ana", CreatedAt: testTime, LastUsedAt: &testTime, RevokedAt: &testTime},
		{ID: testAPIKeyID, Name: "local", KeyHint: "7xQe", CreatedBy: "ana", CreatedAt: testTime},
	}}
	h := newAPIServerWithAPIKeys(t, accountService, apiKeyService)

	rec := do(t, h, call{method: http.MethodGet, path: apiKeysPath, token: testToken})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"apiKeys":[
		{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","name":"production workers","keyHint":"bQlp","createdBy":"ana","createdAt":"2026-11-14T08:00:00Z","lastUsedAt":"2026-11-14T08:00:00Z","revokedAt":"2026-11-14T08:00:00Z"},
		{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","name":"local","keyHint":"7xQe","createdBy":"ana","createdAt":"2026-11-14T08:00:00Z"}
	]}`, rec.Body.String())
	assert.Equal(t, domain.PermissionAPIKeysRead, accountService.authorizedPermission)
	assert.Equal(t, testProjectID, accountService.authorizedProject)
}

func TestListAPIKeysRefused(t *testing.T) {
	t.Run("not signed in, before any project check", func(t *testing.T) {
		accountService := &fakeAccountService{user: testUser}
		h := newAPIServerWithAPIKeys(t, accountService, &fakeAPIKeyService{})

		rec := do(t, h, call{method: http.MethodGet, path: apiKeysPath})

		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		assert.Empty(t, accountService.authorizedPermission)
	})

	t.Run("no permission in the project", func(t *testing.T) {
		accountService := &fakeAccountService{user: testUser, authorizeErr: apperr.PermissionDenied(apperr.CodePermissionDenied, "permission denied")}
		h := newAPIServerWithAPIKeys(t, accountService, &fakeAPIKeyService{})

		rec := do(t, h, call{method: http.MethodGet, path: apiKeysPath, token: testToken})

		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
}

func TestCreateAPIKey(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	apiKeyService := &fakeAPIKeyService{}
	h := newAPIServerWithAPIKeys(t, accountService, apiKeyService)

	rec := do(t, h, call{method: http.MethodPost, path: apiKeysPath, token: testToken, body: `{"name":"production workers"}`})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"apiKey":{"id":"019a5b1e-7c2d-7000-8000-4b1d2c3e4f53","name":"production workers","keyHint":"bQlp","createdBy":"ana","createdAt":"2026-11-14T08:00:00Z"},
		"key":"rc_api_lUcxC1pN20eawXirnLO0Grrx1i2yG6O6oyilcOTbQlp"
	}`, rec.Body.String())
	assert.Equal(t, domain.PermissionAPIKeysWrite, accountService.authorizedPermission)
	assert.Equal(t, services.CreateAPIKeyParams{ProjectID: testProjectID, Name: "production workers", CreatedBy: testUser.ID}, apiKeyService.created)
}

func TestCreateAPIKeyErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		origin     string
		wantCode   int
		wantError  string
		wantFields []string
	}{
		{name: "empty name", body: `{"name":""}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"name"}},
		{name: "no name", body: `{}`, wantCode: http.StatusBadRequest, wantError: "invalid_request", wantFields: []string{"name"}},
		{name: "another origin", body: `{"name":"ci"}`, origin: "http://evil.test", wantCode: http.StatusForbidden, wantError: "permission_denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKeyService := &fakeAPIKeyService{}
			h := newAPIServerWithAPIKeys(t, &fakeAccountService{user: testUser}, apiKeyService)

			rec := do(t, h, call{method: http.MethodPost, path: apiKeysPath, token: testToken, body: tt.body, origin: tt.origin})

			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			code, fields := decodeError(t, rec)
			assert.Equal(t, tt.wantError, code)
			assert.Equal(t, tt.wantFields, fields)
			assert.Empty(t, apiKeyService.created.Name, "no key is made")
		})
	}
}

func TestRevokeAPIKey(t *testing.T) {
	accountService := &fakeAccountService{user: testUser}
	apiKeyService := &fakeAPIKeyService{}
	h := newAPIServerWithAPIKeys(t, accountService, apiKeyService)
	path := apiKeysPath + "/" + testAPIKeyID.String() + "/revoke"

	rec := do(t, h, call{method: http.MethodPost, path: path, token: testToken})

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, testAPIKeyID, apiKeyService.revoked)
	assert.Equal(t, domain.PermissionAPIKeysWrite, accountService.authorizedPermission)

	apiKeyService.err = apperr.NotFound(apperr.CodeNotFound, "api key not found")

	rec = do(t, h, call{method: http.MethodPost, path: path, token: testToken})

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	code, fields := decodeError(t, rec)
	assert.Equal(t, "not_found", code)
	assert.Empty(t, fields)

	rec = do(t, h, call{method: http.MethodPost, path: apiKeysPath + "/not-a-uuid/revoke", token: testToken})

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	_, fields = decodeError(t, rec)
	assert.Equal(t, []string{"apiKeyId"}, fields)
}
