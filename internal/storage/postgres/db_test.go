package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRerunSourceIsGoneOnlyForItsForeignKey(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "a foreign key violation on runs_rerun_of_fkey",
			err:  &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: "runs_rerun_of_fkey"},
			want: true,
		},
		{
			name: "the same violation wrapped with %w",
			err:  fmt.Errorf("create run: %w", &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: "runs_rerun_of_fkey"}),
			want: true,
		},
		{
			name: "a foreign key violation on another constraint",
			err:  &pgconn.PgError{Code: pgForeignKeyViolation, ConstraintName: "steps_run_id_fkey"},
			want: false,
		},
		{
			name: "a unique violation on the same constraint name",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "runs_rerun_of_fkey"},
			want: false,
		},
		{
			name: "a plain error",
			err:  errors.New("connection reset"),
			want: false,
		},
		{
			name: "no error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRerunSourceGone(tt.err))
		})
	}
}

func TestNoRowsErrorIsFoundThroughWrapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"pgx no rows", pgx.ErrNoRows, true},
		{"pgx no rows wrapped twice", fmt.Errorf("get run: %w", fmt.Errorf("query: %w", pgx.ErrNoRows)), true},
		{"an error with the same text but not the same value", errors.New("no rows in result set"), false},
		{"a foreign key violation", &pgconn.PgError{Code: pgForeignKeyViolation}, false},
		{"no error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNoRowsError(tt.err))
		})
	}
}

func TestNestedWithTxJoinsTheOpenTransaction(t *testing.T) {
	// The wrapper's transaction is nil here. A nested WithTx only passes itself on, so
	// no method of the transaction is called.
	tx := &txWrapper{}

	var joined DB
	err := tx.WithTx(context.Background(), func(inner DB) error {
		joined = inner
		return nil
	})
	require.NoError(t, err)
	assert.Same(t, tx, joined, "a nested WithTx runs in the transaction it was called on")

	sentinel := errors.New("stop the transaction")
	err = tx.WithTx(context.Background(), func(DB) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel, "an error from the nested function is returned as it is")
}
