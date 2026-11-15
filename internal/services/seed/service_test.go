package seed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

func TestValidate(t *testing.T) {
	const badUsername = "admin username must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-'"

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "defaults", cfg: Config{AdminUsername: "admin", AdminPassword: "admin"}},
		{name: "bad username", cfg: Config{AdminUsername: "Admin", AdminPassword: "admin"}, want: badUsername},
		{name: "empty password", cfg: Config{AdminUsername: "admin"}, want: "admin password must not be empty"},
		{name: "password too long", cfg: Config{AdminUsername: "admin", AdminPassword: strings.Repeat("a", 129)}, want: "admin password must be at most 128 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == "" {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, tt.want)
		})
	}
}

type fakeUserStore struct {
	store.UserStore

	created bool
	err     error

	admin            domain.User
	defaultProjectID uuid.UUID
}

func (s *fakeUserStore) CreateFirstServerAdmin(_ context.Context, admin domain.User, defaultProjectID uuid.UUID) (bool, error) {
	s.admin = admin
	s.defaultProjectID = defaultProjectID

	return s.created, s.err
}

func TestSeed(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	store := &fakeUserStore{created: true}
	service := New(Config{AdminUsername: "root", AdminPassword: "s3cret!"}, logger, store)

	require.NoError(t, service.Seed(t.Context()))

	assert.Equal(t, "root", store.admin.Username)
	assert.Equal(t, byte(0x70), store.admin.ID[6]&0xf0)
	assert.Equal(t, byte(0x70), store.defaultProjectID[6]&0xf0)
	assert.True(t, strings.HasPrefix(store.admin.PasswordHash, "$argon2id$"))
	assert.Contains(t, logs.String(), "username=root")
	assert.NotContains(t, logs.String(), "s3cret!")
}

func TestSeedWhenUsersExist(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	service := New(Config{AdminUsername: "admin", AdminPassword: "admin"}, logger, &fakeUserStore{})

	require.NoError(t, service.Seed(t.Context()))

	assert.Empty(t, logs.String())
}

func TestSeedStoreError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &fakeUserStore{err: errors.New("connection refused")}
	service := New(Config{AdminUsername: "admin", AdminPassword: "admin"}, logger, store)

	assert.EqualError(t, service.Seed(t.Context()), "create first server admin: connection refused")
}
