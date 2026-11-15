package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"uuid"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

const (
	permissionExtension = "x-runcoil-permission"
	permissionSelf      = "self"
	permissionPublic    = "public"
)

var (
	errNotSignedIn      = apperr.Unauthenticated(apperr.CodeUnauthenticated, "not signed in")
	errPermissionDenied = apperr.PermissionDenied(apperr.CodePermissionDenied, "permission denied")
)

// Permissions reads the permission of every operation in spec, keyed by the operation's
// Go name. An operation with `security: []` is public.
func Permissions(spec *openapi3.T) (map[string]string, error) {
	permissions := make(map[string]string)

	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.Security != nil && len(*op.Security) == 0 {
				permissions[goName(op.OperationID)] = permissionPublic
				continue
			}

			permission, _ := op.Extensions[permissionExtension].(string)
			if permission == "" {
				return nil, fmt.Errorf("%s %s has no %s", method, path, permissionExtension)
			}

			if permission != permissionSelf && !slices.Contains(domain.Permissions, domain.Permission(permission)) {
				return nil, fmt.Errorf("%s %s has an unknown permission %q", method, path, permission)
			}

			permissions[goName(op.OperationID)] = permission
		}
	}

	if len(permissions) == 0 {
		return nil, errors.New("the spec has no operation")
	}

	return permissions, nil
}

// Authorizer checks a permission inside a project.
type Authorizer interface {
	Authorize(ctx context.Context, params services.AuthorizeParams) error
}

// CheckPermission lets an operation run only for an identity with its permission. The
// permission of an operation under a project is checked in that project by authorizer.
// It refuses an operation that permissions does not list.
func CheckPermission(permissions map[string]string, authorizer Authorizer) gen.StrictMiddlewareFunc {
	return func(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		permission, ok := permissions[operationID]
		if !ok {
			return func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
				return nil, fmt.Errorf("operation %s has no permission in the spec", operationID)
			}
		}

		if permission == permissionPublic {
			return f
		}

		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			identity, ok := IdentityFromContext(ctx)
			if !ok {
				return nil, errNotSignedIn
			}

			if permission == permissionSelf {
				return f(ctx, w, r, req)
			}

			if projectID := r.PathValue("projectId"); projectID != "" {
				id, err := uuid.Parse(projectID)
				if err != nil {
					return nil, fmt.Errorf("parse project id: %w", err)
				}

				err = authorizer.Authorize(ctx, services.AuthorizeParams{
					Identity:   identity,
					ProjectID:  id,
					Permission: domain.Permission(permission),
				})
				if err != nil {
					return nil, fmt.Errorf("authorize: %w", err)
				}

				return f(ctx, w, r, req)
			}

			if !slices.Contains(identity.Permissions, domain.Permission(permission)) {
				return nil, errPermissionDenied
			}

			return f(ctx, w, r, req)
		}
	}
}

func goName(operationID string) string {
	if operationID == "" {
		return ""
	}

	return strings.ToUpper(operationID[:1]) + operationID[1:]
}
