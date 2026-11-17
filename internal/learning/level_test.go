package learning_test

import (
	"bytes"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/packages/packagestest"
	"github.com/altnovatrail/api/internal/rpc"
)

// threeBands is the module choices for grades 1 to 6, with three bands, 1–2,
// 3–4 and 5–6, its tasks all in the first.
func threeBands(path string, data []byte) []byte {
	if path == "manifest.yaml" {
		data = bytes.Replace(data, []byte("  grades: [1, 2]\n"), []byte("  grades: [1, 2, 3, 4, 5, 6]\n"), 1)
		data = bytes.Replace(data, []byte("    grades: [1, 2]\n"), []byte("    grades: [1, 2]\n"+
			"  - id: grades-3-4\n    title: Grades 3–4\n    grades: [3, 4]\n"+
			"  - id: grades-5-6\n    title: Grades 5–6\n    grades: [5, 6]\n"), 1)
	}
	return data
}

// bandedWorld is the world of the module choices with three bands, current.
func bandedWorld(t *testing.T) *world {
	t.Helper()
	pkg := packagestest.OwnEdited(t, "choices", "1.0.0", threeBands)
	if len(pkg.View.Manifest.Bands) != 3 {
		t.Fatalf("the module has %d bands, want 3", len(pkg.View.Manifest.Bands))
	}
	set := packagestest.Set(t, packagestest.Entry{Pkg: pkg, Release: "v0.2.0"})
	w := worldOn(t, set, gate(t, "", config.RungFull), nil)
	w.current, w.connected = pkg, []string{module}
	return w
}

// corrections is how many corrections of a level the profile's journal holds.
func (w *world) corrections(t *testing.T, profile string) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM evidence WHERE profile_id = $1 AND correction_id IS NOT NULL`,
		profile).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// An adult sets a module's level a band at a time, easier or harder, from the
// band the skill map gives, and the model takes it at once: the map gives the
// new band and when the adult set it, and the progress the last correction of
// each module, connected or not since. A band named that is the level's
// appends nothing and answers as the level is, and any band further off is
// refused (R89; R157 spec).
func TestAnAdultSetsTheLevelABandAtATime(t *testing.T) {
	t.Parallel()

	w := bandedWorld(t)
	l := w.learner(t)
	// A learner of grade 3 starts a band below grades 3–4: in grades 1–2.
	w.exec(t, `UPDATE profiles SET grade = 3 WHERE id = $1`, l.child)
	set := func(profile, band string) (*v1.SetModuleLevelResponse, error) {
		return w.progress.SetModuleLevel(l.ctx, &v1.SetModuleLevelRequest{ProfileId: profile, ModuleId: module, Band: band})
	}
	level := func(action v1.Action) *v1.GetSkillMapResponse {
		t.Helper()
		got, err := w.progress.GetSkillMap(l.ctx, &v1.GetSkillMapRequest{ProfileId: l.child, ModuleId: module, Action: action})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := level(v1.Action_ACTION_LEARNING); got.GetLevelBand() != "grades-1-2" || got.LevelSetAt != nil {
		t.Errorf("the level of a learner of grade 3 = %q set at %v, want grades-1-2 set by no adult", got.GetLevelBand(), got.LevelSetAt)
	}

	for _, band := range []string{"grades-5-6", "grades-7-8"} {
		_, err := set(l.child, band)
		refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_BAND_NOT_NEXT)
	}
	_, err := set(l.child, "")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	same, err := set(l.child, "grades-1-2")
	if err != nil || same.GetLevelBand() != "grades-1-2" || same.LevelSetAt != nil || w.corrections(t, l.child) != 0 {
		t.Errorf("the band of the level named = %v, %v, %d corrections; want the level as it is and none", same, err, w.corrections(t, l.child))
	}

	steps := []struct {
		band string
		at   *timestamppb.Timestamp
	}{{band: "grades-3-4"}, {band: "grades-5-6"}, {band: "grades-3-4"}}
	for i := range steps {
		w.clock.move(time.Hour)
		before := w.clock.now().Truncate(time.Second)
		got, err := set(l.child, steps[i].band)
		if err != nil {
			t.Fatalf("%s: %v", steps[i].band, err)
		}
		steps[i].at = got.GetLevelSetAt()
		if at := got.GetLevelSetAt().AsTime(); got.GetLevelBand() != steps[i].band || at.Before(before) || at.Nanosecond() != 0 {
			t.Errorf("the level set in %s = %v, want it in that band now, in whole seconds", steps[i].band, got)
		}
		// A repeat after an answer lost changes nothing.
		again, err := set(l.child, steps[i].band)
		if err != nil || again.GetLevelBand() != steps[i].band || !again.GetLevelSetAt().AsTime().Equal(got.GetLevelSetAt().AsTime()) {
			t.Errorf("the repeat of %s = %v, %v; want %v", steps[i].band, again, err, got)
		}
		if n := w.corrections(t, l.child); n != i+1 {
			t.Errorf("after %d corrections and their repeats the journal holds %d", i+1, n)
		}
	}
	last := steps[len(steps)-1].at.AsTime()
	for _, action := range []v1.Action{v1.Action_ACTION_LEARNING, v1.Action_ACTION_ADULT} {
		if got := level(action); got.GetLevelBand() != "grades-3-4" || !got.GetLevelSetAt().AsTime().Equal(last) {
			t.Errorf("the map of %v = %q set at %v, want grades-3-4 set at %v", action, got.GetLevelBand(), got.GetLevelSetAt(), last)
		}
	}

	// The level moves on by the evidence after a correction.
	w.solve(t, l, l.child, "five")
	if got := level(v1.Action_ACTION_LEARNING); got.GetLevelBand() == "" || !got.GetLevelSetAt().AsTime().Equal(last) {
		t.Errorf("the map after an attempt = %q set at %v, want a band and the same correction", got.GetLevelBand(), got.GetLevelSetAt())
	}

	// The progress gives the last correction, the module disconnected since too.
	w.exec(t, `DELETE FROM connected_modules WHERE profile_id = $1`, l.child)
	progress, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.child})
	if err != nil {
		t.Fatal(err)
	}
	if s := progress.GetLevelSettings(); len(s) != 1 || s[0].GetModuleId() != module || s[0].GetBand() != "grades-3-4" ||
		!s[0].GetSetAt().AsTime().Equal(last) {
		t.Errorf("the corrections in the progress = %v, want %s in grades-3-4 at %v", s, module, last)
	}
	_, err = set(l.child, "grades-3-4")
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	if progress, err := w.progress.GetProgress(l.ctx, &v1.GetProgressRequest{ProfileId: l.adult}); err != nil || len(progress.GetLevelSettings()) != 0 {
		t.Errorf("the progress of a profile with no correction = %v, %v", progress.GetLevelSettings(), err)
	}
}

// Below the lowest band, where a learner of its grades starts, the only band
// to name is the lowest, and no band comes before it: the owner's profile too
// (R89).
func TestBelowTheLowestBandOnlyTheLowestIsNext(t *testing.T) {
	t.Parallel()

	w := bandedWorld(t)
	l := w.learner(t)
	w.exec(t, `UPDATE profiles SET grade = 1 WHERE id = $1`, l.adult)
	got, err := w.progress.GetSkillMap(l.ctx, &v1.GetSkillMapRequest{ProfileId: l.adult, ModuleId: module, Action: v1.Action_ACTION_LEARNING})
	if err != nil || got.GetLevelBand() != "" {
		t.Fatalf("the level of a learner of grade 1 = %q, %v; want below the lowest band", got.GetLevelBand(), err)
	}
	set := func(band string) (*v1.SetModuleLevelResponse, error) {
		return w.progress.SetModuleLevel(l.ctx, &v1.SetModuleLevelRequest{ProfileId: l.adult, ModuleId: module, Band: band})
	}
	_, err = set("grades-3-4")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_BAND_NOT_NEXT)
	lowest, err := set("grades-1-2")
	if err != nil || lowest.GetLevelBand() != "grades-1-2" || lowest.LevelSetAt == nil {
		t.Fatalf("the lowest band = %v, %v", lowest, err)
	}
	_, err = set("grades-5-6")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_BAND_NOT_NEXT)
	if n := w.corrections(t, l.adult); n != 1 {
		t.Errorf("the journal holds %d corrections, want 1", n)
	}
}

// The correction is refused as a learning request is — a profile without
// consent, one not of the account —, and a module not connected or with no
// current version, as the skill map refuses it (R89).
func TestACorrectionIsRefusedAsTheSkillMapIs(t *testing.T) {
	t.Parallel()

	w := bandedWorld(t)
	l := w.learner(t)
	set := func(profile, module string) error {
		_, err := w.progress.SetModuleLevel(l.ctx, &v1.SetModuleLevelRequest{ProfileId: profile, ModuleId: module, Band: "grades-1-2"})
		return err
	}
	refusedAs(t, set(w.child(t, l, "under_13", false), module), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CONSENT)
	refusedAs(t, set(l.child, "example/none"), connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	w.exec(t, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, 'example/none')`, l.child)
	refusedAs(t, set(l.child, "example/none"), connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
	if err := set(uuid.NewString(), module); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("a profile not the account's = %v, want not found", err)
	}
}

// A correction on a database that fails is unavailable, wherever it fails: the
// settling, the reading of the corrections or their appending (R89).
func TestACorrectionOnADatabaseThatFailsIsUnavailable(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{"FROM connected_modules", "UPDATE skill_states", "correction_id IS NOT NULL AND",
		"INSERT INTO evidence (profile_id, seq, correction_id"} {
		t.Run(failOn, func(t *testing.T) {
			t.Parallel()

			built(t)
			w := worldOf(t, gate(t, "", config.RungFull), nil, func(p *pgxpool.Pool) rpc.DB { return &failing{Pool: p, failOn: failOn} })
			l := w.learner(t)
			_, err := w.progress.SetModuleLevel(l.ctx, &v1.SetModuleLevelRequest{ProfileId: l.child, ModuleId: module, Band: "grades-1-2"})
			if connect.CodeOf(err) != connect.CodeUnavailable {
				t.Errorf("the correction = %v, want unavailable", err)
			}
		})
	}
}
