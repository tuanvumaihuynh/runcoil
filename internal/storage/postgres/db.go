package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is what the stores need from the database: statements, and transactions
// that run a function atomically.
type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row

	// WithTx runs txFunc in a transaction. It commits when txFunc returns nil
	// and rolls back otherwise. Inside txFunc, WithTx reuses the transaction.
	WithTx(ctx context.Context, txFunc func(DB) error) error
}

var _ DB = (*Client)(nil)

// Client is a DB backed by a connection pool.
type Client struct {
	*pgxpool.Pool
}

// txWrapper is a DB inside a transaction. A nested WithTx joins the transaction.
type txWrapper struct {
	pgx.Tx
}

// pgForeignKeyViolation is the SQLSTATE of a foreign key violation.
const pgForeignKeyViolation = "23503"

// NewClient wraps a pool.
func NewClient(pool *pgxpool.Pool) *Client {
	return &Client{Pool: pool}
}

// WithTx runs txFunc in a new transaction.
func (c *Client) WithTx(ctx context.Context, txFunc func(DB) error) (err error) {
	tx, err := c.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			rbErr := tx.Rollback(ctx)
			if !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, rbErr)
			}
		}
	}()

	if err = txFunc(&txWrapper{Tx: tx}); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		err = fmt.Errorf("commit transaction: %w", err)
	}

	return err
}

func (t *txWrapper) WithTx(_ context.Context, txFunc func(DB) error) error {
	return txFunc(t)
}

// isNoRowsError reports whether err is pgx's "no rows" error.
func isNoRowsError(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// isRerunSourceGone reports whether err is the foreign key violation of a rerun whose
// source Run was removed by retention between the read and the insert.
func isRerunSourceGone(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == pgForeignKeyViolation &&
		pgErr.ConstraintName == "runs_rerun_of_fkey"
}
