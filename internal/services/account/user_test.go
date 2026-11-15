package account

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/domain"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{name: "every allowed character", username: "ops.admin_1-a"},
		{name: "longest", username: strings.Repeat("a", 64)},
		{name: "empty", username: "", wantErr: true},
		{name: "too long", username: strings.Repeat("a", 65), wantErr: true},
		{name: "upper case", username: "Admin", wantErr: true},
		{name: "email", username: "admin@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, "must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-'")
		})
	}
}

func TestGetUser(t *testing.T) {
	ana := domain.User{ID: uuid.NewV7(), Username: "ana"}
	s := newService(&fakeStore{users: []domain.User{ana}})

	user, err := s.GetUser(context.Background(), ana.ID)
	require.NoError(t, err)
	assert.Equal(t, ana, user)

	_, err = s.GetUser(context.Background(), uuid.NewV7())
	assert.ErrorIs(t, err, domain.ErrUserNotFound)
}
