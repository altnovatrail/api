package account_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
)

// keyWorld is the world whose own key's requests ask a gate at the stage rung,
// with no facts and no key of the platform, which the own key needs none of.
func keyWorld(t *testing.T, rung string) *world {
	t.Helper()
	gate, _, err := admission.Load(config.Admission{Rung: rung})
	if err != nil {
		t.Fatal(err)
	}
	return newWorldWith(t, []account.Option{account.WithOwnKeys(gate)})
}

// childOf is a profile of the account, by a consent confirmed of a learner of
// age, as old, in the US.
func (w *world) childOf(t *testing.T, ctx context.Context, key string, age int32) string {
	t.Helper()
	consent, err := w.askConsent(t, ctx, key, age, "US")
	if err != nil {
		t.Fatal(err)
	}
	w.confirm(t, consent.GetConsentId())
	child, err := w.client.CreateChildProfile(ctx, &v1.CreateChildProfileRequest{ConsentId: consent.GetConsentId(),
		Settings: &v1.ProfileSettings{Pseudonym: ptr("Fox")}})
	if err != nil {
		t.Fatal(err)
	}
	return child.GetProfile().GetProfileId()
}

func (w *world) ownKeyProviders(t *testing.T, ctx context.Context, profileID string) []*v1.OwnKeyProvider {
	t.Helper()
	res, err := w.client.ListOwnKeyProviders(ctx, &v1.ListOwnKeyProvidersRequest{ProfileId: profileID})
	if err != nil {
		t.Fatalf("ListOwnKeyProviders = %v", err)
	}
	return res.GetProviders()
}

// At the stage author the owner's own key of Anthropic is open, by the code and
// the name of the table, and nobody's below 18, with no refusal (R72).
func TestTheProvidersOfTheOwnKeyAreTheAdultsAtTheStageAuthor(t *testing.T) {
	t.Parallel()

	w := keyWorld(t, config.RungAuthor)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	got := w.ownKeyProviders(t, ctx, owner)
	if want := []*v1.OwnKeyProvider{{Provider: "anthropic", Name: "Anthropic"}}; len(got) != 1 || !proto.Equal(got[0], want[0]) {
		t.Errorf("the owner's providers = %v, want %v", got, want)
	}
	for name, age := range map[string]int32{"a child": 8, "a teen": 15} {
		if got := w.ownKeyProviders(t, ctx, w.childOf(t, ctx, "key-"+name, age)); len(got) != 0 {
			t.Errorf("the providers of %s = %v, want none", name, got)
		}
	}
}

// At the stage full no model is admitted, by an own key either: the list is
// empty.
func TestNoProviderOfTheOwnKeyIsOpenAtTheStageFull(t *testing.T) {
	t.Parallel()

	w := keyWorld(t, config.RungFull)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	if got := w.ownKeyProviders(t, ctx, w.ownerProfile(t, ctx).GetProfileId()); len(got) != 0 {
		t.Errorf("the providers at full = %v, want none", got)
	}
}

// Another account's profile, and one that is not there, are refused "no
// profile" by the rule of can_manage, before the handler; so is one gone since
// the checks, by the handler; and with no gate the method is unimplemented.
func TestTheProvidersOfAProfileNotTheCallersAreRefused(t *testing.T) {
	t.Parallel()

	w := keyWorld(t, config.RungAuthor)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	other := w.owner(t, "uid-other")
	_, err := w.client.ListOwnKeyProviders(other, &v1.ListOwnKeyProvidersRequest{ProfileId: owner})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = w.client.ListOwnKeyProviders(ctx, &v1.ListOwnKeyProvidersRequest{ProfileId: "00000000-0000-4000-8000-000000000000"})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	// A tuple whose profile is gone — dangling — gives nothing.
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.ListOwnKeyProviders(ctx, &v1.ListOwnKeyProvidersRequest{ProfileId: owner})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)

	bare := newWorld(t)
	bare.allow(t, "US")
	ctx = bare.owner(t, "uid-owner")
	_, err = bare.client.ListOwnKeyProviders(ctx, &v1.ListOwnKeyProvidersRequest{ProfileId: bare.ownerProfile(t, ctx).GetProfileId()})
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("ListOwnKeyProviders with no gate = %v, want unimplemented", err)
	}
}
