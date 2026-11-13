package learning

import (
	"encoding/base64"
	"slices"
	"testing"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/store"
)

// A mastered skill has its band, its cautious probability and its review date,
// or its review due; an anchor skill of each holding, with the modules
// offered whatever it is, and each reason, read in the contract's words.
func TestAMasteredSkillHasItsReview(t *testing.T) {
	t.Parallel()

	at := time.Unix(1_800_000_000, 0)
	read := learnermodel.SkillReadout{EvidenceCount: 6, LastPractice: at.Unix(), Estimate: 2, Uncertainty: 0.1, Retention: 0.9,
		Mastered: true, MasteryBand: learnermodel.BandRef{Place: 1, ID: "grades-1-2"}, CautiousProbability: 0.8,
		Review: learnermodel.ReviewDate{At: at.Add(time.Hour).Unix()}}
	got := skillState("comparing", read)
	if got.GetMasteryBand() != "grades-1-2" || got.GetCautiousProbability() != 0.8 || got.GetReviewDue() ||
		!got.GetReviewDate().AsTime().Equal(at.Add(time.Hour)) || got.GetEstimate() != 2 || got.GetEvidenceCount() != 6 {
		t.Errorf("a mastered skill = %v", got)
	}
	read.Review = learnermodel.ReviewDate{Already: true}
	if due := skillState("comparing", read); !due.GetReviewDue() || due.GetReviewDate() != nil {
		t.Errorf("a skill due for review = %v", due)
	}

	for status, want := range map[learnermodel.AnchorStatus]v1.AnchorSkillHolding{
		learnermodel.AnchorHas: v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_HELD, learnermodel.AnchorHasNot: v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_NOT_HELD,
		learnermodel.AnchorUnknown: v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_UNKNOWN,
	} {
		got := anchorState(learnermodel.AnchorReadout{Anchor: "core.multiplication-table", Status: status}, []string{"example/young"})
		if got.GetHolding() != want || !slices.Equal(got.GetTeachingModules(), []string{"example/young"}) {
			t.Errorf("the holding of %v = %v, offering %v; want %v, offering example/young", status, got.GetHolding(), got.GetTeachingModules(), want)
		}
	}
	for reason, want := range map[learnermodel.AnchorReason]v1.AnchorSkillReason{
		learnermodel.AnchorReasonNone:             v1.AnchorSkillReason_ANCHOR_SKILL_REASON_UNSPECIFIED,
		learnermodel.AnchorReasonNoTeacher:        v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NOBODY_TEACHES,
		learnermodel.AnchorReasonNoVerdictTeacher: v1.AnchorSkillReason_ANCHOR_SKILL_REASON_ONLY_UNASSESSABLE,
	} {
		if got := anchorState(learnermodel.AnchorReadout{Status: learnermodel.AnchorUnknown, Reason: reason}, nil); got.GetReason() != want {
			t.Errorf("the reason %v = %v, want %v", reason, got.GetReason(), want)
		}
	}

	// A module with no graph has no skills on its map.
	if m := skillMap(formatv0.CanonicalView{}, "sha256:x", learnermodel.Readout{}, nil); len(m.GetSkills()) != 0 || m.GetPackageDigest() != "sha256:x" {
		t.Errorf("the map of a module with no graph = %v", m)
	}
}

// A page token is the cursor it was made of, and nothing else is one.
func TestAPageTokenIsItsCursor(t *testing.T) {
	t.Parallel()

	c := store.Cursor{StartedAt: time.UnixMicro(1_800_000_000_123_456).UTC(), ID: "0b8f6c1e-2a4d-4d6b-9c55-6f2a8e7d1c3b"}
	if got, err := cursorOf(tokenOf(c)); err != nil || got == nil || *got != c {
		t.Errorf("the cursor of its token = %v, %v; want %v", got, err, c)
	}
	if got, err := cursorOf(""); got != nil || err != nil {
		t.Errorf("no token = %v, %v; want the first page", got, err)
	}
}

// Whatever app gives back as a page token, cursorOf does not panic, and the
// cursor it reads is one whose token reads back to it, of a time between 1970
// and the year 9999, as the service gives one.
func FuzzCursorOf(f *testing.F) {
	f.Add(tokenOf(store.Cursor{StartedAt: time.UnixMicro(1_800_000_000_123_456).UTC(), ID: "0b8f6c1e-2a4d-4d6b-9c55-6f2a8e7d1c3b"}))
	for _, seed := range []string{
		"", "%%", "bm90LWEtdG9rZW4", "MTIzL25vdC1hbi1pZA",
		base64.RawURLEncoding.EncodeToString([]byte("-1/0b8f6c1e-2a4d-4d6b-9c55-6f2a8e7d1c3b")),
		base64.RawURLEncoding.EncodeToString([]byte("+0/0b8f6c1e-2a4d-4d6b-9c55-6f2a8e7d1c3b")),
		base64.RawURLEncoding.EncodeToString([]byte("9223372036854775807/0B8F6C1E-2A4D-4D6B-9C55-6F2A8E7D1C3B")),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, token string) {
		c, err := cursorOf(token)
		if err != nil || c == nil {
			return
		}
		if c.StartedAt.Before(time.Unix(0, 0)) || c.StartedAt.Year() > 9999 {
			t.Fatalf("cursorOf(%q) = %v, out of 1970 to 9999", token, c)
		}
		if again, err := cursorOf(tokenOf(*c)); err != nil || again == nil || *again != *c {
			t.Fatalf("cursorOf(%q) = %v; its token reads to %v, %v", token, c, again, err)
		}
	})
}
