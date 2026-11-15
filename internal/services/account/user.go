package account

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"uuid"

	"github.com/runcoil/runcoil/internal/domain"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

// ValidateUsername checks that name is a valid username.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return errors.New("must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-'")
	}

	return nil
}

// GetUser returns the user with id.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	user, err := s.userStore.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

const maxEmailLen = 254

// ValidateEmail checks that email is a plain address such as ana@example.com.
func ValidateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" || len(email) > maxEmailLen {
		return errors.New("must be a plain address such as ana@example.com")
	}

	return nil
}
