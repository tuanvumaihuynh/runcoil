package account

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store"
)

type fakeStore struct {
	store.UserStore
	store.ProjectStore

	users    []domain.User
	sessions map[string]domain.User
	roles    []domain.ProjectRole
	projects []domain.Project

	role    *domain.Role
	roleErr error

	createErr error
	updateErr error

	created       []byte
	createdFor    uuid.UUID
	createdWith   string
	deleted       []byte
	passwordHash  string
	replacedHash  string
	keptTokenHash []byte
}

func (f *fakeStore) GetUserByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}

	return domain.User{}, domain.ErrUserNotFound
}

func (f *fakeStore) GetUserByUsername(_ context.Context, username string) (domain.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			return u, nil
		}
	}

	return domain.User{}, domain.ErrUserNotFound
}

func (f *fakeStore) GetUserByEmail(_ context.Context, email string) (domain.User, error) {
	for _, u := range f.users {
		if u.Email != nil && *u.Email == email {
			return u, nil
		}
	}

	return domain.User{}, domain.ErrUserNotFound
}

func (f *fakeStore) CreateSession(_ context.Context, tokenHash []byte, userID uuid.UUID, passwordHash string, _ time.Duration) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.created = tokenHash
	f.createdFor = userID
	f.createdWith = passwordHash

	return nil
}

func (f *fakeStore) GetSessionUserAndTouch(_ context.Context, tokenHash []byte, _ time.Duration) (domain.User, error) {
	user, ok := f.sessions[string(tokenHash)]
	if !ok {
		return domain.User{}, domain.ErrSessionNotFound
	}

	return user, nil
}

func (f *fakeStore) DeleteSession(_ context.Context, tokenHash []byte) error {
	f.deleted = tokenHash
	return nil
}

func (f *fakeStore) UpdatePasswordAndDeleteOtherSessions(_ context.Context, _ uuid.UUID, currentHash, newHash string, keepTokenHash []byte) error {
	if f.updateErr != nil {
		return f.updateErr
	}

	f.replacedHash = currentHash
	f.passwordHash = newHash
	f.keptTokenHash = keepTokenHash

	return nil
}

func (f *fakeStore) ListProjectsWithRoleByUser(context.Context, uuid.UUID) ([]domain.ProjectRole, error) {
	return f.roles, nil
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.Project, error) {
	return f.projects, nil
}

func (f *fakeStore) GetProjectRoleOfUser(context.Context, uuid.UUID, uuid.UUID) (*domain.Role, error) {
	return f.role, f.roleErr
}

var testConfig = Config{
	SessionMaxLifetime:     30 * 24 * time.Hour,
	SessionMaxInactive:     7 * 24 * time.Hour,
	BootstrapAdminUsername: "admin",
	BootstrapAdminPassword: "admin",
}

func newService(store *fakeStore) *Service {
	return New(testConfig, store, store, store)
}

func hashPassword(t *testing.T, password string) string {
	t.Helper()

	hash, err := HashPassword(context.Background(), password)
	require.NoError(t, err)

	return hash
}

func identityOf(user domain.User) domain.Identity {
	return domain.Identity{Type: domain.IdentityUser, UserID: user.ID, SessionID: hashToken("token")}
}

func ptr[T any](v T) *T {
	return &v
}

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()

	appErr, ok := errors.AsType[*apperr.Error](err)
	require.True(t, ok, err)
	assert.Equal(t, apperr.KindUnauthenticated, appErr.Kind)
	assert.Equal(t, apperr.CodeUnauthenticated, appErr.Code)
}

func TestConfigValidate(t *testing.T) {
	assert.NoError(t, testConfig.Validate())
	assert.NoError(t, (&Config{SessionMaxLifetime: time.Hour, SessionMaxInactive: 5 * time.Minute}).Validate())
	assert.EqualError(t, (&Config{SessionMaxInactive: time.Hour}).Validate(), "max lifetime must be positive")
	assert.EqualError(t, (&Config{SessionMaxLifetime: time.Hour}).Validate(), "max inactive must be at least 5m")
	assert.EqualError(t, (&Config{SessionMaxLifetime: time.Hour, SessionMaxInactive: 30 * time.Second}).Validate(), "max inactive must be at least 5m")
}
