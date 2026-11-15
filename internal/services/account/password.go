package account

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

const (
	argonMemoryKiB = 19 * 1024
	argonPasses    = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32

	maxArgonMemoryKiB = 256 * 1024
	maxArgonPasses    = 16
	maxArgonThreads   = 16
	maxArgonKeyLen    = 128
)

const minPasswordLen = 8

// MaxPasswordLen is the longest password, in characters.
const MaxPasswordLen = 128

var hashSlots = make(chan struct{}, runtime.GOMAXPROCS(0))

var errMalformedHash = errors.New("malformed password hash")

var errWrongCurrentPassword = apperr.Invalid(apperr.CodeInvalidRequest, "current password is wrong").WithDetail("field", "currentPassword")

// ChangePassword sets the password of the user of identity and ends the user's other
// sessions.
func (s *Service) ChangePassword(ctx context.Context, identity domain.Identity, currentPassword, newPassword string) error {
	user, err := s.userStore.GetUserByID(ctx, identity.UserID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return ErrNotSignedIn
	}

	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	ok, err := VerifyPassword(ctx, user.PasswordHash, currentPassword)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}

	if !ok {
		return errWrongCurrentPassword
	}

	if err := ValidatePassword(newPassword); err != nil {
		return apperr.Invalid(apperr.CodeInvalidRequest, "new password "+err.Error()).WithDetail("field", "newPassword")
	}

	hash, err := HashPassword(ctx, newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	err = s.userStore.UpdatePasswordAndDeleteOtherSessions(ctx, user.ID, user.PasswordHash, hash, identity.SessionID)
	if errors.Is(err, domain.ErrSessionNotFound) || errors.Is(err, domain.ErrUserNotFound) {
		return ErrNotSignedIn
	}

	if errors.Is(err, domain.ErrPasswordChanged) {
		return errWrongCurrentPassword
	}

	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	return nil
}

// ValidatePassword checks password against the password rule.
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length > MaxPasswordLen {
		return fmt.Errorf("must be at most %d characters", MaxPasswordLen)
	}

	var upper, lower, digit, special bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		case !unicode.IsLetter(r):
			special = true
		}
	}

	if length < minPasswordLen || !upper || !lower || !digit || !special {
		return errors.New("must be at least 8 characters with an upper-case letter, a lower-case letter, a digit and a special character")
	}

	return nil
}

// HashPassword hashes a password with argon2id into a PHC string.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	_, _ = rand.Read(salt)

	key, err := idKey(ctx, []byte(password), salt, argonPasses, argonMemoryKiB, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB,
		argonPasses,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches hash, a PHC string from HashPassword.
func VerifyPassword(ctx context.Context, hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}

	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, errMalformedHash
	}

	if memory == 0 || memory > maxArgonMemoryKiB || passes == 0 || passes > maxArgonPasses || threads == 0 || threads > maxArgonThreads {
		return false, errMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > maxArgonKeyLen {
		return false, errMalformedHash
	}

	got, err := idKey(ctx, []byte(password), salt, passes, memory, threads, uint32(len(want))) //nolint:gosec // At most maxArgonKeyLen.
	if err != nil {
		return false, err
	}

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func idKey(ctx context.Context, password, salt []byte, passes, memory uint32, threads uint8, keyLen uint32) ([]byte, error) {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a hash slot: %w", ctx.Err())
	}
	defer func() { <-hashSlots }()

	return argon2.IDKey(password, salt, passes, memory, threads, keyLen), nil
}
