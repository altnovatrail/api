package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// Options are what the adapter of Firebase Auth is built from (R32).
type Options struct {
	// ProjectID is the Firebase project whose tokens api accepts.
	ProjectID string
	// Credentials is the JSON of a service account's key, and only of a service
	// account, off a deployment. Empty is the token of the service's own account
	// from the metadata server, as on Cloud Run, with no key: never the
	// credentials the machine has by default, which may be gcloud's, of a user
	// and another project.
	Credentials []byte
}

// emulatorHost is the variable that turns the SDK to the emulator, which
// verifies no signature: api refuses to start with it (R32).
const emulatorHost = "FIREBASE_AUTH_EMULATOR_HOST"

// scopes are the scopes of the service's token: those the Admin SDK v4.22.0 asks
// for itself, its internal.FirebaseScopes, which no code outside it can read.
var scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/datastore",
	"https://www.googleapis.com/auth/devstorage.full_control",
	"https://www.googleapis.com/auth/firebase",
	"https://www.googleapis.com/auth/identitytoolkit",
	"https://www.googleapis.com/auth/userinfo.email",
}

// The refusals of New.
var (
	// errNoProject is an adapter asked for with no project: it would accept no
	// token.
	errNoProject = errors.New("authn: no Firebase project ID")
	// errNotAServiceAccount is a key that is not JSON of a service account's.
	errNotAServiceAccount = errors.New("authn: the key is not a service account's")
	// errEmulator is the SDK turned to the emulator, which would accept a token
	// with no signature.
	errEmulator = errors.New("authn: " + emulatorHost + " is set: the emulator verifies no signature")
)

// serviceAccount refuses a key that is not a service account's. The library
// checks the type too, but only on the first call that needs the key; its
// refusal, and this one, hold none of the key.
func serviceAccount(key []byte) error {
	var parsed struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(key, &parsed) != nil || parsed.Type != "service_account" {
		return errNotAServiceAccount
	}
	return nil
}

// Firebase is the Port on Firebase Auth, through the Admin SDK for Go (R03).
type Firebase struct {
	client *auth.Client
}

var _ Port = (*Firebase)(nil)

// New builds the adapter. It makes no call: Google's keys come with the first
// token, and the service's token with the first call that needs it.
func New(ctx context.Context, o Options) (*Firebase, error) {
	return newFirebase(ctx, o)
}

// newFirebase builds the adapter with more of the SDK's options: the tests' client
// of HTTP, which reaches their own Google.
func newFirebase(ctx context.Context, o Options, extra ...option.ClientOption) (*Firebase, error) {
	if o.ProjectID == "" {
		return nil, errNoProject
	}
	if os.Getenv(emulatorHost) != "" {
		return nil, errEmulator
	}
	var opts []option.ClientOption
	if len(o.Credentials) > 0 {
		// A key of any other type would be one api did not mean to take.
		if err := serviceAccount(o.Credentials); err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, o.Credentials))
	} else {
		opts = append(opts, option.WithTokenSource(google.ComputeTokenSource("", scopes...)))
	}
	opts = append(opts, extra...)
	// NewApp fails only when it reads its config from FIREBASE_CONFIG, and it is
	// given one.
	app, _ := firebase.NewApp(ctx, &firebase.Config{ProjectID: o.ProjectID}, opts...)
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("authn: %w", err)
	}
	return &Firebase{client: client}, nil
}

// Verify verifies the token's signature and asks the sign-in service whether it
// is revoked and its user still there and enabled. A token revoked, or of a user
// who is not there, is read once more by its signature alone, with no call, for
// its UID (R20).
func (f *Firebase) Verify(ctx context.Context, idToken string) (Token, error) {
	token, err := f.client.VerifyIDTokenAndCheckRevoked(ctx, idToken)
	if err == nil {
		return tokenOf(token), nil
	}
	refusal := &Refusal{Class: classOf(err), err: err}
	if refusal.Class == Revoked || refusal.Class == NoUser {
		read, readErr := f.client.VerifyIDToken(ctx, idToken)
		if readErr != nil {
			// The UID is not known, so the refusal is the one of the token as it
			// is now — expired by then, or Google's keys not to be had — with
			// both errors in its chain.
			return Token{}, &Refusal{Class: classOf(readErr), err: errors.Join(err, readErr)}
		}
		refusal.UID = read.UID
	}
	return Token{}, refusal
}

// classOf is the class of the SDK's error, which reaches it unwrapped: its
// helpers compare the error's type, not its chain. The order matters, since
// IsIDTokenInvalid holds for an expired, a revoked and a disabled one too.
func classOf(err error) Class {
	switch {
	case auth.IsIDTokenExpired(err):
		return Expired
	case auth.IsIDTokenRevoked(err):
		return Revoked
	case auth.IsUserDisabled(err):
		return Disabled
	case auth.IsUserNotFound(err):
		return NoUser
	case auth.IsIDTokenInvalid(err):
		return Invalid
	default:
		return Failed
	}
}

func tokenOf(token *auth.Token) Token {
	verified := Token{UID: token.UID}
	if token.AuthTime != 0 {
		verified.AuthTime = time.Unix(token.AuthTime, 0).UTC()
	}
	return verified
}

// failure is a call to the sign-in service that failed. Its text says which: the
// SDK's words may name the UID. The SDK's error is its link, for the log to name
// by its type.
type failure struct {
	call string
	err  error
}

func (f *failure) Error() string { return "authn: " + f.call + " failed" }

func (f *failure) Unwrap() error { return f.err }

// RemoveUser revokes the user's refresh tokens and deletes the user. The deletion
// goes on when the revocation fails, since a deleted user's refresh token serves
// nobody, and the step is done once the deletion is (R21).
func (f *Firebase) RemoveUser(ctx context.Context, uid string) error {
	revoked := f.client.RevokeRefreshTokens(ctx, uid)
	deleted := f.client.DeleteUser(ctx, uid)
	if deleted == nil || auth.IsUserNotFound(deleted) {
		return nil
	}
	if revoked != nil && !auth.IsUserNotFound(revoked) {
		return &failure{call: "remove user", err: errors.Join(revoked, deleted)}
	}
	return &failure{call: "remove user", err: deleted}
}

// Users lists the users a page at a time, as the sign-in service gives them.
func (f *Firebase) Users(ctx context.Context) iter.Seq2[User, error] {
	return func(yield func(User, error) bool) {
		users := f.client.Users(ctx, "")
		for {
			record, err := users.Next()
			if errors.Is(err, iterator.Done) {
				return
			}
			if err != nil {
				yield(User{}, &failure{call: "list users", err: err})
				return
			}
			if !yield(userOf(record), nil) {
				return
			}
		}
	}
}

func userOf(record *auth.ExportedUserRecord) User {
	return User{
		UID:         record.UID,
		LastSignIn:  millis(record.UserMetadata.LastLogInTimestamp),
		LastRefresh: millis(record.UserMetadata.LastRefreshTimestamp),
		Created:     millis(record.UserMetadata.CreationTimestamp),
	}
}

// millis is a time the SDK gives in milliseconds since the epoch, 0 for never.
func millis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
