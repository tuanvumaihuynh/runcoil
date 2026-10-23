package apierr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithDetailCopiesError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		details map[string]string
	}{
		{name: "nil details"},
		{name: "populated details", details: map[string]string{"task_id": "task-1"}},
		{name: "overwrite", details: map[string]string{"run_id": "run-old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &Error{Kind: KindInvalid, Code: "invalid_run", Message: "invalid run", Details: tc.details}
			wantOriginal := make(map[string]string, len(tc.details))
			for key, value := range tc.details {
				wantOriginal[key] = value
			}

			got := original.WithDetail("run_id", "run-new")

			require.NotSame(t, original, got)
			require.Equal(t, original.Kind, got.Kind)
			require.Equal(t, original.Code, got.Code)
			require.Equal(t, original.Message, got.Message)
			require.Equal(t, "run-new", got.Details["run_id"])
			for key, value := range wantOriginal {
				require.Equal(t, value, original.Details[key])
				if key != "run_id" {
					require.Equal(t, value, got.Details[key])
				}
			}
			if tc.details == nil {
				require.Nil(t, original.Details)
			} else {
				require.Equal(t, wantOriginal, original.Details)
			}

			got.Details["task_id"] = "changed-result"
			require.NotEqual(t, "changed-result", original.Details["task_id"])
			if original.Details != nil {
				original.Details["task_id"] = "changed-original"
				require.Equal(t, "changed-result", got.Details["task_id"])
			}
		})
	}
}

func TestWithDetailChaining(t *testing.T) {
	original := Invalid("invalid_run", "invalid run")
	first := original.WithDetail("task_id", "task-1")
	chained := first.WithDetail("run_id", "run-1")

	require.Nil(t, original.Details)
	require.Equal(t, map[string]string{"task_id": "task-1"}, first.Details)
	require.Equal(t, map[string]string{"task_id": "task-1", "run_id": "run-1"}, chained.Details)
}

func TestWithDetailSiblingsAreIndependent(t *testing.T) {
	original := &Error{Kind: KindInvalid, Details: map[string]string{"task_id": "task-1"}}
	first := original.WithDetail("run_id", "run-1")
	second := original.WithDetail("run_id", "run-2")

	first.Details["task_id"] = "task-2"
	require.Equal(t, map[string]string{"task_id": "task-1"}, original.Details)
	require.Equal(t, map[string]string{"task_id": "task-2", "run_id": "run-1"}, first.Details)
	require.Equal(t, map[string]string{"task_id": "task-1", "run_id": "run-2"}, second.Details)
}
