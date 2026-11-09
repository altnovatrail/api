// Package authn is api's port to the sign-in service. It verifies an ID token of
// Firebase Auth with its revocation and names the class of a refusal, removes a
// user and lists the users (R03, R20, R21, R32). api's code holds the Port:
// Firebase is its adapter on the Admin SDK for Go, and authntest a fake of it for
// the tests of the rest. The sign-in service's UID lives only in the account's
// row; rights are the account's (R04), so another sign-in service changes the
// adapter, not the rights or the data.
package authn

import (
	"context"
	"iter"
	"time"
)

// Port is what api asks of the sign-in service.
type Port interface {
	// Verify verifies an ID token with its revocation. A refused token is a
	// *Refusal, which names its class.
	Verify(ctx context.Context, idToken string) (Token, error)
	// RemoveUser revokes the user's refresh tokens, then deletes the user: step 3
	// of an account's deletion (R21). A user who is not there is a step already
	// done, and no error.
	RemoveUser(ctx context.Context, uid string) error
	// Users lists every user of the sign-in service, for the operator's job that
	// removes those without an account (R21). An error ends the list.
	Users(ctx context.Context) iter.Seq2[User, error]
}

// Token is what api reads of a verified ID token: the user and when they signed
// in. Neither the email nor any other claim is read (R29 canvas-api).
type Token struct {
	// UID is the sign-in service's ID of the user.
	UID string
	// AuthTime is when the user signed in, not when the token was refreshed.
	AuthTime time.Time
}

// User is what api reads of a user of the sign-in service.
type User struct {
	// UID is the sign-in service's ID of the user.
	UID string
	// LastSignIn is when the user last signed in; zero if never.
	LastSignIn time.Time
	// LastRefresh is when the user's ID token was last refreshed; zero if never.
	LastRefresh time.Time
	// Created is when the user was made, so that one made and never signed in is
	// not taken for one long gone.
	Created time.Time
}

// Class is why a token was refused (R20; architecture 3.10).
type Class int

// The classes of a refusal.
const (
	// Expired is a token past its time: the SDK on the device refreshes it.
	Expired Class = iota + 1
	// Invalid is a token that is not one: malformed, of another project, or
	// signed by no key of Google's.
	Invalid
	// Revoked is a token issued before the user's tokens were revoked.
	Revoked
	// NoUser is a token whose user the sign-in service no longer has.
	NoUser
	// Disabled is a token whose user is disabled.
	Disabled
	// Failed is a check that did not finish: Google's keys, the network, the
	// service. It says nothing of the token.
	Failed
)

// classNames are the classes' names, as a log writes them.
var classNames = map[Class]string{
	Expired:  "expired",
	Invalid:  "invalid",
	Revoked:  "revoked",
	NoUser:   "no_user",
	Disabled: "disabled",
	Failed:   "failed",
}

// String is the class's name.
func (c Class) String() string {
	if name, known := classNames[c]; known {
		return name
	}
	return "unknown"
}

// Refusal is a token that Verify refused, by its class. Its text is the class
// alone: the SDK's words name the UID, and a token or a UID is not for a log or a
// reply. The SDK's error is its link, for the log to name by its type.
type Refusal struct {
	// Class is why the token was refused.
	Class Class
	// UID is the token's user, read from its signature alone, when the class is
	// Revoked or NoUser: api answers those by the account's mark (R20).
	UID string
	err error
}

// Error is the refusal's class.
func (r *Refusal) Error() string { return "authn: token refused: " + r.Class.String() }

// Unwrap is the SDK's error.
func (r *Refusal) Unwrap() error { return r.err }

// FreshFor is how old a sign-in may be, and how far ahead of api's clock, for an
// action that waits for a fresh one: the deletion of a profile or of an account,
// and a forgotten PIN's change (R20). A refusal for want of one carries it, as
// RFC 9470 carries max_age.
const FreshFor = 300 * time.Second

// Fresh says whether a sign-in at authTime is fresh at now: no older than
// FreshFor, and no further ahead than FreshFor either, the leeway the Admin SDK
// gives a token's own times. A token with no time of sign-in is not fresh.
func Fresh(authTime, now time.Time) bool {
	if authTime.IsZero() {
		return false
	}
	return !authTime.Before(now.Add(-FreshFor)) && !authTime.After(now.Add(FreshFor))
}
