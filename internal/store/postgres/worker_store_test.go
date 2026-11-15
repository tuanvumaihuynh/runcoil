package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/runcoil/runcoil/internal/domain"
)

func TestByTaskName(t *testing.T) {
	tasks := []domain.TaskDefinition{{Name: "refund"}, {Name: "charge"}, {Name: "ship"}}

	assert.Equal(t, []int{1, 0, 2}, byTaskName(tasks))
	assert.Empty(t, byTaskName(nil))
}
