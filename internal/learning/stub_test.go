package learning_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// newStubWorld is the world of the dev mode (R66): the gate of the stub, which
// admits an adult's handwriting and words at the author's stage with no key of
// the platform, and the stub, which reads and words in place of tutor.
func newStubWorld(t *testing.T) (*world, *admission.Gate) {
	t.Helper()
	facts := filepath.Join(t.TempDir(), "facts.yaml")
	if err := os.WriteFile(facts, []byte(adultFacts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.AdmissionFrom([]string{"API_TUTOR_STUB=1", "API_ADMISSION_RUNG=author", "API_ADMISSION_FACTS_FILE=" + facts})
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := admission.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))}), g
}

// The path of the check, the hint and the words runs on the stub, with no
// provider (R66): a square crop is illegible, a tall crop reads a wrong value,
// the rung's words are the stub's and pass the checks, "explain differently"
// gives other words, and a wide crop reads the right value and solves the task.
// Each call spends what tutor expects it to cost, the reserve it took, so
// nothing is left in reserve; the log names the model stub.
func TestThePathRunsOnTheStub(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "apples").GetAttemptId()
	submit := func(width, height int) *v1.SubmitWorkResponse {
		t.Helper()
		res, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a,
			IdempotencyKey: uuid.NewString(), Work: sheet(pngOf(t, width, height))})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := submit(10, 10); verdicts(res) != "unreadable[unreadable illegible]" || res.GetWrongAnswers() != 0 {
		t.Fatalf("a square crop = %s, %d wrong; want illegible, no wrong answer", verdicts(res), res.GetWrongAnswers())
	}
	if res := submit(10, 40); !strings.HasPrefix(verdicts(res), "not_yet") || res.GetWrongAnswers() != 1 {
		t.Fatalf("a tall crop = %s, %d wrong; want not yet, one wrong", verdicts(res), res.GetWrongAnswers())
	}
	wrong := int32(1)
	words, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, Work: sheet(pngOf(t, 10, 40))})
	if err != nil {
		t.Fatal(err)
	}
	if words.GetWords() == "" || words.GetWords() == firstRung || words.GetOutcome() != nil {
		t.Errorf("the rung's words = %v, want the stub's question, not the fallback", words)
	}
	other, err := w.explain(l, l.adult, a, 1)
	if err != nil {
		t.Fatal(err)
	}
	if other.GetWords() == "" || other.GetWords() == words.GetWords() || other.GetOutcome() != nil {
		t.Errorf("explained differently = %v, want other words of the stub than %q", other, words.GetWords())
	}
	if res := submit(40, 10); verdicts(res) != "correct[correct 7]" || res.GetSolved() == nil {
		t.Errorf("a wide crop = %s, solved %v; want correct and solved", verdicts(res), res.GetSolved())
	}

	adult := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategoryAdult}
	sheetPrice := quota.USD(g.Price(tutor.CallSheet, adult).ExpectedUSD)
	wordsPrice := quota.USD(g.Price(tutor.CallWords, adult).ExpectedUSD)
	want := quota.Counter{Spent: 3*sheetPrice + 2*wordsPrice}
	if profile, account := w.counted(t, l.accountID, l.adult); sheetPrice == 0 || wordsPrice == 0 || profile != want || account != want {
		t.Errorf("the counters = %+v, %+v; want %+v, the expected prices, and nothing in reserve", profile, account, want)
	}
	log := w.log.String()
	if !strings.Contains(log, `"model":"stub"`) || strings.Contains(log, "words_check") || strings.Contains(log, "claude-") {
		t.Errorf("the log = %s, want the stub's usage, no words turned down and no model of Claude", log)
	}
}
