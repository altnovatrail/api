package model_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/store/storetest"
)

// correct settles the learner and appends a correction of the level of the
// module of pkg to its band 1, at at.
func (l learner) correct(t *testing.T, m *model.Model, pkg *modulepackage.Package, at time.Time) model.Settled {
	t.Helper()
	var out model.Settled
	if err := pgx.BeginFunc(t.Context(), l.pool, func(tx pgx.Tx) error {
		s, err := m.Settle(t.Context(), tx, l.profile, at)
		if err != nil {
			return err
		}
		out, err = m.Correct(t.Context(), tx, l.profile, s, pkg, learnermodel.BandRef{Place: 1, ID: "grades-1-2"}, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// A correction of the level goes to the journal and to the cache at once, and
// the state recomputed of the journal is the state appended; a module with no
// evidence before it has its level in the band set; a correction on a version
// withdrawn for a wrong key is folded all the same; and the levels are the last
// correction of each module that decodes (R89).
func TestACorrectionIsFoldedAndCached(t *testing.T) {
	t.Parallel()

	m := base(t)
	pool := storetest.DB(t)
	l := newLearner(t, pool, 1)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	s := l.correct(t, m, current, at)
	read, err := m.Read(s, at, []string{"example/choices"})
	if err != nil || len(read.Modules) != 1 || !read.Modules[0].HasLevelBand || read.Modules[0].LevelBand.ID != "grades-1-2" {
		t.Fatalf("the level after the correction = %+v, %v; want grades-1-2", read.Modules, err)
	}
	appended := l.cache(t)
	if appended.LastSeq != 1 || s.LastSeq != 1 {
		t.Errorf("the cache took %d, the state %d; want the correction, 1", appended.LastSeq, s.LastSeq)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET learner_model_version = 'v0.1.3' WHERE profile_id = $1`, l.profile); err != nil {
		t.Fatal(err)
	}
	l.settle(t, m)
	if recomputed := l.cache(t); !bytes.Equal(recomputed.State, appended.State) {
		t.Error("the state recomputed of the journal differs from the state appended")
	}

	// The image after this one wrote the cache: the correction goes to the
	// journal, and the cache stays as that image wrote it.
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET modules_release = 'v10.0.0' WHERE profile_id = $1`, l.profile); err != nil {
		t.Fatal(err)
	}
	if s := l.correct(t, m, current, at.Add(time.Minute)); s.LastSeq != 2 || l.cache(t).LastSeq != 1 {
		t.Errorf("under a newer cache: the state took %d, the cache %d; want 2 and 1", s.LastSeq, l.cache(t).LastSeq)
	}

	w := newLearner(t, pool, 1)
	if s := w.correct(t, m, earlier, at); len(s.State.Modules) != 1 {
		t.Errorf("a correction on a version withdrawn for a wrong key gives the state %+v, want its module", s.State)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE skill_states SET learner_model_version = 'v0.1.3' WHERE profile_id = $1`, w.profile); err != nil {
		t.Fatal(err)
	}
	if recomputed := w.settle(t, m); len(recomputed.State.Modules) != 1 {
		t.Errorf("the state recomputed = %+v, want the correction folded", recomputed.State)
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO evidence (profile_id, seq, correction_id, package_digest, module_id, evidence)
		VALUES ($1, 3, gen_random_uuid(), $2, 'example/choices', '\xff')`, l.profile, string(current.Digest)); err != nil {
		t.Fatal(err)
	}
	levels, err := model.Levels(t.Context(), pool, l.profile, "")
	if err != nil || len(levels) != 1 || levels[0].Module != "example/choices" || levels[0].Band != "grades-1-2" ||
		!levels[0].SetAt.Equal(at.Add(time.Minute)) {
		t.Errorf("the levels = %+v, %v; want the last that decodes, at %v", levels, err, at.Add(time.Minute))
	}
	if journal, err := store.Journal(t.Context(), pool, l.profile, 0); err != nil || !slices.Equal(model.LevelsOf(journal), levels) {
		t.Errorf("the levels of the journal read = %+v, want %+v", model.LevelsOf(journal), levels)
	}
}
