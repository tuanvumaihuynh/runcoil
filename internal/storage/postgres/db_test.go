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

// recordingDB is a DB that keeps the last call it got, with the SQL and the arguments.
type recordingDB struct {
	method string
	sql    string
	args   []any
}

func (r *recordingDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.record("exec", sql, args)
	return pgconn.CommandTag{}, nil
}

func (r *recordingDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.record("query", sql, args)
	return nil, nil
}

func (r *recordingDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	r.record("queryrow", sql, args)
	return nil
}

func (r *recordingDB) WithTx(context.Context, func(DB) error) error {
	return errors.New("not used by planPerCallDB")
}

func (r *recordingDB) record(method, sql string, args []any) {
	r.method, r.sql, r.args = method, sql, args
}

func TestPlanPerCallDBAddsTheExecModeOnlyToQuery(t *testing.T) {
	ctx := context.Background()
	db := &recordingDB{}
	wrapped := planPerCallDB{db: db}

	wrapped.Query(ctx, "SELECT 1", "w3", "t13", int32(50))
	assert.Equal(t, "query", db.method)
	assert.Equal(t, "SELECT 1", db.sql)
	assert.Equal(t, []any{pgx.QueryExecModeExec, "w3", "t13", int32(50)}, db.args,
		"Query passes the exec mode first, then the arguments in order")

	wrapped.Query(ctx, "SELECT 2")
	assert.Equal(t, []any{pgx.QueryExecModeExec}, db.args, "a query without arguments gets the mode alone")

	wrapped.Exec(ctx, "UPDATE runs", "a", 1)
	assert.Equal(t, "exec", db.method)
	assert.Equal(t, []any{"a", 1}, db.args, "Exec passes the arguments unchanged")

	wrapped.QueryRow(ctx, "SELECT 3", "b")
	assert.Equal(t, "queryrow", db.method)
	assert.Equal(t, []any{"b"}, db.args, "QueryRow passes the arguments unchanged")
}
