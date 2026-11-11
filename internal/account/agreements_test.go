package account_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/forms"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/store"
)

// texts is the current version of each of ids, as a request names them.
func texts(ids ...string) []*v1.LegalTextRef { return legal.Spec().Refs(ids...) }

func text(id string) *v1.LegalTextRef { return texts(id)[0] }

// An account is made on the current versions of the policy and the terms, which
// it records; a text left out, or a version not current, is refused (R136 spec).
func TestAnAccountIsMadeOnTheCurrentTexts(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.signIn(t, "uid-owner")
	code := w.code(t)
	policy := text(legal.PrivacyPolicy)
	old := &v1.LegalTextRef{Id: legal.PrivacyPolicy, Version: policy.GetVersion() - 1}
	for _, refs := range [][]*v1.LegalTextRef{nil, {policy}, {old, text(legal.TermsOfUse)}} {
		_, err := w.client.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code, AdultStated: true, AcceptedTexts: refs})
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
	}
	if _, err := w.client.CreateAccount(ctx, &v1.CreateAccountRequest{InviteCode: code, AdultStated: true,
		AcceptedTexts: texts(legal.TermsOfUse, legal.PrivacyPolicy)}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(t, "account_texts"); n != 2 {
		t.Errorf("account_texts = %d, want the two texts", n)
	}
}

// askChild asks the consent for a child of age in the country.
func (w *world) askChild(t *testing.T, ctx context.Context, key string, age int32, country string) *v1.ChildConsent {
	t.Helper()
	c, err := w.askConsent(t, ctx, key, age, country)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// childProfile creates the profile of the consent.
func (w *world) childProfile(t *testing.T, ctx context.Context, consentID string) *v1.Profile {
	t.Helper()
	res, err := w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: consentID,
		Settings: &v1.ProfileSettings{Pseudonym: ptr("Fox")}})
	if err != nil {
		t.Fatal(err)
	}
	return res.GetProfile()
}

// A learner from the age of digital consent of their country consents
// themselves: the profile is made at once and waits for the consent, which the
// learner gives to the current teen-consent, once; a learner below it does not
// (R116 spec).
func TestALearnerFromTheAgeConsentsThemselves(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	if c := teen.GetConsent(); c.GetGiver() != v1.ConsentGiver_CONSENT_GIVER_LEARNER || c.GetNeed() != v1.ConsentNeed_CONSENT_NEED_OWN {
		t.Errorf("the teen's profile has %v, want the learner's own consent needed", c)
	}
	_, err := w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: teen.GetProfileId()})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
	for range 2 {
		res, err := w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: teen.GetProfileId(), Text: text(legal.TeenConsent)})
		if err != nil || res.GetProfile().GetConsent().GetNeed() != v1.ConsentNeed_CONSENT_NEED_NONE {
			t.Fatalf("GiveOwnConsent = %v, %v; want no consent needed", res, err)
		}
	}
	var method string
	var version int32
	if err := w.pool.QueryRow(t.Context(), `SELECT method, text_version FROM consents WHERE profile_id = $1`,
		teen.GetProfileId()).Scan(&method, &version); err != nil || method != store.ConsentByLearner || version != text(legal.TeenConsent).GetVersion() {
		t.Errorf("the consent is %s of version %d, %v; want the learner's own to teen-consent", method, version, err)
	}

	child := w.askChild(t, ctx, "k-child", 9, "US")
	_, err = w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: child.GetConsentId(),
		Settings: &v1.ProfileSettings{Pseudonym: ptr("Owl")}})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	owner := w.ownerProfile(t, ctx)
	_, err = w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: owner.GetProfileId(), Text: text(legal.TeenConsent)})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	_, err = w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: "00000000-0000-0000-0000-000000000000"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

// A parent's consent goes by the scan of its signed form, kept in the store of
// the forms and not in the database; the operator confirms it by its code, and
// a new scan takes the place of one not accepted (R116 spec).
func TestAParentsConsentGoesByItsForm(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	c := w.askChild(t, ctx, "k", 9, "US")
	send := func(key string, form []byte, media string) (*v1.ChildConsent, error) {
		res, err := w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: c.GetConsentId(),
			IdempotencyKey: key, Form: form, MediaType: media})
		return res.GetConsent(), err
	}
	for _, tc := range []struct {
		form  []byte
		media string
	}{{nil, "image/png"}, {make([]byte, 10<<20+1), "image/png"}, {[]byte("scan"), "image/gif"}} {
		_, err := send("k-bad", tc.form, tc.media)
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	_, err := w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: "not an ID", IdempotencyKey: "k", Form: []byte("x"), MediaType: "image/png"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	got, err := send("k-1", []byte("first scan"), "image/png")
	if err != nil || got.GetForm() != v1.FormState_FORM_STATE_SENT {
		t.Fatalf("SendConsentForm = %v, %v; want the form sent", got, err)
	}
	if again, err := send("k-1", []byte("first scan"), "image/png"); err != nil || again.GetForm() != v1.FormState_FORM_STATE_SENT {
		t.Errorf("the repeat = %v, %v; want the same consent", again, err)
	}
	if err := store.JudgeForm(t.Context(), w.pool, c.GetRequestCode(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := send("k-2", []byte("second scan"), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	var object string
	if err := w.pool.QueryRow(t.Context(), `SELECT object FROM consent_forms WHERE consent_id = $1`, c.GetConsentId()).Scan(&object); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(w.forms, object)); err != nil || string(data) != "second scan" {
		t.Errorf("the scan of the consent = %q, %v; want the second, in the place of the first", data, err)
	}
	// The scan before, and that of the repeat, which its record answered, go today.
	if n := w.count(t, "consent_forms WHERE consent_id IS NULL AND keep_until = (now() AT TIME ZONE 'UTC')::date"); n != 2 {
		t.Errorf("scans to erase today = %d, want the first and the repeat's", n)
	}
	if err := store.JudgeForm(t.Context(), w.pool, c.GetRequestCode(), true); err != nil {
		t.Fatal(err)
	}
	_, err = send("k-3", []byte("third scan"), "image/jpeg")
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
	p := w.childProfile(t, ctx, c.GetConsentId())
	if p.GetConsent().GetGiver() != v1.ConsentGiver_CONSENT_GIVER_PARENT || p.GetConsent().GetNeed() != v1.ConsentNeed_CONSENT_NEED_NONE {
		t.Errorf("the profile has %v, want a parent's consent, confirmed", p.GetConsent())
	}
	// A learner's own consent has no form to send.
	teen := w.askChild(t, ctx, "k-teen", 15, "US")
	_, err = w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: teen.GetConsentId(), IdempotencyKey: "k-4",
		Form: []byte("scan"), MediaType: "image/png"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
}

// With no store of the forms, the scan is refused as unavailable.
func TestNoStoreOfTheFormsIsUnavailable(t *testing.T) {
	t.Parallel()

	w := newWorldWith(t, nil)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	c := w.askChild(t, ctx, "k", 9, "US")
	_, err := w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: c.GetConsentId(), IdempotencyKey: "k",
		Form: []byte("scan"), MediaType: "image/png"})
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("SendConsentForm = %v, want unavailable", err)
	}
	if _, err := w.client.ExportAccount(ctx, &v1.ExportAccountRequest{}); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("ExportAccount with no export = %v, want unimplemented", err)
	}
	if _, err := w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: w.ownerProfile(t, ctx).GetProfileId()}); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("ExportProfile with no export = %v, want unimplemented", err)
	}
}

// A consent with no profile is taken back: one that waits leaves nothing, and
// its scan goes the same day; one confirmed leaves its minimal record, and its
// scan is kept 5 years (R117 spec). A repeat is "not found".
func TestAConsentIsTakenBack(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	waiting := w.askChild(t, ctx, "k-1", 9, "US")
	if _, err := w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: waiting.GetConsentId(),
		IdempotencyKey: "k-form", Form: []byte("scan"), MediaType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	confirmed := w.askChild(t, ctx, "k-2", 9, "US")
	w.confirm(t, confirmed.GetConsentId())
	for _, id := range []string{waiting.GetConsentId(), confirmed.GetConsentId()} {
		if _, err := w.client.CancelChildConsent(ctx, &v1.CancelChildConsentRequest{ConsentId: id}); err != nil {
			t.Fatal(err)
		}
		_, err := w.client.CancelChildConsent(ctx, &v1.CancelChildConsentRequest{ConsentId: id})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}
	_, err := w.client.CancelChildConsent(ctx, &v1.CancelChildConsentRequest{ConsentId: "nope"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if n := w.count(t, "consents WHERE subject = 'child'"); n != 0 {
		t.Errorf("consents = %d, want none", n)
	}
	if n := w.count(t, "child_consent_records"); n != 1 {
		t.Errorf("minimal records = %d, want the confirmed one's", n)
	}
	if n := w.count(t, "consent_forms WHERE consent_id IS NULL AND keep_until = (now() AT TIME ZONE 'UTC')::date"); n != 1 {
		t.Errorf("forms to erase today = %d, want the waiting one's", n)
	}
}

// A learner withdraws their own consent, which deletes their profile and
// leaves its minimal record; at a profile whose consent an adult gives, it is
// "adult needed" (R132 spec).
func TestALearnerWithdrawsTheirOwnConsent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	if _, err := w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: teen.GetProfileId(), Text: text(legal.TeenConsent)}); err != nil {
		t.Fatal(err)
	}
	owner := w.ownerProfile(t, ctx)
	_, err := w.client.WithdrawOwnConsent(ctx, &v1.WithdrawOwnConsentRequest{ProfileId: owner.GetProfileId()})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	if _, err := w.client.WithdrawOwnConsent(ctx, &v1.WithdrawOwnConsentRequest{ProfileId: teen.GetProfileId()}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(t, "profiles WHERE NOT owner"); n != 0 {
		t.Errorf("profiles = %d, want the teen's gone", n)
	}
	if n := w.count(t, "child_consent_records WHERE method = 'own_consent'"); n != 1 {
		t.Errorf("minimal records = %d, want the learner's", n)
	}
	_, err = w.client.WithdrawOwnConsent(ctx, &v1.WithdrawOwnConsentRequest{ProfileId: teen.GetProfileId()})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

// The age given again works out the category anew: below the country's age a
// parent's consent is asked by a new code, from it the learner's own; the
// owner's profile gives its country alone (R134 spec).
func TestTheAgeIsGivenAgain(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US", "CA")
	ctx := w.owner(t, "uid-owner")
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	restate := func(key, profileID string, age *int32, country string) (*v1.Profile, error) {
		res, err := w.client.RestateAge(ctx, &v1.RestateAgeRequest{ProfileId: profileID, IdempotencyKey: key, AgeYears: age,
			Country: country, ParentConsentText: text(legal.ParentConsent)})
		return res.GetProfile(), err
	}
	p, err := restate("k-1", teen.GetProfileId(), ptr(int32(12)), "US")
	if err != nil || p.GetAgeCategory() != "under_13" || p.GetConsent().GetNeed() != v1.ConsentNeed_CONSENT_NEED_PARENT ||
		len(p.GetConsent().GetWaiting().GetRequestCode()) != 11 {
		t.Fatalf("RestateAge to 12 = %v, %v; want a parent's consent asked by a code", p, err)
	}
	if again, err := restate("k-1", teen.GetProfileId(), ptr(int32(12)), "US"); err != nil ||
		again.GetConsent().GetWaiting().GetRequestCode() != p.GetConsent().GetWaiting().GetRequestCode() {
		t.Errorf("the repeat = %v, %v; want the same profile", again, err)
	}
	p, err = restate("k-2", teen.GetProfileId(), ptr(int32(16)), "US")
	if err != nil || p.GetAgeCategory() != "from_consent_to_18" || p.GetConsent().GetNeed() != v1.ConsentNeed_CONSENT_NEED_OWN {
		t.Errorf("RestateAge to 16 = %v, %v; want the learner's own consent asked", p, err)
	}
	owner := w.ownerProfile(t, ctx)
	_, err = restate("k-3", owner.GetProfileId(), ptr(int32(16)), "US")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	if p, err := restate("k-4", owner.GetProfileId(), nil, "CA"); err != nil || p.GetCountry() != "CA" {
		t.Errorf("the owner's country = %v, %v; want CA", p, err)
	}
	_, err = restate("k-5", teen.GetProfileId(), nil, "US")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = restate("k-6", teen.GetProfileId(), ptr(int32(16)), "MX")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_COUNTRY_NOT_ACCEPTED)
	for _, bad := range []*v1.RestateAgeRequest{
		{ProfileId: teen.GetProfileId(), IdempotencyKey: "k-7", AgeYears: ptr(int32(18)), Country: "US", ParentConsentText: text(legal.ParentConsent)},
		{ProfileId: teen.GetProfileId(), IdempotencyKey: "k-8", AgeYears: ptr(int32(16)), Country: "usa", ParentConsentText: text(legal.ParentConsent)},
	} {
		_, err = w.client.RestateAge(ctx, bad)
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	_, err = w.client.RestateAge(ctx, &v1.RestateAgeRequest{ProfileId: teen.GetProfileId(), IdempotencyKey: "k-9",
		AgeYears: ptr(int32(16)), Country: "US"})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
}

// The age of a learner who consents themselves is asked again once their
// country raised its age above the one the category was given by (R134 spec).
func TestARaisedAgeAsksTheAgeAgain(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	if _, err := w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: teen.GetProfileId(), Text: text(legal.TeenConsent)}); err != nil {
		t.Fatal(err)
	}
	// Austria's age is 14: a category given by 13 there is asked again.
	if _, err := w.pool.Exec(t.Context(), `UPDATE consents SET applied = 13, country = 'AT' WHERE profile_id = $1`, teen.GetProfileId()); err != nil {
		t.Fatal(err)
	}
	account, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range account.GetAccount().GetProfiles() {
		if p.GetProfileId() == teen.GetProfileId() && p.GetConsent().GetNeed() != v1.ConsentNeed_CONSENT_NEED_AGE {
			t.Errorf("the teen's profile has %v, want the age needed", p.GetConsent())
		}
	}
}

// The storage of works is switched on by whoever consents for the learner —
// the adult, or a learner who consents themselves — to the current
// storage-consent, and off with its date (R113 spec).
func TestTheStorageOfWorks(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx)
	set := func(profileID string, action v1.Action, on bool, ref *v1.LegalTextRef) (*v1.Profile, error) {
		res, err := w.client.SetWorkStorage(ctx, &v1.SetWorkStorageRequest{ProfileId: profileID, Action: action, Enabled: on, Text: ref})
		return res.GetProfile(), err
	}
	_, err := set(owner.GetProfileId(), v1.Action_ACTION_UNSPECIFIED, true, text(legal.StorageConsent))
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	_, err = set(owner.GetProfileId(), v1.Action_ACTION_ADULT, true, nil)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
	_, err = set(owner.GetProfileId(), v1.Action_ACTION_LEARNING, true, text(legal.StorageConsent))
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	for range 2 {
		p, err := set(owner.GetProfileId(), v1.Action_ACTION_ADULT, true, text(legal.StorageConsent))
		if err != nil || !p.GetWorkStorage() {
			t.Fatalf("SetWorkStorage on = %v, %v; want it on", p, err)
		}
	}
	for range 2 {
		p, err := set(owner.GetProfileId(), v1.Action_ACTION_ADULT, false, nil)
		if err != nil || p.GetWorkStorage() {
			t.Fatalf("SetWorkStorage off = %v, %v; want it off", p, err)
		}
	}
	if n := w.count(t, "work_storage_consents WHERE giver = 'owner' AND off_at IS NOT NULL AND text_version IS NOT NULL"); n != 1 {
		t.Errorf("records of the storage = %d, want one, on and off", n)
	}
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	_, err = set(teen.GetProfileId(), v1.Action_ACTION_ADULT, true, text(legal.StorageConsent))
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_LEARNER_DECIDES)
	_, err = set(teen.GetProfileId(), v1.Action_ACTION_LEARNING, true, text(legal.StorageConsent))
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	if _, err := w.client.GiveOwnConsent(ctx, &v1.GiveOwnConsentRequest{ProfileId: teen.GetProfileId(), Text: text(legal.TeenConsent)}); err != nil {
		t.Fatal(err)
	}
	if p, err := set(teen.GetProfileId(), v1.Action_ACTION_LEARNING, true, text(legal.StorageConsent)); err != nil || !p.GetWorkStorage() {
		t.Errorf("the teen's storage on = %v, %v; want it on", p, err)
	}
	_, err = set("00000000-0000-0000-0000-000000000000", v1.Action_ACTION_ADULT, true, text(legal.StorageConsent))
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

// The export gives api.zip of a profile and of the account, by the version of
// the part, and a learner's own only at a profile whose learner consents
// themselves (R132, R135 spec).
func TestTheExportIsGiven(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx)
	names := func(part []byte) map[string]bool {
		t.Helper()
		r, err := zip.NewReader(bytes.NewReader(part), int64(len(part)))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, f := range r.File {
			got[f.Name] = true
		}
		return got
	}
	profile, err := w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: owner.GetProfileId()})
	if err != nil || profile.GetExportFormatVersion() != "1" {
		t.Fatalf("ExportProfile = %v, %v", profile, err)
	}
	if got := names(profile.GetPart()); !got["profiles/"+owner.GetProfileId()+".json"] || got["account.json"] ||
		!got["proto/novatrail/learnermodel/v1/state.proto"] {
		t.Errorf("the export of the profile holds %v", got)
	}
	account, err := w.client.ExportAccount(ctx, &v1.ExportAccountRequest{})
	if err != nil || !names(account.GetPart())["account.json"] {
		t.Errorf("ExportAccount = %v, %v; want the account's file", account, err)
	}
	_, err = w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: owner.GetProfileId(), Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	teen := w.childProfile(t, ctx, w.askChild(t, ctx, "k-teen", 15, "US").GetConsentId())
	if _, err := w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: teen.GetProfileId(), Action: v1.Action_ACTION_LEARNING}); err != nil {
		t.Errorf("the learner's own export = %v", err)
	}
	_, err = w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: "00000000-0000-0000-0000-000000000000"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

// What the requests of the consent refuse beyond their own: a request for a
// child's consent that names no parent-consent, a store of the forms that fails,
// a profile not the caller's or of no ID.
func TestTheRefusalsOfTheConsents(t *testing.T) {
	t.Parallel()

	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorldWith(t, []account.Option{account.WithForms(forms.Dir(blocked))})
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	_, err := w.client.RequestChildConsent(ctx, &v1.RequestChildConsentRequest{IdempotencyKey: "k", AgeYears: ptr(int32(9)), Country: "US"})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_TEXT_NOT_CURRENT)
	c := w.askChild(t, ctx, "k-1", 9, "US")
	if _, err := w.client.SendConsentForm(ctx, &v1.SendConsentFormRequest{ConsentId: c.GetConsentId(), IdempotencyKey: "k-2",
		Form: []byte("scan"), MediaType: "image/png"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("a store that fails = %v, want the failure", err)
	}
	other := w.ownerProfile(t, w.owner(t, "uid-other"))
	_, err = w.client.SetWorkStorage(ctx, &v1.SetWorkStorageRequest{ProfileId: other.GetProfileId(), Action: v1.Action_ACTION_ADULT, Enabled: true})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	for _, id := range []string{"not an ID", other.GetProfileId()} {
		_, err = w.client.WithdrawOwnConsent(ctx, &v1.WithdrawOwnConsentRequest{ProfileId: id})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	}
	_, err = w.client.ExportProfile(ctx, &v1.ExportProfileRequest{ProfileId: other.GetProfileId(), Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}
