package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/store"
)

// debugAccessUsage is what api debug-access says of itself.
const debugAccessUsage = `Usage: api debug-access grant <uid> | revoke <uid>

The right of the debug mode of the models of a task from paper: give it to the
account of a sign-in UID, or take it. The account then has it, or not, from its
next request. A UID with no account, or one of an account being deleted, is
refused.
`

// maxUID is the longest UID the sign-in service gives a user, in bytes.
const maxUID = 128

// setDebugAccess gives the account of a sign-in UID the right of the debug
// mode, or takes it (R97). The operator names the account by the UID the
// sign-in service shows; neither the output nor the log names it. stdout says
// whether the account has the right now.
func setDebugAccess(ctx context.Context, args, environ []string, stdout, stderr io.Writer) error {
	action, uid, err := operatorArgs("debug-access", debugAccessUsage, args, stderr, func(rest []string) (string, string, error) {
		switch {
		case len(rest) != 2 || (rest[0] != "grant" && rest[0] != "revoke"):
			return "", "", errors.New("give grant <uid> or revoke <uid>")
		case rest[1] == "" || len(rest[1]) > maxUID:
			// The argument is not echoed: it is an identifier of a user.
			return "", "", errors.New("the argument is no UID of the sign-in service")
		}
		return rest[0], rest[1], nil
	})
	if err != nil {
		return err
	}
	pool, log, err := operatorPool(ctx, environ, stderr, logging.MomentDebug)
	if err != nil {
		return err
	}
	defer pool.Close()
	switch err := store.SetDebugModels(ctx, pool, uid, action == "grant"); {
	case errors.Is(err, store.ErrNoAccount):
		fmt.Fprintln(stderr, "api debug-access: no account has this UID, or it is being deleted")
		return errLogged
	case err != nil:
		return logged(log, logging.MomentDebug, err, logging.FieldDatabase, logging.DatabaseAPI)
	}
	if action == "grant" {
		fmt.Fprintln(stdout, "granted")
	} else {
		fmt.Fprintln(stdout, "revoked")
	}
	return nil
}
