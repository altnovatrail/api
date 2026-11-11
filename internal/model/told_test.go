package model_test

import (
	"testing"

	"github.com/altnovatrail/api/internal/store"
)

// What an attempt's rows tell once its package reads them: its items of its
// answers, the traps found in its wrong values, the task's techniques and its
// leading question; with no package in the image, only the items (Д27).
func TestAnAttemptsRowsTellItsFacts(t *testing.T) {
	t.Parallel()

	m := base(t)
	wrong := [][]byte{answer(1, []slot{{"not_yet", "", "five"}}), answer(1, []slot{{"unreadable", "illegible", ""}}),
		answer(1, []slot{{"correct", "", "three and four"}})}
	o := store.Owed{PackageDigest: string(current.Digest), TaskID: "pick", Answers: wrong}
	told, err := m.Tell(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !told.Read || len(told.Items) != 1 || !told.Items[0].HasFirstVerdict || len(told.Traps) != 1 {
		t.Errorf("Tell = %+v, want one item, its trap, the package read", told)
	}
	for name, o := range map[string]store.Owed{
		"no package":    {PackageDigest: "sha256:" + string(make([]byte, 0)), TaskID: "pick", Answers: wrong},
		"no task":       {PackageDigest: string(current.Digest), TaskID: "none", Answers: wrong},
		"no submission": {PackageDigest: "sha256:none", TaskID: "pick"},
	} {
		told, err := m.Tell(t.Context(), o)
		if err != nil || told.Read || len(told.Traps) != 0 || len(told.Techniques) != 0 {
			t.Errorf("%s: Tell = %+v, %v; want the items alone", name, told, err)
		}
	}
	if _, err := m.Tell(t.Context(), store.Owed{Answers: [][]byte{{0xff}}}); err == nil {
		t.Error("an answer that is no message: nil error")
	}
	picked := 99
	if _, err := m.Tell(t.Context(), store.Owed{PackageDigest: string(current.Digest), TaskID: "greater", Instance: &picked}); err == nil {
		t.Error("an instance a template has not: nil error")
	}
}
