// Package deletion is the deletion of a profile and of an account in the order
// of R21 (architecture 3.10): the database first, then the tuples of the rights,
// then the user at the sign-in service, and the account's row last. What does not
// finish after the database is done is the daily job's to finish (T28), and the
// caller answers "deleted" all the same.
package deletion

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/store"
)

// Rights is what the deletion asks of the rights: authz.Rights.
type Rights interface {
	DeleteOwner(ctx context.Context, accountID, profileID string) error
	DeleteOwned(ctx context.Context, accountID string, profileIDs []string) error
	OwnedProfiles(ctx context.Context, accountID string) ([]string, error)
}

// Signin is what the deletion asks of the sign-in service: authn.Port.
type Signin interface {
	RemoveUser(ctx context.Context, uid string) error
}

// Unfinished is a deletion whose database part is done and whose rest is not: a
// tuple left dangling, a user left at the sign-in service, the account's row. The
// caller answers "deleted": nothing of a learner is left in the database (R21).
type Unfinished struct{ err error }

func (u *Unfinished) Error() string { return "deletion: unfinished: " + u.err.Error() }

func (u *Unfinished) Unwrap() error { return u.err }

// Deleter deletes on the database api, the rights and the sign-in service.
type Deleter struct {
	db     store.Beginner
	rights Rights
	signin Signin
}

// New is the deletion on db, rights and signin.
func New(db store.Beginner, rights Rights, signin Signin) *Deleter {
	return &Deleter{db: db, rights: rights, signin: signin}
}

// Profile deletes the account's profile (R21; architecture 3.10): one
// transaction under the account, the account's row FOR KEY SHARE and the
// profile's FOR UPDATE, then its tuple. A profile that is not the account's is
// store.ErrNoProfile, a marked account store.ErrAccountDeleting; a tuple that
// does not go is Unfinished, dangling: no row is there for it to give a right to,
// and the job removes it.
func (d *Deleter) Profile(ctx context.Context, accountID, profileID string) error {
	err := store.WriteUnderAccount(ctx, d.db, accountID, func(tx pgx.Tx) error {
		if _, err := store.ProfileFor(ctx, tx, accountID, profileID); err != nil {
			return err
		}
		return store.DeleteProfileRows(ctx, tx, profileID)
	})
	if err != nil {
		return err
	}
	if err := d.rights.DeleteOwner(ctx, accountID, profileID); err != nil {
		return &Unfinished{err: err}
	}
	return nil
}

// Account deletes the account of the sign-in service's uid (R21; architecture
// 3.10). Step 1 is one transaction: the account's row FOR UPDATE, the mark
// "deleting", and all its data. When it fails, nothing is done, and the error is
// the caller's to answer. The rest — Finish — that does not finish is
// Unfinished: the mark stays, which tells the other devices the account is gone
// and which the job finishes.
func (d *Deleter) Account(ctx context.Context, accountID, uid string) error {
	err := pgx.BeginFunc(ctx, d.db, func(tx pgx.Tx) error {
		if err := store.MarkDeleting(ctx, tx, accountID); err != nil {
			return err
		}
		return store.ClearAccount(ctx, tx, accountID)
	})
	if err != nil {
		return err
	}
	if err := d.Finish(ctx, accountID, uid); err != nil {
		return &Unfinished{err: err}
	}
	return nil
}

// Finish does steps 2 to 4 of the deletion of a marked account (R21;
// architecture 3.10): the tuples of all its profiles, read by the account and
// deleted in batches; the user at the sign-in service — its refresh tokens
// revoked, then the user deleted, a user not there being done — which goes on
// when the tuples failed, since the sign-in service holds the person's identity
// and the tuples only IDs; and only when both are done, the account's row. A
// repeat redoes what is left; the job of T28 calls it for the marks the requests
// left.
func (d *Deleter) Finish(ctx context.Context, accountID, uid string) error {
	tuples := d.tuples(ctx, accountID)
	var user error
	if err := d.signin.RemoveUser(ctx, uid); err != nil {
		user = fmt.Errorf("deletion: the user: %w", err)
	}
	// One failure is itself, so that the log names its kind; two are joined.
	switch {
	case tuples != nil && user != nil:
		return errors.Join(tuples, user)
	case tuples != nil:
		return tuples
	case user != nil:
		return user
	}
	return store.DeleteAccountRow(ctx, d.db, accountID)
}

func (d *Deleter) tuples(ctx context.Context, accountID string) error {
	owned, err := d.rights.OwnedProfiles(ctx, accountID)
	if err == nil {
		err = d.rights.DeleteOwned(ctx, accountID, owned)
	}
	if err != nil {
		return fmt.Errorf("deletion: the tuples: %w", err)
	}
	return nil
}
