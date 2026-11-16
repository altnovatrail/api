package account_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/account"
	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
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

// countedKeys is the gate whose checks of a key it counts.
type countedKeys struct {
	*admission.Gate
	checks atomic.Int32
}

func (c *countedKeys) CheckKey(ctx context.Context, category formatv0.AgeCategory, provider, key string) (tutor.KeyCheck, error) {
	c.checks.Add(1)
	return c.Gate.CheckKey(ctx, category, provider, key)
}

// stubKeyWorld is the world whose own key's requests ask the gate of the dev
// mode's stub at the stage author (R66), which checks a key by its ending, and
// counts its checks.
func stubKeyWorld(t *testing.T) (*world, *countedKeys) {
	t.Helper()
	gate, _, err := admission.Load(config.Admission{Rung: config.RungAuthor, Stub: true})
	if err != nil {
		t.Fatal(err)
	}
	keys := &countedKeys{Gate: gate}
	return newWorldWith(t, []account.Option{account.WithOwnKeys(keys)}), keys
}

// The check of the owner's key gives each of the contract's five ends through
// tutor's check, on the stub's endings (R73): a provider that gave no answer is
// no refusal but OWN_KEY_CHECK_UNSPECIFIED with CAUSE_AI_FAILURE; a provider
// closed to the key — OpenAI, one the table does not know — is PROVIDER_CLOSED.
// The log line says how it ended, and holds neither the key nor its provider.
func TestTheOwnersKeyIsCheckedByTutor(t *testing.T) {
	t.Parallel()

	w, _ := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	const key = "sk-ant-synthetic-7f3a"
	for name, tc := range map[string]struct {
		provider, key string
		want          v1.OwnKeyCheck
		outcome       v1.Cause
		logged        string
	}{
		"accepted":       {"anthropic", key, v1.OwnKeyCheck_OWN_KEY_CHECK_ACCEPTED, v1.Cause_CAUSE_UNSPECIFIED, logging.KeyCheckAccepted},
		"invalid":        {"anthropic", key + admission.StubKeyInvalid, v1.OwnKeyCheck_OWN_KEY_CHECK_INVALID, v1.Cause_CAUSE_UNSPECIFIED, logging.KeyCheckInvalid},
		"no permission":  {"anthropic", key + admission.StubKeyNoPerm, v1.OwnKeyCheck_OWN_KEY_CHECK_NO_PERMISSION, v1.Cause_CAUSE_UNSPECIFIED, logging.KeyCheckNoPermission},
		"no answer":      {"anthropic", key + admission.StubKeySilent, v1.OwnKeyCheck_OWN_KEY_CHECK_UNSPECIFIED, v1.Cause_CAUSE_AI_FAILURE, logging.KeyCheckUnanswered},
		"OpenAI":         {"openai", key, v1.OwnKeyCheck_OWN_KEY_CHECK_PROVIDER_CLOSED, v1.Cause_CAUSE_UNSPECIFIED, logging.KeyCheckProviderClosed},
		"no table knows": {"acme", key, v1.OwnKeyCheck_OWN_KEY_CHECK_PROVIDER_CLOSED, v1.Cause_CAUSE_UNSPECIFIED, logging.KeyCheckProviderClosed},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(w.log.String())
			res, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner,
				OwnKey: &v1.OwnKey{Provider: tc.provider, Key: tc.key}})
			if err != nil {
				t.Fatalf("CheckOwnKey = %v", err)
			}
			if res.GetCheck() != tc.want || res.GetOutcome().GetCause() != tc.outcome {
				t.Errorf("CheckOwnKey = %v, %v, want %v, %v", res.GetCheck(), res.GetOutcome().GetCause(), tc.want, tc.outcome)
			}
			// This request's line alone: the lines before it are the other cases'.
			if line := w.log.String()[before:]; !strings.Contains(line, `"key_check":"`+tc.logged+`"`) {
				t.Errorf("no request line says the check ended %s:\n%s", tc.logged, line)
			}
		})
	}
	log := w.log.String()
	if strings.Contains(log, "sk-ant") || strings.Contains(log, "7f3a") || strings.Contains(log, "anthropic") ||
		strings.Contains(log, "openai") || strings.Contains(log, "acme") || strings.Contains(log, owner) {
		t.Errorf("the log holds the key, its provider or the profile:\n%s", log)
	}
	if !strings.Contains(log, `"outcome_cause":"CAUSE_AI_FAILURE"`) {
		t.Errorf("no request line names the outcome of a provider that gave no answer:\n%s", log)
	}
}

// A key short of its provider or itself is invalid_argument, and a key to a
// profile not of the category adult CAUSE_KEY_NOT_FOR_PROFILE, both before any
// check; another account's profile, one not there and one gone are
// CAUSE_NO_PROFILE; with no gate the method is unimplemented.
func TestAKeyThatDoesNotFitIsRefusedWithNoCheck(t *testing.T) {
	t.Parallel()

	w, keys := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	whole := &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}
	for name, own := range map[string]*v1.OwnKey{
		"no own key":  nil,
		"no key":      {Provider: "anthropic"},
		"no provider": {Key: "sk-ant-synthetic"},
		"neither":     {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner, OwnKey: own})
			refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
		})
	}
	for name, age := range map[string]int32{"a child": 8, "a teen": 16} {
		_, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: w.childOf(t, ctx, "key-"+name, age), OwnKey: whole})
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	other := w.owner(t, "uid-other")
	_, err := w.client.CheckOwnKey(other, &v1.CheckOwnKeyRequest{ProfileId: owner, OwnKey: whole})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	if _, err := w.pool.Exec(t.Context(), `DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner, OwnKey: whole})
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	if n := keys.checks.Load(); n != 0 {
		t.Errorf("the refusals checked %d keys", n)
	}

	bare := newWorld(t)
	bare.allow(t, "US")
	ctx = bare.owner(t, "uid-owner")
	_, err = bare.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: bare.ownerProfile(t, ctx).GetProfileId(), OwnKey: whole})
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("CheckOwnKey with no gate = %v, want unimplemented", err)
	}
}

// The log's words of the check are the contract's ends, OwnKeyCheck, in its
// own words, with "unanswered" for OWN_KEY_CHECK_UNSPECIFIED.
func TestTheLogsWordsOfACheckAreTheContracts(t *testing.T) {
	t.Parallel()

	var want []string
	for n := range len(v1.OwnKeyCheck_name) {
		name := strings.ToLower(strings.TrimPrefix(v1.OwnKeyCheck_name[int32(n)], "OWN_KEY_CHECK_"))
		if name == "unspecified" {
			name = logging.KeyCheckUnanswered
		}
		want = append(want, name)
	}
	slices.Sort(want)
	got := slices.Sorted(slices.Values(logging.KeyChecks))
	if !slices.Equal(got, want) {
		t.Errorf("logging.KeyChecks = %v, want the contract's %v", got, want)
	}
}

// A request whose client leaves less than two seconds is deadline_exceeded,
// not a provider that gave no answer, as on the requests of an attempt.
func TestACheckWithTooLittleTimeIsTheClients(t *testing.T) {
	t.Parallel()

	w, _ := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	short, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	_, err := w.client.CheckOwnKey(short, &v1.CheckOwnKeyRequest{ProfileId: owner,
		OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}})
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Errorf("CheckOwnKey with 1.5 s left = %v, want deadline_exceeded", err)
	}
}

// No more than ten checks of a key fit in any hour on the account (R78): the
// eleventh is resource_exhausted with CAUSE_KEY_CHECKS_EXHAUSTED and when the
// next check opens, with no check, a provider closed with no call counts as a
// check, and the hour slides, so that a check opens as the oldest leaves it.
func TestNoMoreThanTenChecksOfAKeyFitInAnHour(t *testing.T) {
	t.Parallel()

	w, keys := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	check := func(provider string) error {
		_, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner,
			OwnKey: &v1.OwnKey{Provider: provider, Key: "sk-ant-synthetic"}})
		return err
	}
	start := w.clock.Now()
	exhausted := func(err error, opens time.Time) {
		t.Helper()
		r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_KEY_CHECKS_EXHAUSTED)
		if got, want := r.GetNextKeyCheck().AsTime(), opens.UTC().Truncate(time.Microsecond); !got.Equal(want) {
			t.Fatalf("next_key_check = %v, want %v", got, want)
		}
	}
	if err := check("anthropic"); err != nil {
		t.Fatalf("the first check = %v", err)
	}
	// The nine others at minute 59, OpenAI's, closed with no call, among them.
	w.clock.set(start.Add(59 * time.Minute))
	for i := range account.MaxKeyChecks - 1 {
		provider := "anthropic"
		if i%3 == 0 {
			provider = "openai"
		}
		if err := check(provider); err != nil {
			t.Fatalf("check %d = %v", i+2, err)
		}
	}
	// The first check leaves the window first.
	exhausted(check("anthropic"), start.Add(account.KeyCheckWindow))
	exhausted(check("openai"), start.Add(account.KeyCheckWindow))
	if n := keys.checks.Load(); n != account.MaxKeyChecks {
		t.Fatalf("the gate checked %d keys, want %d: a check beyond the limit reached it", n, account.MaxKeyChecks)
	}
	// An hour from the first, it has left the window, and one check opens.
	w.clock.set(start.Add(account.KeyCheckWindow))
	if err := check("anthropic"); err != nil {
		t.Fatalf("the check an hour after the first = %v", err)
	}
	exhausted(check("anthropic"), start.Add(59*time.Minute+account.KeyCheckWindow))
	// An hour from the nine, only the check of the last hour counts.
	w.clock.set(start.Add(59*time.Minute + account.KeyCheckWindow))
	for i := range account.MaxKeyChecks - 1 {
		if err := check("anthropic"); err != nil {
			t.Fatalf("check %d of the next hour = %v", i+1, err)
		}
	}
	exhausted(check("anthropic"), start.Add(2*account.KeyCheckWindow))
	if n, want := keys.checks.Load(), 2*account.MaxKeyChecks; n != int32(want) {
		t.Errorf("the gate checked %d keys, want %d", n, want)
	}
	// The limit is the account's: another account checks as it was.
	other := w.owner(t, "uid-other")
	_, err := w.client.CheckOwnKey(other, &v1.CheckOwnKeyRequest{ProfileId: w.ownerProfile(t, other).GetProfileId(),
		OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}})
	if err != nil {
		t.Errorf("another account's check = %v", err)
	}
}

// The times of the checks need not be in order — two instances may differ in
// their clocks —: the next check opens when the oldest leaves the hour.
func TestTheNextCheckOfAKeyOpensByTheOldest(t *testing.T) {
	t.Parallel()

	w, keys := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	start := w.clock.Now().UTC().Truncate(time.Microsecond)
	checked := []time.Time{start.Add(-10 * time.Minute), start.Add(-50 * time.Minute)}
	for range account.MaxKeyChecks - len(checked) {
		checked = append(checked, start.Add(-time.Minute))
	}
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET key_checked_at = $1`, checked); err != nil {
		t.Fatal(err)
	}
	_, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner,
		OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}})
	r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_KEY_CHECKS_EXHAUSTED)
	if got, want := r.GetNextKeyCheck().AsTime(), start.Add(10*time.Minute); !got.Equal(want) {
		t.Errorf("next_key_check = %v, want %v, an hour after the oldest", got, want)
	}
	if n := keys.checks.Load(); n != 0 {
		t.Errorf("the gate checked %d keys, want none", n)
	}
}

// Checks of one account side by side are counted one after the other: twelve
// at once give ten checks and two refusals, never an eleventh check.
func TestChecksOfAKeySideBySideGiveNoEleventh(t *testing.T) {
	t.Parallel()

	w, keys := stubKeyWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	var wg sync.WaitGroup
	var refused atomic.Int32
	for range account.MaxKeyChecks + 2 {
		wg.Go(func() {
			_, err := w.client.CheckOwnKey(as(t, "uid-owner"), &v1.CheckOwnKeyRequest{ProfileId: owner,
				OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}})
			switch {
			case connect.CodeOf(err) == connect.CodeResourceExhausted:
				refused.Add(1)
			case err != nil:
				t.Errorf("a check side by side = %v", err)
			}
		})
	}
	wg.Wait()
	if n, r := keys.checks.Load(), refused.Load(); n != account.MaxKeyChecks || r != 2 {
		t.Errorf("twelve checks at once = %d checked and %d refused, want %d and 2", n, r, account.MaxKeyChecks)
	}
}

// The dev mode's stub calls no provider, and with WithoutKeyCheckLimit the
// checks of a key have no limit there (R78).
func TestTheChecksOfAKeyOnTheStubHaveNoLimit(t *testing.T) {
	t.Parallel()

	gate, _, err := admission.Load(config.Admission{Rung: config.RungAuthor, Stub: true})
	if err != nil {
		t.Fatal(err)
	}
	w := newWorldWith(t, []account.Option{account.WithOwnKeys(gate), account.WithoutKeyCheckLimit()})
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	owner := w.ownerProfile(t, ctx).GetProfileId()
	for i := range account.MaxKeyChecks + 2 {
		_, err := w.client.CheckOwnKey(ctx, &v1.CheckOwnKeyRequest{ProfileId: owner,
			OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic"}})
		if err != nil {
			t.Fatalf("check %d on the stub = %v", i+1, err)
		}
	}
}
