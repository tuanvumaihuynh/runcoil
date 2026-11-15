package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"uuid"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/openapi"
)

func loadSpec(t *testing.T, data []byte) *openapi3.T {
	t.Helper()

	spec, err := openapi3.NewLoader().LoadFromData(data)
	require.NoError(t, err)

	return spec
}

func TestEveryOperationHasAPermission(t *testing.T) {
	permissions, err := Permissions(loadSpec(t, openapi.Spec))
	require.NoError(t, err)

	operations := reflect.TypeFor[gen.StrictServerInterface]()
	for i := range operations.NumMethod() {
		assert.Contains(t, permissions, operations.Method(i).Name)
	}

	var public []string
	for operation, permission := range permissions {
		if permission == permissionPublic {
			public = append(public, operation)
		}
	}

	assert.ElementsMatch(t, []string{"Login", "Logout", "GetVersion"}, public)
	assert.Equal(t, permissionSelf, permissions["GetMe"])
}

func TestDomainPermissionsMatchTheSpec(t *testing.T) {
	spec := loadSpec(t, openapi.Spec)

	var want []domain.Permission
	for _, value := range spec.Components.Schemas["Permission"].Value.Enum {
		want = append(want, domain.Permission(value.(string)))
	}

	assert.Equal(t, want, domain.Permissions)
}

func TestPermissionsRefusesABadSpec(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		want       string
	}{
		{name: "missing", want: "GET /x has no x-runcoil-permission"},
		{name: "unknown", permission: "\n      x-runcoil-permission: runs:delete", want: `GET /x has an unknown permission "runs:delete"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := loadSpec(t, []byte(`
openapi: 3.1.0
info:
  title: t
  version: "1"
paths:
  /x:
    get:
      operationId: getX`+tt.permission+`
      responses:
        "204":
          description: ok
`))

			_, err := Permissions(spec)

			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestCheckPermission(t *testing.T) {
	permissions := map[string]string{
		"Login":       permissionPublic,
		"GetMe":       permissionSelf,
		"CreateUser":  string(domain.PermissionUsersWrite),
		"ListMembers": string(domain.PermissionMembersRead),
	}
	signedIn := withIdentity(context.Background(), domain.Identity{Permissions: []domain.Permission{}})
	admin := withIdentity(context.Background(), domain.Identity{Permissions: domain.Permissions})

	tests := []struct {
		name      string
		operation string
		ctx       context.Context
		allowed   bool
		wantKind  apperr.Kind
	}{
		{name: "public without an identity", operation: "Login", ctx: context.Background(), allowed: true},
		{name: "self without an identity", operation: "GetMe", ctx: context.Background(), wantKind: apperr.KindUnauthenticated},
		{name: "self signed in", operation: "GetMe", ctx: signedIn, allowed: true},
		{name: "permission without an identity", operation: "CreateUser", ctx: context.Background(), wantKind: apperr.KindUnauthenticated},
		{name: "permission refused", operation: "ListMembers", ctx: signedIn, wantKind: apperr.KindPermissionDenied},
		{name: "permission granted", operation: "CreateUser", ctx: admin, allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
				called = true
				return nil, nil
			}

			_, err := CheckPermission(permissions, &fakeAuthorizer{})(next, tt.operation)(tt.ctx, nil, httptest.NewRequest(http.MethodGet, "/", nil), nil)

			if tt.allowed {
				require.NoError(t, err)
				assert.True(t, called)

				return
			}

			appErr, ok := errors.AsType[*apperr.Error](err)
			require.True(t, ok, err)
			assert.Equal(t, tt.wantKind, appErr.Kind)
			assert.False(t, called)
		})
	}
}

func TestCheckPermissionRefusesAnUnknownOperation(t *testing.T) {
	called := false
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		called = true
		return nil, nil
	}

	_, err := CheckPermission(map[string]string{}, &fakeAuthorizer{})(next, "Unknown")(context.Background(), nil, httptest.NewRequest(http.MethodGet, "/", nil), nil)

	assert.EqualError(t, err, "operation Unknown has no permission in the spec")
	assert.False(t, called)
}

type fakeAuthorizer struct {
	err error

	projectID  uuid.UUID
	permission domain.Permission
}

func (f *fakeAuthorizer) Authorize(_ context.Context, params services.AuthorizeParams) error {
	f.projectID = params.ProjectID
	f.permission = params.Permission

	return f.err
}

func TestCheckPermissionInAProject(t *testing.T) {
	permissions := map[string]string{"ListMembers": string(domain.PermissionMembersRead)}
	signedIn := withIdentity(context.Background(), domain.Identity{Permissions: []domain.Permission{}})
	projectID := uuid.NewV7()

	request := func(projectID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("projectId", projectID)

		return r
	}

	t.Run("asks the authorizer", func(t *testing.T) {
		authorizer := &fakeAuthorizer{}
		called := false
		next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
			called = true
			return nil, nil
		}

		_, err := CheckPermission(permissions, authorizer)(next, "ListMembers")(signedIn, nil, request(projectID.String()), nil)

		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, projectID, authorizer.projectID)
		assert.Equal(t, domain.PermissionMembersRead, authorizer.permission)
	})

	t.Run("returns the authorizer's refusal", func(t *testing.T) {
		refusal := apperr.NotFound(apperr.CodeNotFound, "project not found")
		called := false
		next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
			called = true
			return nil, nil
		}

		_, err := CheckPermission(permissions, &fakeAuthorizer{err: refusal})(next, "ListMembers")(signedIn, nil, request(projectID.String()), nil)

		assert.ErrorIs(t, err, refusal)
		assert.False(t, called)
	})

	t.Run("fails on a project ID that is not a UUID", func(t *testing.T) {
		next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
			return nil, nil
		}

		_, err := CheckPermission(permissions, &fakeAuthorizer{})(next, "ListMembers")(signedIn, nil, request("x"), nil)

		assert.ErrorContains(t, err, "parse project id")
	})
}
