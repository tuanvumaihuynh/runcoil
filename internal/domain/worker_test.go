package domain

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationValidate(t *testing.T) {
	task := func(name string) TaskDefinition { return TaskDefinition{Name: name} }

	tests := []struct {
		name string
		reg  Registration
		want *RegistrationError
	}{
		{name: "valid", reg: Registration{App: "shop", Slots: 1, Tasks: []TaskDefinition{task("a")}}},
		{name: "no slots", reg: Registration{App: "shop", Tasks: []TaskDefinition{task("a")}}, want: &RegistrationError{Message: "slots must be between 1 and 100"}},
		{name: "most slots", reg: Registration{App: "shop", Slots: MaxSlots, Tasks: []TaskDefinition{task("a")}}},
		{name: "too many slots", reg: Registration{App: "shop", Slots: MaxSlots + 1, Tasks: []TaskDefinition{task("a")}}, want: &RegistrationError{Message: "slots must be between 1 and 100"}},
		{name: "no tasks", reg: Registration{App: "shop", Slots: 1}, want: &RegistrationError{Message: "a registration needs at least one task"}},
		{
			name: "task twice",
			reg:  Registration{App: "shop", Slots: 1, Tasks: []TaskDefinition{task("a"), task("a")}},
			want: &RegistrationError{Message: "task is declared twice", TaskName: "a"},
		},
		{
			name: "NUL in the app",
			reg:  Registration{App: "sh\x00op", Slots: 1, Tasks: []TaskDefinition{task("a")}},
			want: &RegistrationError{Message: "app name must not contain a NUL byte"},
		},
		{
			name: "NUL in a task",
			reg:  Registration{App: "shop", Slots: 1, Tasks: []TaskDefinition{task("a"), task("b\x00")}},
			want: &RegistrationError{Message: "task name must not contain a NUL byte", TaskName: "b\x00"},
		},
		{name: "most tasks", reg: Registration{App: "shop", Slots: 1, Tasks: tasks(MaxTasks)}},
		{
			name: "too many tasks",
			reg:  Registration{App: "shop", Slots: 1, Tasks: tasks(MaxTasks + 1)},
			want: &RegistrationError{Message: "a registration declares at most 1000 tasks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reg.Validate()
			if tt.want == nil {
				assert.NoError(t, err)
				return
			}

			regErr, ok := AsRegistrationError(err)
			require.True(t, ok)
			assert.Equal(t, tt.want, regErr)
		})
	}
}

func tasks(n int) []TaskDefinition {
	defs := make([]TaskDefinition, 0, n)
	for i := range n {
		defs = append(defs, TaskDefinition{Name: fmt.Sprintf("task-%d", i)})
	}

	return defs
}

func TestNewWorkerSession(t *testing.T) {
	reg := Registration{App: "shop", Slots: 2, Tasks: []TaskDefinition{{Name: "a"}, {Name: "b"}}}

	session, err := NewWorkerSession(reg, []byte("hash"))
	require.NoError(t, err)

	assert.NotEqual(t, [16]byte{}, [16]byte(session.ID))
	assert.Equal(t, []byte("hash"), session.TokenHash)
	assert.Equal(t, reg, session.Registration)
	require.Len(t, session.TaskIDs, 2)
	assert.NotEqual(t, session.TaskIDs[0], session.TaskIDs[1])
}

func TestNewWorkerSessionValidates(t *testing.T) {
	_, err := NewWorkerSession(Registration{App: "shop", Tasks: []TaskDefinition{{Name: "a"}}}, nil)

	regErr, ok := AsRegistrationError(err)
	require.True(t, ok)
	assert.Equal(t, "slots must be between 1 and 100", regErr.Message)
}

func TestRegistrationValidatesTaskSettings(t *testing.T) {
	zero := time.Duration(0)

	tests := []struct {
		name string
		def  TaskDefinition
		want string
	}{
		{name: "too many attempts", def: TaskDefinition{Name: "a", Retry: &RetryPolicy{MaxAttempts: 31}}, want: "retry policy: max attempts must be between 0 and 30"},
		{name: "zero start deadline", def: TaskDefinition{Name: "a", StartDeadline: &zero}, want: "start deadline must be greater than 0"},
		{name: "bad cron", def: TaskDefinition{Name: "a", Schedule: &Schedule{Cron: "0 0 30 2 *", Overlap: OverlapAllow}}, want: "schedule: cron never fires"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Registration{App: "shop", Slots: 1, Tasks: []TaskDefinition{tt.def}}.Validate()

			regErr, ok := AsRegistrationError(err)
			require.True(t, ok)
			assert.Equal(t, tt.want, regErr.Message)
			assert.Equal(t, "a", regErr.TaskName)
		})
	}
}
