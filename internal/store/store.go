// Package store is api's database: its schema, the migrations that make it, the
// pool requests take their connections from, and the transaction a write under an
// account runs in (R29).
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The refusals of a write under an account.
var (
	// ErrNoAccount is a write under an account that is not there.
	ErrNoAccount = errors.New("store: no account")
	// ErrAccountDeleting is a write under an account marked "deleting" (R21).
	ErrAccountDeleting = errors.New("store: the account is being deleted")
)

// errConnectionString is a connection string pgx cannot read. pgx's own error
// would repeat the string, which holds the password, so it is not passed on.
var errConnectionString = errors.New("store: the connection string does not parse")

// Open is a pool of connections to the database at url. The pool takes them as
// it needs them, and pgx keeps its prepared statements by the protocol, which
// Neon's pooler keeps too (R29).
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errConnectionString
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open the pool: %w", err)
	}
	return pool, nil
}

// Beginner is what a transaction starts from: a pool, or a connection.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TxBeginner is what a transaction with its options starts from: a pool, or a
// connection.
type TxBeginner interface {
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
}

// WriteUnderAccount runs fn in a transaction that holds the account's row by FOR
// KEY SHARE and has found it not marked "deleting" (R21). The deletion of an
// account takes the row FOR UPDATE, so a write that got here before the mark
// commits before the mark is set, and one after it sees the mark and writes
// nothing. FOR KEY SHARE, rather than FOR SHARE, lets fn itself update fields of
// the row — the date of the request, the PIN's count — without locking out a
// neighbouring request of the same account. The transaction commits when fn
// returns nil, and rolls back on its error, which it returns as it is.
func WriteUnderAccount(ctx context.Context, db Beginner, accountID string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var deleting *time.Time
		err := tx.QueryRow(ctx, `SELECT deleting_at FROM accounts WHERE id = $1 FOR KEY SHARE`, accountID).Scan(&deleting)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoAccount
		case err != nil:
			return fmt.Errorf("store: hold the account: %w", err)
		case deleting != nil:
			return ErrAccountDeleting
		}
		return fn(tx)
	})
}

// MarkDeleting takes the account's row FOR UPDATE in tx and marks it "deleting",
// the first step of the deletion of an account (R21). FOR UPDATE is what waits
// for every write that holds the row by FOR KEY SHARE: a plain UPDATE of the mark
// takes FOR NO KEY UPDATE, which does not conflict with FOR KEY SHARE, and would
// let a write that does not touch the row commit after the mark. The deletion
// clears the account's data in the same tx, after this.
func MarkDeleting(ctx context.Context, tx pgx.Tx, accountID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNoAccount
	case err != nil:
		return fmt.Errorf("store: lock the account: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET deleting_at = coalesce(deleting_at, now()) WHERE id = $1`, id); err != nil {
		return fmt.Errorf("store: mark the account: %w", err)
	}
	return nil
}
