package authn_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"firebase.google.com/go/v4/auth"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/logging"
)

// adapter is the adapter on the tests' Google.
func adapter(t *testing.T) *authn.Firebase {
	t.Helper()
	f, err := authn.NewWithClient(t.Context(), authn.Options{ProjectID: project}, &http.Client{Transport: google.transport})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// refusal is the refusal Verify gave, or the test fails.
func refusal(t *testing.T, err error) *authn.Refusal {
	t.Helper()
	var r *authn.Refusal
	if !errors.As(err, &r) {
		t.Fatalf("Verify error = %v, want a refusal", err)
	}
	return r
}

func TestAValidTokenGivesItsUserAndTimeOfSignIn(t *testing.T) {
	g := at(t)
	g.addUser("alive", fakeUser{})
	signedIn := time.Now().Add(-time.Minute).Truncate(time.Second).UTC()

	token, err := adapter(t).Verify(t.Context(), g.token(t, "alive", signedIn))
	if err != nil {
		t.Fatalf("Verify error = %v, want nil", err)
	}
	if token.UID != "alive" || !token.AuthTime.Equal(signedIn) {
		t.Errorf("Verify = %+v, want alive, signed in at %v", token, signedIn)
	}
	if got := g.callsMade(); !slices.Equal(got, []string{"lookup:alive"}) {
		t.Errorf("calls = %v, want one lookup: the check of revocation", got)
	}
	if g.fetched() == 0 {
		t.Error("the SDK took no keys of the tests' Google")
	}
}

func TestATokenWithNoTimeOfSignInHasNone(t *testing.T) {
	g := at(t)
	g.addUser("alive", fakeUser{})
	c := claimsOf("alive", time.Now())
	delete(c, "auth_time")

	token, err := adapter(t).Verify(t.Context(), sign(t, g.key, keyID, c))
	if err != nil || !token.AuthTime.IsZero() {
		t.Errorf("Verify = %+v, %v; want no time of sign-in", token, err)
	}
}

// Each refusal of the SDK is its class, by the table of architecture 3.10, and a
// revoked token, or one of a user who is gone, still gives its UID.
func TestEachRefusalIsItsClass(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		user  *fakeUser
		token func(*testing.T, *fakeGoogle) string
		class authn.Class
		uid   string
	}{
		{"expired", &fakeUser{}, func(t *testing.T, g *fakeGoogle) string {
			c := claimsOf("u", now.Add(-3*time.Hour))
			c["iat"], c["exp"] = now.Add(-2*time.Hour).Unix(), now.Add(-time.Hour).Unix()
			return sign(t, g.key, keyID, c)
		}, authn.Expired, ""},
		{"signed by no key of Google's", &fakeUser{}, func(t *testing.T, g *fakeGoogle) string {
			return sign(t, g.stranger, keyID, claimsOf("u", now))
		}, authn.Invalid, ""},
		{"of another project", &fakeUser{}, func(t *testing.T, g *fakeGoogle) string {
			c := claimsOf("u", now)
			c["aud"] = "another-project"
			return sign(t, g.key, keyID, c)
		}, authn.Invalid, ""},
		{"malformed", &fakeUser{}, func(*testing.T, *fakeGoogle) string { return "not.a.token" }, authn.Invalid, ""},
		{"empty", &fakeUser{}, func(*testing.T, *fakeGoogle) string { return "" }, authn.Invalid, ""},
		{"revoked", &fakeUser{validSince: now.Add(time.Minute).Unix()}, func(t *testing.T, g *fakeGoogle) string {
			return g.token(t, "u", now)
		}, authn.Revoked, "u"},
		{"of a user who is gone", nil, func(t *testing.T, g *fakeGoogle) string {
			return g.token(t, "u", now)
		}, authn.NoUser, "u"},
		{"of a disabled user", &fakeUser{disabled: true}, func(t *testing.T, g *fakeGoogle) string {
			return g.token(t, "u", now)
		}, authn.Disabled, ""},
		{"whose lookup fails", &fakeUser{broken: true}, func(t *testing.T, g *fakeGoogle) string {
			return g.token(t, "u", now)
		}, authn.Failed, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := at(t)
			if tc.user != nil {
				g.addUser("u", *tc.user)
			}
			_, err := adapter(t).Verify(t.Context(), tc.token(t, g))
			r := refusal(t, err)
			if r.Class != tc.class || r.UID != tc.uid {
				t.Errorf("refusal = %v with UID %q, want %v with %q", r.Class, r.UID, tc.class, tc.uid)
			}
			if r.Unwrap() == nil {
				t.Error("the refusal lost the SDK's error")
			}
		})
	}
}

// The UID of a revoked token is read by its signature alone: no second call.
func TestARevokedTokenIsReadWithNoSecondCall(t *testing.T) {
	g := at(t)
	g.addUser("u", fakeUser{validSince: time.Now().Add(time.Minute).Unix()})

	_, err := adapter(t).Verify(t.Context(), g.token(t, "u", time.Now()))
	if r := refusal(t, err); r.Class != authn.Revoked {
		t.Fatalf("refusal = %v, want revoked", r.Class)
	}
	if got := g.callsMade(); !slices.Equal(got, []string{"lookup:u"}) {
		t.Errorf("calls = %v, want the one lookup of the revocation check", got)
	}
}

func TestKeysThatCannotBeHadAreAFailure(t *testing.T) {
	g := at(t)
	g.addUser("u", fakeUser{})
	g.set(func(g *fakeGoogle) { g.keysFailFrom = 1 })

	_, err := adapter(t).Verify(t.Context(), g.token(t, "u", time.Now()))
	if r := refusal(t, err); r.Class != authn.Failed || !auth.IsCertificateFetchFailed(r.Unwrap()) {
		t.Errorf("refusal = %v (%v), want a failure to fetch the keys", r.Class, r.Unwrap())
	}
}

// A revoked token whose UID cannot be read, because the keys could not be had
// again, is a failure: api cannot tell which account it is.
func TestARevokedTokenWhoseUIDCannotBeReadIsAFailure(t *testing.T) {
	g := at(t)
	g.addUser("u", fakeUser{validSince: time.Now().Add(time.Minute).Unix()})
	g.set(func(g *fakeGoogle) { g.keysMaxAge, g.keysFailFrom = 0, 2 })

	_, err := adapter(t).Verify(t.Context(), g.token(t, "u", time.Now()))
	if r := refusal(t, err); r.Class != authn.Failed || r.UID != "" {
		t.Errorf("refusal = %v with UID %q, want a failure with none", r.Class, r.UID)
	}
	if kind := logging.ErrorKind(err).String(); !strings.HasSuffix(kind, "*errors.joinError[2]") {
		t.Errorf("the log's kind = %q, want both errors in the chain", kind)
	}
}

// What a refusal says, and what the log writes of it, holds neither the UID nor
// the SDK's words, which name it.
func TestARefusalHoldsNoUID(t *testing.T) {
	g := at(t)
	uid := "a-uid-to-keep-out-of-the-log"

	_, err := adapter(t).Verify(t.Context(), g.token(t, uid, time.Now()))
	r := refusal(t, err)
	if !strings.Contains(r.Unwrap().Error(), uid) {
		t.Fatalf("the SDK's error %q does not name the UID: the test checks nothing", r.Unwrap())
	}
	if err.Error() != "authn: token refused: no_user" {
		t.Errorf("Error() = %q, want the class alone", err.Error())
	}
	if kind := logging.ErrorKind(err).String(); strings.Contains(kind, uid) {
		t.Errorf("the log's kind %q holds the UID", kind)
	}
}

func TestRemoveUserRevokesThenDeletes(t *testing.T) {
	g := at(t)
	g.addUser("u", fakeUser{})

	if err := adapter(t).RemoveUser(t.Context(), "u"); err != nil {
		t.Fatalf("RemoveUser error = %v, want nil", err)
	}
	if got := g.callsMade(); !slices.Equal(got, []string{"revoke:u", "delete:u"}) {
		t.Errorf("calls = %v, want the revocation, then the deletion", got)
	}
	if g.has("u") {
		t.Error("the user is still there")
	}
}

func TestRemovingAUserWhoIsGoneIsDone(t *testing.T) {
	at(t)
	if err := adapter(t).RemoveUser(t.Context(), "gone"); err != nil {
		t.Errorf("RemoveUser error = %v, want nil: the step is done", err)
	}
}

// A failed revocation does not stop the deletion, and once the user is deleted
// the step is done (R21).
func TestTheDeletionGoesOnWhenTheRevocationFails(t *testing.T) {
	g := at(t)
	g.addUser("u", fakeUser{})
	g.set(func(g *fakeGoogle) { g.failRevoke = true })

	if err := adapter(t).RemoveUser(t.Context(), "u"); err != nil {
		t.Errorf("RemoveUser error = %v, want nil: the user is deleted", err)
	}
	if g.has("u") {
		t.Error("the user is still there")
	}
}

func TestAFailedDeletionIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failRevoke bool
		links      string
	}{
		{"the deletion alone", false, "*internal.FirebaseError"},
		{"both", true, "*errors.joinError[2]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := at(t)
			g.addUser("a-uid", fakeUser{})
			g.set(func(g *fakeGoogle) { g.failRevoke, g.failDelete = tc.failRevoke, true })

			err := adapter(t).RemoveUser(t.Context(), "a-uid")
			if err == nil || err.Error() != "authn: remove user failed" {
				t.Fatalf("RemoveUser error = %v, want the failure, with no UID", err)
			}
			if kind := logging.ErrorKind(err).String(); !strings.HasSuffix(kind, tc.links) {
				t.Errorf("the log's kind = %q, want it to end with %q", kind, tc.links)
			}
		})
	}
}

func TestUsersListsEveryPage(t *testing.T) {
	g := at(t)
	login := time.Date(2026, 11, 1, 10, 0, 0, 123_000_000, time.UTC)
	created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	g.addUser("a", fakeUser{lastLoginAt: login.UnixMilli(), lastRefreshAt: "2026-11-02T10:00:00Z", createdAt: created.UnixMilli()})
	for _, uid := range []string{"b", "c", "d", "e"} {
		g.addUser(uid, fakeUser{})
	}

	var got []authn.User
	for user, err := range adapter(t).Users(t.Context()) {
		if err != nil {
			t.Fatalf("Users error = %v", err)
		}
		got = append(got, user)
	}
	if len(got) != 5 || got[0].UID != "a" || got[4].UID != "e" {
		t.Fatalf("Users = %+v, want a to e", got)
	}
	if !got[0].LastSignIn.Equal(login) || !got[0].LastRefresh.Equal(time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)) ||
		!got[0].Created.Equal(created) {
		t.Errorf("a = %+v, want its times to the millisecond", got[0])
	}
	if !got[1].LastSignIn.IsZero() || !got[1].LastRefresh.IsZero() || !got[1].Created.IsZero() {
		t.Errorf("b = %+v, want no times: it never signed in", got[1])
	}
	if pages := g.callsMade(); !slices.Equal(pages, []string{"list:", "list:2", "list:4"}) {
		t.Errorf("pages = %v, want three", pages)
	}
}

func TestUsersStopsWhenAsked(t *testing.T) {
	g := at(t)
	for _, uid := range []string{"a", "b", "c"} {
		g.addUser(uid, fakeUser{})
	}
	for user := range adapter(t).Users(t.Context()) {
		if user.UID != "a" {
			t.Errorf("the first user = %q, want a", user.UID)
		}
		break
	}
	if pages := g.callsMade(); len(pages) != 1 {
		t.Errorf("pages = %v, want only the first", pages)
	}
}

func TestAFailedListEndsWithItsError(t *testing.T) {
	g := at(t)
	g.addUser("a", fakeUser{})
	g.set(func(g *fakeGoogle) { g.failList = true })

	var errs []error
	for _, err := range adapter(t).Users(t.Context()) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || errs[0] == nil || errs[0].Error() != "authn: list users failed" {
		t.Errorf("Users = %v, want one failure", errs)
	}
}

// With no key, the adapter takes the service's token from the metadata server,
// and never the machine's default credentials: a file those would read, here one
// that is not there, is not read, and New makes no call.
func TestWithNoKeyTheDefaultCredentialsAreNotRead(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
	for _, credentials := range [][]byte{nil, {}} {
		if _, err := authn.New(t.Context(), authn.Options{ProjectID: project, Credentials: credentials}); err != nil {
			t.Errorf("New with no key (%q): error = %v, want nil", credentials, err)
		}
	}
}

// The emulator verifies no signature, so api does not start on it.
func TestTheEmulatorIsRefused(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	_, err := authn.NewWithClient(t.Context(), authn.Options{ProjectID: project}, &http.Client{Transport: google.transport})
	if err == nil || !strings.Contains(err.Error(), "FIREBASE_AUTH_EMULATOR_HOST") {
		t.Errorf("New with the emulator's variable: error = %v, want a refusal naming it", err)
	}
}

func TestTheAdapterNeedsAProject(t *testing.T) {
	if _, err := authn.New(t.Context(), authn.Options{}); err == nil {
		t.Error("New with no project: error = nil")
	}
}

// The key is a service account's, and only that: a key of another type, or one
// that is not JSON, is refused. A service account's key builds the adapter with
// no call.
func TestTheKeyIsAServiceAccountsOnly(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	serviceAccount, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     project,
		"private_key_id": keyID,
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   "api@" + project + ".iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authn.New(t.Context(), authn.Options{ProjectID: project, Credentials: serviceAccount}); err != nil {
		t.Errorf("New with a service account's key: error = %v, want nil", err)
	}

	user := []byte(`{"type":"authorized_user","client_id":"1","client_secret":"not-a-secret","refresh_token":"not-a-token"}`)
	broken := []byte(`{"type":"service_account","project_id":"` + project + `","private_key":"not a key","client_email":"api@` + project + `.iam.gserviceaccount.com"}`)
	for name, credentials := range map[string][]byte{"a user's": user, "not JSON": []byte("not JSON"), "a broken service account's": broken} {
		if _, err := authn.New(t.Context(), authn.Options{ProjectID: project, Credentials: credentials}); err == nil {
			t.Errorf("New with %s key: error = nil, want a refusal", name)
		}
	}
}
