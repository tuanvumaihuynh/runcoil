package project

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

func TestListProjects(t *testing.T) {
	every := []domain.Project{{ID: uuid.NewV7(), Name: "a"}, {ID: uuid.NewV7(), Name: "b"}}
	mine := every[1:]
	s := newService(&fakeStore{projects: every, mine: mine})

	projects, err := s.ListProjects(context.Background(), domain.Identity{UserID: uuid.NewV7(), Permissions: []domain.Permission{}})
	require.NoError(t, err)
	assert.Equal(t, mine, projects)

	projects, err = s.ListProjects(context.Background(), domain.Identity{UserID: uuid.NewV7(), Permissions: domain.Permissions})
	require.NoError(t, err)
	assert.Equal(t, every, projects)
}

func TestCreateProject(t *testing.T) {
	store := &fakeStore{}

	project, err := newService(store).CreateProject(context.Background(), "billing")

	require.NoError(t, err)
	assert.Equal(t, "billing", project.Name)
	assert.NotEqual(t, uuid.Nil(), project.ID)
	assert.Equal(t, store.created, project)

	_, err = newService(&fakeStore{err: domain.ErrProjectNameTaken}).CreateProject(context.Background(), "billing")
	assertAppError(t, err, apperr.KindConflict, apperr.CodeAlreadyExists, "name")
}
