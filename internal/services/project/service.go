package project

import (
	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/internal/store"
)

// The errors the service returns to clients.
var (
	ErrProjectNameTaken = apperr.Conflict(apperr.CodeAlreadyExists, "project name is taken")
	ErrMemberNotFound   = apperr.NotFound(apperr.CodeNotFound, "user is not a member of the project")
	ErrMemberExists     = apperr.Conflict(apperr.CodeAlreadyExists, "user is already a member of the project")
	ErrNoSuchUsername   = apperr.NotFound(apperr.CodeNotFound, "no user has this username")
)

var _ services.ProjectService = (*Service)(nil)

// Service manages the projects and their members.
type Service struct {
	projectStore store.ProjectStore
	userStore    store.UserStore
}

// New creates the project service.
func New(projectStore store.ProjectStore, userStore store.UserStore) *Service {
	return &Service{projectStore: projectStore, userStore: userStore}
}
