package authntest_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/authn/authntest"
)

func TestTheFakeAnswersAsItIsSetUp(t *testing.T) {
	t.Parallel()

	var fake authntest.Fake
	signedIn := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	fake.Accept("good", authn.Token{UID: "u", AuthTime: signedIn})
	fake.Refuse("revoked", authn.Revoked, "r")
	fake.Refuse("expired", authn.Expired, "e")

	if token, err := fake.Verify(t.Context(), "good"); err != nil || token.UID != "u" || !token.AuthTime.Equal(signedIn) {
		t.Errorf("Verify(good) = %+v, %v", token, err)
	}
	for _, tc := range []struct {
		token string
		class authn.Class
		uid   string
	}{
		{"revoked", authn.Revoked, "r"},
		{"expired", authn.Expired, ""},
		{"unknown", authn.Invalid, ""},
	} {
		_, err := fake.Verify(t.Context(), tc.token)
		var r *authn.Refusal
		if !errors.As(err, &r) || r.Class != tc.class || r.UID != tc.uid {
			t.Errorf("Verify(%s) = %v, want %v with UID %q", tc.token, err, tc.class, tc.uid)
		}
	}

	fake.FailVerify()
	_, err := fake.Verify(t.Context(), "good")
	var r *authn.Refusal
	if !errors.As(err, &r) || r.Class != authn.Failed {
		t.Errorf("Verify after FailVerify = %v, want a failure", err)
	}
}

func TestTheFakeRemovesAndListsUsers(t *testing.T) {
	t.Parallel()

	var fake authntest.Fake
	for _, uid := range []string{"a", "b", "c"} {
		fake.AddUser(authn.User{UID: uid})
	}
	if err := fake.RemoveUser(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if err := fake.RemoveUser(t.Context(), "gone"); err != nil {
		t.Errorf("RemoveUser of a user who is gone = %v, want nil", err)
	}
	var listed []string
	for user, err := range fake.Users(t.Context()) {
		if err != nil {
			t.Fatal(err)
		}
		listed = append(listed, user.UID)
	}
	if !slices.Equal(listed, []string{"a", "c"}) {
		t.Errorf("Users = %v, want a and c", listed)
	}
	for range fake.Users(t.Context()) {
		break
	}
	if got := fake.Removed(); !slices.Equal(got, []string{"b", "gone"}) {
		t.Errorf("Removed = %v", got)
	}

	fake.FailRemoveUser()
	if err := fake.RemoveUser(t.Context(), "a"); !errors.Is(err, authntest.ErrFailed) {
		t.Errorf("RemoveUser after FailRemoveUser = %v, want ErrFailed", err)
	}
	fake.FailUsers()
	for _, err := range fake.Users(t.Context()) {
		if !errors.Is(err, authntest.ErrFailed) {
			t.Errorf("Users after FailUsers = %v, want ErrFailed", err)
		}
	}
}
