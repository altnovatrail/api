package account_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/authz"
)

// allow takes learners from the countries, as the operator's command does.
func (w *world) allow(t *testing.T, countries ...string) {
	t.Helper()
	for _, c := range countries {
		if _, err := w.pool.Exec(t.Context(), `INSERT INTO accepted_countries (country) VALUES ($1)`, c); err != nil {
			t.Fatal(err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func (w *world) ownerProfile(t *testing.T, ctx context.Context) *v1.Profile {
	t.Helper()
	res, err := w.client.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{
		Country: "US", Settings: &v1.ProfileSettings{Pseudonym: ptr("Owl")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.GetProfile()
}

func TestTheOwnersProfileIsAnAdultsWithItsConsentAndTuple(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	got := w.ownerProfile(t, ctx)
	off := false
	want := &v1.Profile{
		ProfileId: got.GetProfileId(), Owner: true, Pseudonym: "Owl", AgeCategory: "adult", Country: "US",
		PresentationStyle: &v1.PresentationStyle{Voicing: &v1.Voicing{Enabled: &off, Speed: v1.VoicingSpeed_VOICING_SPEED_NORMAL}},
		DefaultMode:       v1.Mode_MODE_BY_STEPS,
		Consent:           &v1.ProfileConsent{Giver: v1.ConsentGiver_CONSENT_GIVER_OWNER, Need: v1.ConsentNeed_CONSENT_NEED_NONE},
	}
	if !proto.Equal(got, want) {
		t.Errorf("CreateOwnerProfile = %v\nwant %v", got, want)
	}
	var accountID, method, state, subject string
	var statedOn, adultOn string
	if err := w.pool.QueryRow(t.Context(), `SELECT p.account_id, c.method, c.state, c.subject,
			p.age_stated_on::text, a.adult_stated_at::date::text
		FROM profiles p JOIN consents c ON c.profile_id = p.id JOIN accounts a ON a.id = p.account_id
		WHERE p.id = $1`, got.GetProfileId()).Scan(&accountID, &method, &state, &subject, &statedOn, &adultOn); err != nil {
		t.Fatal(err)
	}
	if method != "owner_statement" || state != "confirmed" || subject != "adult" || statedOn != adultOn {
		t.Errorf("consent = %s %s %s, age stated on %s; want the owner's statement, confirmed, of the statement's date %s",
			method, state, subject, statedOn, adultOn)
	}
	if can, err := w.rights.Can(t.Context(), accountID, authz.CanManage, got.GetProfileId()); err != nil || !can {
		t.Errorf("can_manage = %v, %v; want the tuple written", can, err)
	}
	// The account gives it; a second owner's profile is there already, even of
	// a country no longer taken.
	account, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil || len(account.GetAccount().GetProfiles()) != 1 || !proto.Equal(account.GetAccount().GetProfiles()[0], want) {
		t.Errorf("GetAccount = %v, %v; want the profile", account, err)
	}
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM accepted_countries`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{Country: "US", Settings: &v1.ProfileSettings{Pseudonym: ptr("Elk")}})
	refusedAs(t, err, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
}

func TestACountryNotTakenTakesNoProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	for _, country := range []string{"CA", "DE"} {
		_, err := w.client.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{Country: country, Settings: &v1.ProfileSettings{Pseudonym: ptr("Owl")}})
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_COUNTRY_NOT_ACCEPTED)
	}
	for _, country := range []string{"", "us", "USA"} {
		_, err := w.client.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{Country: country, Settings: &v1.ProfileSettings{Pseudonym: ptr("Owl")}})
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	if n := w.count(t, "profiles"); n != 0 {
		t.Errorf("profiles = %d, want none", n)
	}
}

func (w *world) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestATupleNotWrittenLeavesNoProfile(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	w.ownerFails.Store(true)
	_, err := w.client.CreateOwnerProfile(ctx, &v1.CreateOwnerProfileRequest{Country: "US", Settings: &v1.ProfileSettings{Pseudonym: ptr("Owl")}})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
	if n, c := w.count(t, "profiles"), w.count(t, "consents"); n != 0 || c != 0 {
		t.Errorf("profiles %d, consents %d; want the transaction rolled back", n, c)
	}
}

func TestTheSettingsOfAProfileAreCheckedAndPartial(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	update := func(s *v1.ProfileSettings) (*v1.Profile, error) {
		res, err := w.client.UpdateProfile(ctx, &v1.UpdateProfileRequest{ProfileId: id, Settings: s})
		return res.GetProfile(), err
	}
	got, err := update(&v1.ProfileSettings{
		Pseudonym: ptr("  Snowy Owl  "), Avatar: ptr("owl-2"), InterfaceLanguage: ptr("sr-Latn-RS"), Grade: ptr[int32](7),
		DefaultMode: v1.Mode_MODE_SELF, Voicing: &v1.Voicing{Enabled: ptr(true), Speed: v1.VoicingSpeed_VOICING_SPEED_SLOW},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetPseudonym() != "Snowy Owl" || got.GetAvatar() != "owl-2" || got.GetInterfaceLanguage() != "sr-Latn-RS" ||
		got.GetGrade() != 7 || got.GetDefaultMode() != v1.Mode_MODE_SELF ||
		!got.GetPresentationStyle().GetVoicing().GetEnabled() || got.GetPresentationStyle().GetVoicing().GetSpeed() != v1.VoicingSpeed_VOICING_SPEED_SLOW {
		t.Fatalf("UpdateProfile = %v, want every field set", got)
	}
	// What is not sent stays; empty and 0 clear.
	got, err = update(&v1.ProfileSettings{Avatar: ptr(""), InterfaceLanguage: ptr(""), Grade: ptr[int32](0)})
	if err != nil || got.GetPseudonym() != "Snowy Owl" || got.GetAvatar() != "" || got.InterfaceLanguage != nil ||
		got.Grade != nil || got.GetDefaultMode() != v1.Mode_MODE_SELF {
		t.Errorf("UpdateProfile clearing = %v, %v; want the three cleared and the rest kept", got, err)
	}
	if got, err := update(nil); err != nil || got.GetPseudonym() != "Snowy Owl" {
		t.Errorf("UpdateProfile of nothing = %v, %v; want the profile as it is", got, err)
	}
	// A language the library does not know is still a well-formed tag.
	if got, err := update(&v1.ProfileSettings{InterfaceLanguage: ptr("qaa-x-private")}); err != nil || got.GetInterfaceLanguage() != "qaa-x-private" {
		t.Errorf("a well-formed unknown tag = %v, %v; want it kept", got, err)
	}
	for name, s := range map[string]*v1.ProfileSettings{
		"an empty pseudonym":    {Pseudonym: ptr("   ")},
		"a long pseudonym":      {Pseudonym: ptr(strings.Repeat("ё", 31))},
		"a long avatar":         {Avatar: ptr(strings.Repeat("a", 65))},
		"an avatar not ASCII":   {Avatar: ptr("сова")},
		"a tag of no form":      {InterfaceLanguage: ptr("en_US!")},
		"an underscore":         {InterfaceLanguage: ptr("en_US")},
		"a tag too long":        {InterfaceLanguage: ptr("en-x-" + strings.Repeat("abcdefgh-", 7) + "a")},
		"a NUL in a pseudonym":  {Pseudonym: ptr("Owl\x00")},
		"a line in a pseudonym": {Pseudonym: ptr("Snowy\nOwl")},
		"grade 13":              {Grade: ptr[int32](13)},
		"grade -1":              {Grade: ptr[int32](-1)},
		"a mode unknown":        {DefaultMode: v1.Mode(9)},
		"a speed unknown":       {Voicing: &v1.Voicing{Speed: v1.VoicingSpeed(9)}},
		"ask before an adult's": {AskBeforeShowing: ptr(true)},
	} {
		_, err := update(s)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if got, err := update(&v1.ProfileSettings{Pseudonym: ptr(strings.Repeat("ё", 30))}); err != nil || got.GetPseudonym() != strings.Repeat("ё", 30) {
		t.Errorf("a pseudonym of 30 code points = %v, %v; want it kept", got, err)
	}
}

func TestTheVoicingAloneIsSetFromATask(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	before := w.ownerProfile(t, ctx)
	res, err := w.client.SetVoicing(ctx, &v1.SetVoicingRequest{ProfileId: before.GetProfileId(),
		Voicing: &v1.Voicing{Enabled: ptr(true)}})
	if err != nil {
		t.Fatal(err)
	}
	want := proto.Clone(before).(*v1.Profile)
	want.PresentationStyle.Voicing.Enabled = ptr(true)
	if !proto.Equal(res.GetProfile(), want) {
		t.Errorf("SetVoicing = %v\nwant %v", res.GetProfile(), want)
	}
}

func TestAProfileThatIsNotThereIsNone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	// A tuple whose profile is gone — dangling — gives nothing.
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}
	_, err := w.client.UpdateProfile(ctx, &v1.UpdateProfileRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = w.client.SetVoicing(ctx, &v1.SetVoicingRequest{ProfileId: id})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
}

func TestNoPseudonymIsInTheLog(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	id := w.ownerProfile(t, ctx).GetProfileId()
	if _, err := w.client.UpdateProfile(ctx, &v1.UpdateProfileRequest{ProfileId: id, Settings: &v1.ProfileSettings{Pseudonym: ptr("Quokka")}}); err != nil {
		t.Fatal(err)
	}
	if log := w.log.String(); strings.Contains(log, "Owl") || strings.Contains(log, "Quokka") || strings.Contains(log, id) ||
		!strings.Contains(log, "UpdateProfile") {
		t.Errorf("the log %s holds a pseudonym or the profile's ID, or no line of the requests", log)
	}
}
