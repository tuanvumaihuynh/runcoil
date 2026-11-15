package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type fakeAuthorizer struct {
	err error

	called     bool
	projectID  uuid.UUID
	permission domain.Permission
}

func (f *fakeAuthorizer) Authorize(_ context.Context, params services.AuthorizeParams) error {
	f.called = true
	f.projectID = params.ProjectID
	f.permission = params.Permission

	return f.err
}

func TestAuthorizeProject(t *testing.T) {
	operations := map[string]Operation{
		"ListMembers": {Permission: string(domain.PermissionMembersRead), InProject: true},
		"ListUsers":   {Permission: string(domain.PermissionUsersRead)},
	}
	signedIn := withIdentity(context.Background(), domain.Identity{Permissions: []domain.Permission{}})
	projectID := uuid.NewV7()

	request := func(projectID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue(projectIDParam, projectID)

		return r
	}

	t.Run("asks the authorizer", func(t *testing.T) {
		authorizer := &fakeAuthorizer{}
		next, called := recordCall()

		_, err := AuthorizeProject(operations, authorizer)(next, "ListMembers")(signedIn, nil, request(projectID.String()), nil)

		require.NoError(t, err)
		assert.True(t, *called)
		assert.Equal(t, projectID, authorizer.projectID)
		assert.Equal(t, domain.PermissionMembersRead, authorizer.permission)
	})

	t.Run("returns the authorizer's refusal", func(t *testing.T) {
		refusal := apperr.NotFound(apperr.CodeNotFound, "project not found")
		next, called := recordCall()

		_, err := AuthorizeProject(operations, &fakeAuthorizer{err: refusal})(next, "ListMembers")(signedIn, nil, request(projectID.String()), nil)

		assert.ErrorIs(t, err, refusal)
		assert.False(t, *called)
	})

	t.Run("leaves an operation outside a project alone", func(t *testing.T) {
		authorizer := &fakeAuthorizer{}
		next, called := recordCall()

		_, err := AuthorizeProject(operations, authorizer)(next, "ListUsers")(signedIn, nil, request(""), nil)

		require.NoError(t, err)
		assert.True(t, *called)
		assert.False(t, authorizer.called)
	})

	t.Run("refuses a caller who is not signed in", func(t *testing.T) {
		authorizer := &fakeAuthorizer{}
		next, called := recordCall()

		_, err := AuthorizeProject(operations, authorizer)(next, "ListMembers")(context.Background(), nil, request(projectID.String()), nil)

		appErr, ok := errors.AsType[*apperr.Error](err)
		require.True(t, ok, err)
		assert.Equal(t, apperr.KindUnauthenticated, appErr.Kind)
		assert.False(t, *called)
		assert.False(t, authorizer.called)
	})

	t.Run("fails on a project ID that is not a UUID", func(t *testing.T) {
		next, called := recordCall()

		_, err := AuthorizeProject(operations, &fakeAuthorizer{})(next, "ListMembers")(signedIn, nil, request("x"), nil)

		assert.ErrorContains(t, err, "parse project id")
		assert.False(t, *called)
	})
}
