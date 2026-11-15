package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

// Authenticator returns the identity a credential proves.
type Authenticator interface {
	Authenticate(ctx context.Context, credential domain.Credential) (domain.Identity, error)
}

// Authenticate puts the identity of a live session cookie in the context. A request
// without one goes on without an identity, and the permission check answers it. Any other
// failure goes to handleError.
func Authenticate(cookieName string, authenticator Authenticator, handleError func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			credential := domain.Credential{Type: domain.CredentialSession, Secret: cookie.Value}

			identity, err := authenticator.Authenticate(r.Context(), credential)

			if err != nil {
				if appErr, ok := errors.AsType[*apperr.Error](err); ok && appErr.Kind == apperr.KindUnauthenticated {
					next.ServeHTTP(w, r)
					return
				}

				handleError(w, r, err)
				return
			}

			recordUserID(r.Context(), identity.UserID.String())

			ctx := withIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
