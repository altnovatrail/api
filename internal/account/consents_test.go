package account_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/authz"
)

func (w *world) askConsent(t *testing.T, ctx context.Context, key string, age int32, country string) (*v1.ChildConsent, error) {
	t.Helper()
	res, err := w.client.RequestChildConsent(ctx, &v1.RequestChildConsentRequest{
		IdempotencyKey: key, AgeYears: ptr(age), Country: country,
	})
	return res.GetConsent(), err
}

// confirm confirms a consent, as the operator will in epic E4.
func (w *world) confirm(t *testing.T, consentID string) {
	t.Helper()
	if _, err := w.pool.Exec(t.Context(), `UPDATE consents SET state = 'confirmed', confirmed_at = now(), method = 'synthetic'
		WHERE id = $1`, consentID); err != nil {
		t.Fatal(err)
	}
}

func TestAConsentForAChildWaitsWithItsCategoryAndCountryAlone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	for _, tc := range []struct {
		age      int32
		category string
	}{{0, "under_13"}, {12, "under_13"}, {13, "from_consent_to_18"}, {17, "from_consent_to_18"}} {
		got, err := w.askConsent(t, ctx, "key-"+tc.category+string(rune('a'+tc.age)), tc.age, "US")
		if err != nil {
			t.Fatal(err)
		}
		want := &v1.ChildConsent{ConsentId: got.GetConsentId(), AgeCategory: tc.category, Country: "US",
			State: v1.ConsentState_CONSENT_STATE_WAITING}
		if !proto.Equal(got, want) {
			t.Errorf("a consent at %d = %v, want %v", tc.age, got, want)
		}
	}
	account, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil || len(account.GetAccount().GetChildConsents()) != 4 || len(account.GetAccount().GetProfiles()) != 0 {
		t.Errorf("GetAccount = %v, %v; want the four consents and no profile", account, err)
	}
}

func TestARepeatOfAConsentIsTheSameConsent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US", "CA")
	ctx := w.owner(t, "uid-owner")
	first, err := w.askConsent(t, ctx, "key", 9, "US")
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.askConsent(t, ctx, "key", 9, "US")
	if err != nil || !proto.Equal(again, first) {
		t.Errorf("the repeat = %v, %v; want the first consent, %v", again, err, first)
	}
	// Another age or country under the key is another request.
	_, err = w.askConsent(t, ctx, "key", 10, "US")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	_, err = w.askConsent(t, ctx, "key", 9, "CA")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	if n := w.count(t, "consents"); n != 1 {
		t.Errorf("consents = %d, want one", n)
	}
}

func TestAConsentOfNoFormOrCountryIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	for _, age := range []int32{-1, 18, 40} {
		_, err := w.askConsent(t, ctx, "k", age, "US")
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	_, err := w.client.RequestChildConsent(ctx, &v1.RequestChildConsentRequest{IdempotencyKey: "k", Country: "US"})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.askConsent(t, ctx, "k", 9, "us")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = w.askConsent(t, ctx, "", 9, "US")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	for _, country := range []string{"CA", "GB"} {
		_, err = w.askConsent(t, ctx, "k-"+country, 9, country)
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_COUNTRY_NOT_ACCEPTED)
	}
	if n := w.count(t, "consents"); n != 0 {
		t.Errorf("consents = %d, want none", n)
	}
}

// The age in years lives in the request alone: neither the consent nor the
// key's record holds it, and a fast hash of the request would give it back.
func TestTheAgeInYearsIsKeptNowhere(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	if _, err := w.askConsent(t, ctx, "key", 11, "US"); err != nil {
		t.Fatal(err)
	}
	var consent, fingerprint string
	// Every column but the IDs and the times, which hold digits of their own.
	if err := w.pool.QueryRow(t.Context(), `SELECT (to_jsonb(c) - 'id' - 'account_id' - 'requested_at')::text FROM consents c`).Scan(&consent); err != nil {
		t.Fatal(err)
	}
	if err := w.pool.QueryRow(t.Context(), `SELECT fingerprint FROM idempotency_keys`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(consent, "11") || !strings.HasPrefix(fingerprint, "$argon2id$") {
		t.Errorf("consent %s, fingerprint %s; want no age and a slow fingerprint", consent, fingerprint)
	}
}

func TestAChildsProfileIsMadeByItsConfirmedConsent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	consent, err := w.askConsent(t, ctx, "key", 8, "US")
	if err != nil {
		t.Fatal(err)
	}
	create := func(settings *v1.ProfileSettings) (*v1.Profile, error) {
		res, err := w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: consent.GetConsentId(), Settings: settings})
		return res.GetProfile(), err
	}
	_, err = create(&v1.ProfileSettings{Pseudonym: ptr("Fox")})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)

	w.confirm(t, consent.GetConsentId())
	_, err = create(&v1.ProfileSettings{})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	got, err := create(&v1.ProfileSettings{Pseudonym: ptr("Fox")})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	want := &v1.Profile{
		ProfileId: got.GetProfileId(), Pseudonym: "Fox", AgeCategory: "under_13", Country: "US",
		PresentationStyle: &v1.PresentationStyle{Voicing: &v1.Voicing{Enabled: &on, Speed: v1.VoicingSpeed_VOICING_SPEED_NORMAL}},
		DefaultMode:       v1.Mode_MODE_BY_STEPS, AskBeforeShowing: true,
	}
	if !proto.Equal(got, want) {
		t.Errorf("CreateChildProfile = %v\nwant %v", got, want)
	}
	var accountID string
	if err := w.pool.QueryRow(t.Context(), `SELECT account_id FROM consents WHERE profile_id = $1`, got.GetProfileId()).Scan(&accountID); err != nil {
		t.Fatalf("the consent is not bound to the profile: %v", err)
	}
	if can, err := w.rights.Can(t.Context(), accountID, authz.CanLearn, got.GetProfileId()); err != nil || !can {
		t.Errorf("can_learn = %v, %v; want the tuple written", can, err)
	}
	// Its repeat is told it is there; the account no longer gives the consent.
	_, err = create(&v1.ProfileSettings{Pseudonym: ptr("Fox")})
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
	account, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil || len(account.GetAccount().GetProfiles()) != 1 || len(account.GetAccount().GetChildConsents()) != 0 {
		t.Errorf("GetAccount = %v, %v; want the profile and no consent waiting", account, err)
	}
	// A child's setting is the adult's to change.
	res, err := w.client.UpdateProfile(ctx, &v1.UpdateProfileRequest{ProfileId: got.GetProfileId(),
		Settings: &v1.ProfileSettings{AskBeforeShowing: ptr(false)}})
	if err != nil || res.GetProfile().GetAskBeforeShowing() {
		t.Errorf("UpdateProfile of ask before showing = %v, %v; want it off", res, err)
	}
}

func TestAConsentOfAnotherOrOfNoneIsNotFound(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	other := w.owner(t, "uid-other")
	theirs, err := w.askConsent(t, other, "key", 8, "US")
	if err != nil {
		t.Fatal(err)
	}
	w.confirm(t, theirs.GetConsentId())
	ctx := w.owner(t, "uid-owner")
	for _, id := range []string{theirs.GetConsentId(), "00000000-0000-0000-0000-000000000000", strings.ToUpper(theirs.GetConsentId()), "not-a-uuid"} {
		_, err := w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: id, Settings: &v1.ProfileSettings{Pseudonym: ptr("Fox")}})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}
}

func TestAChildsTupleNotWrittenLeavesNoProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	consent, err := w.askConsent(t, ctx, "key", 8, "US")
	if err != nil {
		t.Fatal(err)
	}
	w.confirm(t, consent.GetConsentId())
	w.ownerFails.Store(true)
	_, err = w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: consent.GetConsentId(),
		Settings: &v1.ProfileSettings{Pseudonym: ptr("Fox")}})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	var bound bool
	if err := w.pool.QueryRow(t.Context(), `SELECT profile_id IS NOT NULL FROM consents`).Scan(&bound); err != nil || bound || w.count(t, "profiles") != 0 {
		t.Errorf("bound %v, %v, profiles %d; want the transaction rolled back", bound, err, w.count(t, "profiles"))
	}
}

// consentsLimit is the consents for a child that may wait under an account
// (the 2026-11-09 amendment to R98 spec; R36): 10, the author's number.
const consentsLimit = account.WaitingConsentsMax

// Consents wait up to the limit: one more is refused as a form, with no Refusal
// until spec's epic E4 gives the contract its cause, and a repeat at the limit
// is still the same consent. A confirmed consent and another account's do not
// count.
func TestConsentsWaitUpToTheLimit(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	var last *v1.ChildConsent
	for i := range consentsLimit + 1 {
		consent, err := w.askConsent(t, ctx, fmt.Sprintf("key-%d", i), 8, "US")
		if err != nil {
			t.Fatalf("consent %d: %v", i, err)
		}
		if i == 0 {
			w.confirm(t, consent.GetConsentId())
		}
		last = consent
	}
	_, err := w.askConsent(t, ctx, "key-over", 8, "US")
	if refusal := refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED); refusal != nil {
		t.Errorf("the refusal over the limit carries %v, want none", refusal)
	}
	again, err := w.askConsent(t, ctx, fmt.Sprintf("key-%d", consentsLimit), 8, "US")
	if err != nil || !proto.Equal(again, last) {
		t.Errorf("the repeat at the limit = %v, %v; want the same consent, %v", again, err, last)
	}
	other := w.owner(t, "uid-other")
	if _, err := w.askConsent(t, other, "key-0", 8, "US"); err != nil {
		t.Errorf("another account's consent: %v", err)
	}
	if n := w.count(t, "consents WHERE state = 'waiting'"); n != consentsLimit+1 {
		t.Errorf("consents waiting = %d, want %d", n, consentsLimit+1)
	}
}

// Requests beside each other at the limit go one after the other on the
// account's row: one of them takes the last place, and the rest are refused.
func TestConsentsAskedTogetherKeepToTheLimit(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	for i := range consentsLimit - 1 {
		if _, err := w.askConsent(t, ctx, fmt.Sprintf("key-%d", i), 8, "US"); err != nil {
			t.Fatal(err)
		}
	}
	const together = 5
	errs := make(chan error, together)
	var wg sync.WaitGroup
	for i := range together {
		wg.Go(func() {
			_, err := w.askConsent(t, as(t, "uid-owner"), fmt.Sprintf("key-together-%d", i), 8, "US")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	taken := 0
	for err := range errs {
		if err == nil {
			taken++
			continue
		}
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	if n := w.count(t, "consents"); taken != 1 || n != consentsLimit {
		t.Errorf("%d took a place and %d consents wait, want 1 and %d", taken, n, consentsLimit)
	}
}
