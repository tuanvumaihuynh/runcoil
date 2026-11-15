package middleware

import (
	"context"
	"fmt"
	"net/http"
	"uuid"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

// Authorizer checks a permission inside a project.
type Authorizer interface {
	Authorize(ctx context.Context, params services.AuthorizeParams) error
}

// AuthorizeProject lets an operation under /projects/{projectId} run only for a caller
// whose role in that project holds the operation's permission. It runs after
// CheckPermission, which refuses a caller who is not signed in.
func AuthorizeProject(operations map[string]Operation, authorizer Authorizer) gen.StrictMiddlewareFunc {
	return func(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		op := operations[operationID]
		if !op.InProject {
			return f
		}

		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			identity, ok := IdentityFromContext(ctx)
			if !ok {
				return nil, errNotSignedIn
			}

			// A strict middleware gets the request as any, so it reads the ID from the path.
			// The generated wrapper has already refused a malformed projectId with 400, so this
			// parse fails only on a bug.
			projectID, err := uuid.Parse(r.PathValue(projectIDParam))
			if err != nil {
				return nil, fmt.Errorf("parse project id: %w", err)
			}

			err = authorizer.Authorize(ctx, services.AuthorizeParams{
				Identity:   identity,
				ProjectID:  projectID,
				Permission: domain.Permission(op.Permission),
			})
			if err != nil {
				return nil, fmt.Errorf("authorize: %w", err)
			}

			return f(ctx, w, r, req)
		}
	}
}
