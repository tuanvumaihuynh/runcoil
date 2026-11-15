package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/openapi"
)

func loadSpec(t *testing.T, data []byte) *openapi3.T {
	t.Helper()

	spec, err := openapi3.NewLoader().LoadFromData(data)
	require.NoError(t, err)

	return spec
}

func TestEveryOperationHasAPermission(t *testing.T) {
	operations, err := Operations(loadSpec(t, openapi.Spec))
	require.NoError(t, err)

	server := reflect.TypeFor[gen.StrictServerInterface]()
	for i := range server.NumMethod() {
		assert.Contains(t, operations, server.Method(i).Name)
	}

	var public []string
	for name, op := range operations {
		if op.Permission == permissionPublic {
			public = append(public, name)
		}
	}

	assert.ElementsMatch(t, []string{"Login", "Logout", "GetVersion"}, public)
	assert.Equal(t, Operation{Permission: permissionSelf}, operations["GetMe"])
	assert.Equal(t, Operation{Permission: string(domain.PermissionProjectsRead)}, operations["ListProjects"])
	assert.Equal(t, Operation{Permission: string(domain.PermissionMembersRead), InProject: true}, operations["ListMembers"])
}

func TestDomainPermissionsMatchTheSpec(t *testing.T) {
	spec := loadSpec(t, openapi.Spec)

	var want []domain.Permission
	for _, value := range spec.Components.Schemas["Permission"].Value.Enum {
		want = append(want, domain.Permission(value.(string)))
	}

	assert.Equal(t, want, domain.Permissions)
}

func TestOperationsRefusesABadSpec(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		permission string
		want       string
	}{
		{name: "missing", path: "/x", want: "GET /x has no x-runcoil-permission"},
		{name: "unknown", path: "/x", permission: "\n      x-runcoil-permission: runs:delete", want: `GET /x has an unknown permission "runs:delete"`},
		{name: "self under a project", path: "/projects/{projectId}/x", permission: "\n      x-runcoil-permission: self", want: `GET /projects/{projectId}/x is under a project, so it needs a permission, not "self"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := loadSpec(t, []byte(`
openapi: 3.1.0
info:
  title: t
  version: "1"
paths:
  `+tt.path+`:
    get:
      operationId: getX`+tt.permission+`
      responses:
        "204":
          description: ok
`))

			_, err := Operations(spec)

			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestCheckPermission(t *testing.T) {
	operations := map[string]Operation{
		"Login":       {Permission: permissionPublic},
		"GetMe":       {Permission: permissionSelf},
		"CreateUser":  {Permission: string(domain.PermissionUsersWrite)},
		"ListUsers":   {Permission: string(domain.PermissionUsersRead)},
		"ListMembers": {Permission: string(domain.PermissionMembersRead), InProject: true},
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
		{name: "permission refused", operation: "ListUsers", ctx: signedIn, wantKind: apperr.KindPermissionDenied},
		{name: "permission granted", operation: "CreateUser", ctx: admin, allowed: true},
		{name: "in a project, left to AuthorizeProject", operation: "ListMembers", ctx: signedIn, allowed: true},
		{name: "in a project without an identity", operation: "ListMembers", ctx: context.Background(), wantKind: apperr.KindUnauthenticated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, called := recordCall()

			_, err := CheckPermission(operations)(next, tt.operation)(tt.ctx, nil, httptest.NewRequest(http.MethodGet, "/", nil), nil)

			if tt.allowed {
				require.NoError(t, err)
				assert.True(t, *called)

				return
			}

			appErr, ok := errors.AsType[*apperr.Error](err)
			require.True(t, ok, err)
			assert.Equal(t, tt.wantKind, appErr.Kind)
			assert.False(t, *called)
		})
	}
}

func TestCheckPermissionRefusesAnUnknownOperation(t *testing.T) {
	next, called := recordCall()

	_, err := CheckPermission(map[string]Operation{})(next, "Unknown")(context.Background(), nil, httptest.NewRequest(http.MethodGet, "/", nil), nil)

	assert.EqualError(t, err, "operation Unknown has no permission in the spec")
	assert.False(t, *called)
}

func recordCall() (gen.StrictHandlerFunc, *bool) {
	called := false
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		called = true
		return nil, nil
	}

	return next, &called
}
