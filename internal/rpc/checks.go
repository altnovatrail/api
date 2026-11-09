package rpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/store"
)

// Rights is what the checks ask of the rights, and what the deletion of an
// account a user gone needs: authz.Rights.
type Rights interface {
	Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error)
	deletion.Rights
}

// DB is what the checks read and write the accounts on: the pool of the
// database api.
type DB interface {
	store.Querier
	store.Beginner
	store.TxBeginner
}

// Caller is who a request is from, as the checks found it: the handler reads it
// with CallerFrom.
type Caller struct {
	// UID is the sign-in service's ID of the token's user.
	UID string
	// AuthTime is when that user signed in.
	AuthTime time.Time
	// AccountID is the account of the UID; empty only on CreateAccount, for a UID
	// with no account yet.
	AccountID string
}

type callerKey struct{}

// CallerFrom is the caller the checks let through, on a method with a token.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

// checks runs the checks of architecture 3.1 in their order, before the handler
// (R20, R33): the class of the token, the mark "deleting", the account, the
// right on the profile — on its message, before anything is read by its ID —
// and the fresh sign-in, last.
type checks struct {
	signin authn.Port
	db     DB
	rights Rights
	// userGone is called for a user the sign-in service no longer has, whose
	// account is not marked: deleted past api, in the project's console (R20).
	userGone func(ctx context.Context, accountID, uid string) error
	now      func() time.Time
}

func (c *checks) intercept(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		r, known := methods[spec.Procedure]
		if !known {
			return connect.NewError(connect.CodeUnimplemented, "")
		}
		if r.open {
			return next(ctx, spec, stream)
		}
		info, served := connect.CallInfoForServerContext(ctx)
		if !served {
			return signInAgain()
		}
		token, ok := strings.CutPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			return signInAgain()
		}
		verified, err := c.signin.Verify(ctx, token)
		if err != nil {
			return c.refused(ctx, err)
		}
		caller := Caller{UID: verified.UID, AuthTime: verified.AuthTime}
		account, err := store.AccountByUID(ctx, c.db, verified.UID)
		switch {
		case errors.Is(err, store.ErrNoAccount):
			if !r.noAccount {
				return noAccess(ctx)
			}
		case err != nil:
			return Failed(ctx, err)
		case account.Deleting:
			return AccountGone(ctx)
		default:
			// Written once a day: the other requests of the day cost no round trip.
			if !account.TouchedOn(c.now()) {
				if err := store.TouchAccount(ctx, c.db, account.ID); err != nil {
					return Failed(ctx, err)
				}
			}
			caller.AccountID = account.ID
		}
		ctx = context.WithValue(ctx, callerKey{}, caller)
		if r.right == "" {
			if r.fresh && !authn.Fresh(caller.AuthTime, c.now()) {
				return freshSignInNeeded(ctx)
			}
			return next(ctx, spec, stream)
		}
		return next(ctx, spec, &checked{ServerStream: stream, ctx: ctx, checks: c, rule: r, caller: caller})
	}
}

// refused answers a token the sign-in service refused, by its class (R20;
// architecture 3.10).
func (c *checks) refused(ctx context.Context, err error) error {
	var refusal *authn.Refusal
	if !errors.As(err, &refusal) {
		return Failed(ctx, err)
	}
	noteOf(ctx).class = refusal.Class
	switch refusal.Class {
	case authn.Revoked:
		account, err := store.AccountByUID(ctx, c.db, refusal.UID)
		switch {
		case err == nil && account.Deleting:
			return AccountGone(ctx)
		case err != nil && !errors.Is(err, store.ErrNoAccount):
			return Failed(ctx, err)
		}
		return signInAgain()
	case authn.NoUser:
		account, err := store.AccountByUID(ctx, c.db, refusal.UID)
		switch {
		case errors.Is(err, store.ErrNoAccount):
		case err != nil:
			return Failed(ctx, err)
		case !account.Deleting:
			// The user was deleted past api: the account goes too, by the same
			// path as DeleteAccount, with no fresh sign-in, in this request (R20,
			// R21). Its user is gone already; what else does not finish after the
			// mark, the daily job does (T28). A mark that did not go is a failure,
			// and a repeat marks it.
			err := c.userGone(ctx, account.ID, refusal.UID)
			var unfinished *deletion.Unfinished
			switch {
			case errors.As(err, &unfinished):
				NoteFailure(ctx, unfinished)
			case errors.Is(err, store.ErrNoAccount):
				// A request beside this one deleted it first.
			case err != nil:
				return Failed(ctx, err)
			}
		}
		return AccountGone(ctx)
	case authn.Failed:
		return Failed(ctx, refusal)
	default:
		return signInAgain()
	}
}

// checked is the stream of a method with a right: its message is checked as it
// is received, before the handler reads anything by the profile's ID.
type checked struct {
	connect.ServerStream
	ctx    context.Context
	checks *checks
	rule   rule
	caller Caller
}

// profiled is a request that names a profile: every request of a method with a
// right does, as a test holds.
type profiled interface{ GetProfileId() string }

func (s *checked) Receive(msg any) error {
	if err := s.ServerStream.Receive(msg); err != nil {
		return err
	}
	request, ok := msg.(profiled)
	if !ok {
		return noProfile(s.ctx)
	}
	// An ID that is not canonical names no profile, and the rights are not asked
	// of it (store.CanonicalID).
	profileID := request.GetProfileId()
	if !store.CanonicalID(profileID) {
		return noProfile(s.ctx)
	}
	allowed, err := s.checks.rights.Can(s.ctx, s.caller.AccountID, s.rule.right, profileID)
	if err != nil {
		return Failed(s.ctx, err)
	}
	if !allowed {
		return noProfile(s.ctx)
	}
	// Read after the right, never before: a tuple whose profile is gone — a
	// dangling one (R21) — gives a right to nothing.
	there, activeOn, err := store.ProfileOf(s.ctx, s.checks.db, s.caller.AccountID, profileID)
	if err != nil {
		return Failed(s.ctx, err)
	}
	if !there {
		// Only by the deletion's own rule: with its fresh sign-in.
		if s.rule.dropsDangling && authn.Fresh(s.caller.AuthTime, s.checks.now()) {
			if err := s.checks.rights.DeleteOwner(s.ctx, s.caller.AccountID, profileID); err != nil {
				return Failed(s.ctx, err)
			}
		}
		return noProfile(s.ctx)
	}
	if s.rule.fresh && !authn.Fresh(s.caller.AuthTime, s.checks.now()) {
		return freshSignInNeeded(s.ctx)
	}
	// A request on the profile that passed every check is its activity, written
	// once a day (П-9; R36).
	if !store.TodayByUTC(activeOn, s.checks.now()) {
		if err := store.TouchProfile(s.ctx, s.checks.db, profileID); err != nil {
			return Failed(s.ctx, err)
		}
	}
	return nil
}

// RequireConsent is step 5 of architecture 3.1, for the handlers of the learning
// requests (E3): the profile's consent is confirmed, or the request is refused
// "no consent" (R116 spec). It runs after the checks, on the profile they let through.
func RequireConsent(ctx context.Context, db store.Querier, profileID string) error {
	given, err := store.ConsentGiven(ctx, db, profileID)
	if err != nil {
		return Failed(ctx, err)
	}
	if !given {
		return Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	}
	return nil
}
