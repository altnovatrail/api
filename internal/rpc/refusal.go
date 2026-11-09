package rpc

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"

	"github.com/altnovatrail/api/internal/authn"
)

// Refuse is a refusal of the contract: an error of Connect with its code and the
// Refusal of its cause as its detail (R17; R97 spec). data, when given, fills
// the Refusal's data. The message is the cause's name, which holds nothing of
// the request. The cause goes to the request's line of the log.
func Refuse(ctx context.Context, code connect.Code, cause v1.Cause, data ...func(*v1.Refusal)) error {
	refusal := &v1.Refusal{Cause: cause}
	for _, fill := range data {
		fill(refusal)
	}
	noteOf(ctx).cause = cause.String()
	err := connect.NewError(code, cause.String())
	// A Refusal of the contract always marshals: the error is ignored.
	detail, _ := connectproto.NewErrorDetail(refusal)
	return err.WithDetail(detail)
}

// AccountGone is the account marked "deleting" or deleted: the checks give it,
// and so does a handler that finds the account gone after them (R20).
func AccountGone(ctx context.Context) error {
	return Refuse(ctx, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
}

// The other refusals of the checks (architecture 3.1, 3.10).
func noAccess(ctx context.Context) error {
	return Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_NO_ACCESS)
}

func noProfile(ctx context.Context) error {
	return Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

func freshSignInNeeded(ctx context.Context) error {
	return Refuse(ctx, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED, func(r *v1.Refusal) {
		r.Data = &v1.Refusal_MaxAuthAgeSeconds{MaxAuthAgeSeconds: int32(authn.FreshFor / time.Second)}
	})
}

// signInAgain is a token that will not do, with no cause: the SDK on the device
// refreshes it, or signs out by itself (R20).
func signInAgain() error { return connect.NewError(connect.CodeUnauthenticated, "") }

// NoteFailure names, in the request's line of the log, a failure that its
// answer does not show: a deletion done in the database whose rest the job
// finishes (R21). The line is an error, with the failure's kind.
func NoteFailure(ctx context.Context, err error) { noteOf(ctx).failure = err }

// Failed is a check or a handler that did not finish — the sign-in service, the
// database, the rights. The client sees the code alone; the log names the
// failure by its kind.
func Failed(ctx context.Context, err error) error {
	noteOf(ctx).failure = err
	return connect.NewError(connect.CodeUnavailable, "").WithCause(err)
}
