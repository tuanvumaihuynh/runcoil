package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

const (
	permissionExtension = "x-runcoil-permission"
	permissionSelf      = "self"
	permissionPublic    = "public"

	projectIDParam = "projectId"
)

var (
	errNotSignedIn      = apperr.Unauthenticated(apperr.CodeUnauthenticated, "not signed in")
	errPermissionDenied = apperr.PermissionDenied(apperr.CodePermissionDenied, "permission denied")
)

// Operation is what the spec says about who may call an operation.
type Operation struct {
	// Permission is a domain.Permission, "self" for the caller's own account, or "public".
	Permission string
	// InProject is true when the path has {projectId}, so the permission is checked in that
	// project.
	InProject bool
}

// Operations reads every operation of spec, keyed by the operation's Go name. An operation
// with `security: []` is public.
func Operations(spec *openapi3.T) (map[string]Operation, error) {
	operations := make(map[string]Operation)

	for path, item := range spec.Paths.Map() {
		inProject := strings.Contains(path, "{"+projectIDParam+"}")

		for method, op := range item.Operations() {
			permission, err := operationPermission(op)
			if err != nil {
				return nil, fmt.Errorf("%s %s %w", method, path, err)
			}

			if inProject && (permission == permissionPublic || permission == permissionSelf) {
				return nil, fmt.Errorf("%s %s is under a project, so it needs a permission, not %q", method, path, permission)
			}

			operations[goName(op.OperationID)] = Operation{Permission: permission, InProject: inProject}
		}
	}

	if len(operations) == 0 {
		return nil, errors.New("the spec has no operation")
	}

	return operations, nil
}

func operationPermission(op *openapi3.Operation) (string, error) {
	if op.Security != nil && len(*op.Security) == 0 {
		return permissionPublic, nil
	}

	permission, _ := op.Extensions[permissionExtension].(string)
	if permission == "" {
		return "", fmt.Errorf("has no %s", permissionExtension)
	}

	if permission != permissionSelf && !slices.Contains(domain.Permissions, domain.Permission(permission)) {
		return "", fmt.Errorf("has an unknown permission %q", permission)
	}

	return permission, nil
}

// CheckPermission lets an operation run only for a signed-in identity with its
// server-wide permission. An operation under a project is left to AuthorizeProject, which
// runs after it. It refuses an operation that operations does not list.
func CheckPermission(operations map[string]Operation) gen.StrictMiddlewareFunc {
	return func(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		op, ok := operations[operationID]
		if !ok {
			return func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
				return nil, fmt.Errorf("operation %s has no permission in the spec", operationID)
			}
		}

		if op.Permission == permissionPublic {
			return f
		}

		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			identity, ok := IdentityFromContext(ctx)
			if !ok {
				return nil, errNotSignedIn
			}

			if op.Permission == permissionSelf || op.InProject {
				return f(ctx, w, r, req)
			}

			if !slices.Contains(identity.Permissions, domain.Permission(op.Permission)) {
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
