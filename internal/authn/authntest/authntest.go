// Package authntest is a fake of authn.Port for the tests of the code that holds
// it: a token is whatever the test says it is, and the users are the test's own.
package authntest

import (
	"context"
	"errors"
	"iter"
	"slices"
	"sync"

	"github.com/altnovatrail/api/internal/authn"
)

// ErrFailed is the error of a call the test asked to fail.
var ErrFailed = errors.New("authntest: the call failed, as the test asked")

// Fake is a sign-in service the test sets up. Its zero value refuses every token
// as Invalid and has no users. It is safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	tokens  map[string]outcome
	users   []authn.User
	removed []string
	fail    failing
}

// failing is the calls the test asked to fail.
type failing struct {
	verify, remove, users bool
}

var _ authn.Port = (*Fake)(nil)

// outcome is what Verify answers for a token.
type outcome struct {
	token   authn.Token
	refusal *authn.Refusal
}

// Accept makes Verify accept idToken as token.
func (f *Fake) Accept(idToken string, token authn.Token) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set(idToken, outcome{token: token})
}

// Refuse makes Verify refuse idToken by class. The UID goes with the classes
// Revoked and NoUser, as the adapter's does.
func (f *Fake) Refuse(idToken string, class authn.Class, uid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refusal := &authn.Refusal{Class: class}
	if class == authn.Revoked || class == authn.NoUser {
		refusal.UID = uid
	}
	f.set(idToken, outcome{refusal: refusal})
}

func (f *Fake) set(idToken string, o outcome) {
	if f.tokens == nil {
		f.tokens = map[string]outcome{}
	}
	f.tokens[idToken] = o
}

// AddUser adds a user to the list Users gives.
func (f *Fake) AddUser(user authn.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, user)
}

// FailVerify makes every later Verify refuse its token as authn.Failed.
func (f *Fake) FailVerify() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail.verify = true
}

// FailRemoveUser makes every later RemoveUser fail with ErrFailed, the user kept.
func (f *Fake) FailRemoveUser() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail.remove = true
}

// FailUsers makes every later list of Users end at once with ErrFailed.
func (f *Fake) FailUsers() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail.users = true
}

// Removed is the UIDs RemoveUser was called with, in order.
func (f *Fake) Removed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

// Verify answers as the test set the token up.
func (f *Fake) Verify(_ context.Context, idToken string) (authn.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail.verify {
		return authn.Token{}, &authn.Refusal{Class: authn.Failed}
	}
	o, known := f.tokens[idToken]
	switch {
	case !known:
		return authn.Token{}, &authn.Refusal{Class: authn.Invalid}
	case o.refusal != nil:
		refusal := *o.refusal
		return authn.Token{}, &refusal
	default:
		return o.token, nil
	}
}

// RemoveUser records the call and takes the user out of the list: a user who is
// not there is no error, as with the adapter.
func (f *Fake) RemoveUser(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, uid)
	if f.fail.remove {
		return ErrFailed
	}
	f.users = slices.DeleteFunc(f.users, func(u authn.User) bool { return u.UID == uid })
	return nil
}

// Users lists the users as they are when the list starts.
func (f *Fake) Users(context.Context) iter.Seq2[authn.User, error] {
	f.mu.Lock()
	users, failed := slices.Clone(f.users), f.fail.users
	f.mu.Unlock()
	return func(yield func(authn.User, error) bool) {
		if failed {
			yield(authn.User{}, ErrFailed)
			return
		}
		for _, user := range users {
			if !yield(user, nil) {
				return
			}
		}
	}
}
