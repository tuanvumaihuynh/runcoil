package domain

import "errors"

// The outcomes the user store reports. The services pick what the client sees.
var (
	ErrUserNotFound    = errors.New("user not found")
	ErrPasswordChanged = errors.New("password changed since it was checked")
	ErrUsernameTaken   = errors.New("username is taken")
	ErrEmailTaken      = errors.New("email is taken")
	ErrLastServerAdmin = errors.New("last active server admin")
)

// The outcomes the project store reports.
var (
	ErrProjectNotFound  = errors.New("project not found")
	ErrProjectNameTaken = errors.New("project name is taken")
	ErrMemberNotFound   = errors.New("user is not a member of the project")
	ErrMemberExists     = errors.New("user is already a member of the project")
)

// The outcomes the session store reports.
var (
	ErrSessionNotFound = errors.New("no session for the token")
)

// The outcomes the worker store reports.
var (
	ErrAPIKeyNotFound = errors.New("api key not found")
	ErrSessionEnded   = errors.New("worker session has ended")
	ErrStaleLease     = errors.New("lease is no longer the attempt's")
	ErrStreamFenced   = errors.New("worker's stream is no longer this one")
)
