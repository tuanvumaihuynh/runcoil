package account

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

func TestChangePassword(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana", PasswordHash: hashPassword(t, "admin")}
	store := &fakeStore{users: []domain.User{ana}}

	err := newService(store).ChangePassword(context.Background(), identityOf(ana), "admin", "Secret-1")

	require.NoError(t, err)
	assert.Equal(t, hashToken("token"), store.keptTokenHash)
	assert.Equal(t, ana.PasswordHash, store.replacedHash)

	ok, err := VerifyPassword(context.Background(), store.passwordHash, "Secret-1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChangePasswordRefused(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana", PasswordHash: hashPassword(t, "admin")}

	tests := []struct {
		name      string
		current   string
		new       string
		wantField string
	}{
		{name: "wrong current password", current: "Admin", new: "Secret-1", wantField: "currentPassword"},
		{name: "weak new password", current: "admin", new: "secret", wantField: "newPassword"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{users: []domain.User{ana}}

			err := newService(store).ChangePassword(context.Background(), identityOf(ana), tt.current, tt.new)

			appErr, ok := errors.AsType[*apperr.Error](err)
			require.True(t, ok, err)
			assert.Equal(t, apperr.KindInvalid, appErr.Kind)
			assert.Equal(t, apperr.CodeInvalidRequest, appErr.Code)
			assert.Equal(t, tt.wantField, appErr.Details["field"])
			assert.Empty(t, store.passwordHash)
		})
	}
}

func TestChangePasswordWhenTheSessionEnded(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana", PasswordHash: hashPassword(t, "admin")}

	for _, updateErr := range []error{domain.ErrSessionNotFound, domain.ErrUserNotFound} {
		store := &fakeStore{users: []domain.User{ana}, updateErr: updateErr}

		err := newService(store).ChangePassword(context.Background(), identityOf(ana), "admin", "Secret-1")

		assert.ErrorIs(t, err, ErrNotSignedIn, updateErr)
	}

	err := newService(&fakeStore{}).ChangePassword(context.Background(), identityOf(ana), "admin", "Secret-1")
	assert.ErrorIs(t, err, ErrNotSignedIn)
}

func TestChangePasswordWhenThePasswordChangedMeanwhile(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana", PasswordHash: hashPassword(t, "admin")}
	store := &fakeStore{users: []domain.User{ana}, updateErr: domain.ErrPasswordChanged}

	err := newService(store).ChangePassword(context.Background(), identityOf(ana), "admin", "Secret-1")

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, apperr.KindInvalid, appErr.Kind)
	assert.Equal(t, "currentPassword", appErr.Details["field"])
}

func TestHashPassword(t *testing.T) {
	hash := hashPassword(t, "admin")

	parts := strings.Split(hash, "$")
	require.Len(t, parts, 6)
	assert.Equal(t, "argon2id", parts[1])
	assert.Equal(t, "v=19", parts[2])
	assert.Equal(t, "m=19456,t=2,p=1", parts[3])

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	require.NoError(t, err)
	assert.Len(t, salt, 16)

	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	require.NoError(t, err)
	assert.Equal(t, argon2.IDKey([]byte("admin"), salt, 2, 19456, 1, 32), key)
	assert.NotEqual(t, argon2.IDKey([]byte("Admin"), salt, 2, 19456, 1, 32), key)
	assert.NotEqual(t, hash, hashPassword(t, "admin"))
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "every class", password: "Secret-1"},
		{name: "unicode letters", password: "Mật-khẩu1"},
		{name: "too short", password: "Sec-1ab", wantErr: true},
		{name: "no upper case", password: "secret-12", wantErr: true},
		{name: "no lower case", password: "SECRET-12", wantErr: true},
		{name: "no digit", password: "Secret-ab", wantErr: true},
		{name: "no special", password: "Secret123", wantErr: true},
		{name: "longest", password: "Secret-1" + strings.Repeat("a", 120)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, "must be at least 8 characters with an upper-case letter, a lower-case letter, a digit and a special character")
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	hash := hashPassword(t, "Secret-1")

	ok, err := VerifyPassword(context.Background(), hash, "Secret-1")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword(context.Background(), hash, "secret-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerifyPasswordWithOtherParameters(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("admin"), salt, 3, 8*1024, 2, 16)
	hash := "$argon2id$v=19$m=8192,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)

	ok, err := VerifyPassword(context.Background(), hash, "admin")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyPasswordMalformed(t *testing.T) {
	for _, hash := range []string{
		"",
		"plain",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=x,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$!!$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",
		"$argon2id$v=19$m=19456,t=0,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=0$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=4294967295,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=17,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=17$c2FsdA$a2V5",
	} {
		_, err := VerifyPassword(context.Background(), hash, "admin")
		assert.ErrorIs(t, err, errMalformedHash, hash)
	}
}

func TestValidatePasswordTooLong(t *testing.T) {
	err := ValidatePassword("Secret-1" + strings.Repeat("a", 121))

	assert.EqualError(t, err, "must be at most 128 characters")
}

func TestHashPasswordStopsWaitingWhenCanceled(t *testing.T) {
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}

	defer func() {
		for range cap(hashSlots) {
			<-hashSlots
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := HashPassword(ctx, "admin")

	assert.ErrorIs(t, err, context.Canceled)
}
