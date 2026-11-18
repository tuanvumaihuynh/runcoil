package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

const sessionTokenLen = 32

// Login checks a username or an email and its password, and starts a session.
func (s *Service) Login(ctx context.Context, params services.LoginParams) (services.LoginResult, error) {
	login := strings.ToLower(params.Login)

	var user domain.User
	var err error
	if strings.Contains(login, "@") {
		user, err = s.userStore.GetUserByEmail(ctx, login)
	} else {
		user, err = s.userStore.GetUserByUsername(ctx, login)
	}

	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			_, _ = VerifyPassword(ctx, s.dummyHash, params.Password)
			return services.LoginResult{}, ErrWrongLogin
		}

		return services.LoginResult{}, fmt.Errorf("get user: %w", err)
	}

	ok, err := VerifyPassword(ctx, user.PasswordHash, params.Password)
	if err != nil {
		return services.LoginResult{}, fmt.Errorf("verify password: %w", err)
	}

	if !ok || user.DisabledAt != nil {
		return services.LoginResult{}, ErrWrongLogin
	}

	token := newSessionToken()

	err = s.sessionStore.CreateSession(ctx, hashToken(token), user.ID, user.PasswordHash, s.cfg.SessionMaxLifetime)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return services.LoginResult{}, ErrWrongLogin
		}

		return services.LoginResult{}, fmt.Errorf("create session: %w", err)
	}

	return services.LoginResult{
		Token:                   token,
		ExpiresAt:               s.now().Add(s.cfg.SessionMaxLifetime),
		User:                    user,
		PasswordChangeSuggested: user.Username == s.cfg.BootstrapAdminUsername && params.Password == s.cfg.BootstrapAdminPassword,
	}, nil
}

// Authenticate returns the identity a credential proves, and records the activity of its
// session.
func (s *Service) Authenticate(ctx context.Context, credential domain.Credential) (domain.Identity, error) {
	if credential.Type != domain.CredentialSession {
		return domain.Identity{}, fmt.Errorf("unknown credential type %q", credential.Type)
	}

	sessionID := hashToken(credential.Secret)

	user, err := s.sessionStore.GetSessionUserAndTouch(ctx, sessionID, s.cfg.SessionMaxInactive)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return domain.Identity{}, ErrNotSignedIn
		}

		return domain.Identity{}, fmt.Errorf("get session user: %w", err)
	}

	return domain.Identity{
		Type:        domain.IdentityUser,
		UserID:      user.ID,
		SessionID:   sessionID,
		Permissions: userPermissions(user),
	}, nil
}

// Logout ends the session of identity.
func (s *Service) Logout(ctx context.Context, identity domain.Identity) error {
	if err := s.sessionStore.DeleteSession(ctx, identity.SessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

func newSessionToken() string {
	b := make([]byte, sessionTokenLen)
	_, _ = rand.Read(b)

	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
