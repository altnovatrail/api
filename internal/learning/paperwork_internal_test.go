package learning

import (
	"strings"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/tutor"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/store"
)

// box is a box of the work from top to bottom, across the whole width.
func box(top, bottom int32) *observationv1.CropBox {
	return &observationv1.CropBox{Ymin: top, Ymax: bottom, Xmax: 1000}
}

// Each verdict of the Check is the verdict of its slot by the README's table:
// the value only of an answer, the box of what was read, and with no answer
// yet the box of the first mistake, the solution so far, or nothing at all.
func TestASlotOfTheCheckIsItsVerdict(t *testing.T) {
	t.Parallel()

	read, mistake := box(0, 100), box(40, 60)
	wrong := []tutor.CheckError{{Box: mistake, Kind: "arithmetic"}}
	for _, tc := range []struct {
		name                   string
		slot                   tutor.SlotChecked
		verdict, reason, value string
		box                    *observationv1.CropBox
	}{
		{"correct", tutor.SlotChecked{Verdict: "correct", Read: "85", ReadBox: read}, "correct", "", "85", read},
		{"incorrect", tutor.SlotChecked{Verdict: "incorrect", Read: "851", ReadBox: read, Errors: wrong}, "not_yet", "", "851", read},
		{"incorrect, nothing read", tutor.SlotChecked{Verdict: "incorrect"}, "not_yet", "", "", nil},
		{"unreadable", tutor.SlotChecked{Verdict: "unreadable", Read: "8?"}, "unreadable", "illegible", "", nil},
		{"no answer, a mistake", tutor.SlotChecked{Verdict: "no_answer", Read: "37 + 4 = 31", ReadBox: read, Errors: wrong}, "not_yet", "", "", mistake},
		{"no answer, a mistake with no place", tutor.SlotChecked{Verdict: "no_answer", Read: "37 + 4", ReadBox: read,
			Errors: []tutor.CheckError{{Kind: "rule"}}}, "not_yet", "", "", read},
		{"no answer, a solution so far", tutor.SlotChecked{Verdict: "no_answer", Read: "37 + 4", ReadBox: read}, "so_far_correct", "", "", read},
		{"no answer, nothing read", tutor.SlotChecked{Verdict: "no_answer", ReadBox: read}, "unreadable", "blank", "", nil},
	} {
		tc.slot.Task = 2
		v := slotVerdict(tc.slot)
		if v.GetVerdict() != tc.verdict || v.GetReason() != tc.reason || v.GetValue() != tc.value || (v.Value != nil) != (tc.value != "") ||
			!proto.Equal(v.GetCropBox(), tc.box) || v.GetAnswerSlot().GetItem() != 2 || v.GetAnswerSlot().GetSlot() != 1 {
			t.Errorf("%s = %v, want %s %s %q at %v", tc.name, v, tc.verdict, tc.reason, tc.value, tc.box)
		}
	}
}

// verdicts are the verdicts of the items of answer, in order.
func verdicts(answer *v1.SubmitWorkResponse) string {
	var out []string
	for _, item := range answer.GetItems() {
		out = append(out, item.GetVerdict())
	}
	return strings.Join(out, " ")
}

// The verdict of a Check: every task correct solves the attempt; a task not yet
// counts one wrong answer, however many are; one so far correct counts none
// and solves nothing; a Check that read nothing of a solution in any slot is
// CAUSE_NO_KEY, and one that gave no answer the outcome of why, each with
// every slot unchecked and no tutor's words: the slots the Check gave, else
// those it was told of, else one.
func TestThePaperVerdictOfACheck(t *testing.T) {
	t.Parallel()

	answered := tutor.Outcome{Kind: tutor.Answered}
	slot := func(n int, verdict, read string) tutor.SlotChecked {
		return tutor.SlotChecked{Task: n, Verdict: verdict, Read: read}
	}
	row := store.Attempt{WrongAnswers: 1}
	for _, tc := range []struct {
		name     string
		c        paperChecking
		verdicts string
		wrong    int32
		solved   bool
		cause    v1.Cause
		words    string
	}{
		{"all correct", paperChecking{check: tutor.PaperCheck{Tasks: 2}, checked: tutor.PaperChecked{Outcome: answered, TutorWords: "Well done.",
			Slots: []tutor.SlotChecked{slot(1, "correct", "85"), slot(2, "correct", "86")}}}, "correct correct", 1, true, 0, "Well done."},
		{"two not yet", paperChecking{check: tutor.PaperCheck{Tasks: 3}, checked: tutor.PaperChecked{Outcome: answered, TutorWords: "Look again.",
			Slots: []tutor.SlotChecked{slot(1, "incorrect", "1"), slot(2, "correct", "86"), slot(3, "incorrect", "2")}}},
			"not_yet correct not_yet", 2, false, 0, "Look again."},
		{"so far correct", paperChecking{check: tutor.PaperCheck{Tasks: 2}, checked: tutor.PaperChecked{Outcome: answered,
			Slots: []tutor.SlotChecked{slot(1, "correct", "85"), slot(2, "no_answer", "40 +")}}}, "correct so_far_correct", 1, false, 0, ""},
		{"blank", paperChecking{check: tutor.PaperCheck{Tasks: 2}, checked: tutor.PaperChecked{Outcome: answered, TutorWords: "Start here.",
			Slots: []tutor.SlotChecked{slot(1, "no_answer", ""), slot(2, "no_answer", "")}}}, "unchecked unchecked", 1, false, v1.Cause_CAUSE_NO_KEY, ""},
		{"blank, the tasks found", paperChecking{checked: tutor.PaperChecked{Outcome: answered,
			Slots: []tutor.SlotChecked{slot(1, "no_answer", ""), slot(2, "no_answer", ""), slot(3, "no_answer", "")}}},
			"unchecked unchecked unchecked", 1, false, v1.Cause_CAUSE_NO_KEY, ""},
		{"failed", paperChecking{check: tutor.PaperCheck{Tasks: 3}, checked: tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Failed}}},
			"unchecked unchecked unchecked", 1, false, v1.Cause_CAUSE_AI_FAILURE, ""},
		{"failed, told no task", paperChecking{checked: tutor.PaperChecked{Outcome: tutor.Outcome{Kind: tutor.Failed}}},
			"unchecked", 1, false, v1.Cause_CAUSE_AI_FAILURE, ""},
		{"over the quota", paperChecking{check: tutor.PaperCheck{}, reserve: reserve{refused: &v1.Outcome{Cause: v1.Cause_CAUSE_QUOTA}}},
			"unchecked", 1, false, v1.Cause_CAUSE_QUOTA, ""},
	} {
		answer := paperVerdict(row, tc.c)
		if got := verdicts(answer); got != tc.verdicts || answer.GetWrongAnswers() != tc.wrong || (answer.GetSolved() != nil) != tc.solved ||
			answer.GetOutcome().GetCause() != tc.cause || answer.GetTutorWords() != tc.words || answer.GetNext() != nil {
			t.Errorf("%s = %v, want %s, %d wrong, solved %v, %v, words %q", tc.name, answer, tc.verdicts, tc.wrong, tc.solved, tc.cause, tc.words)
		}
		for n, item := range answer.GetItems() {
			if place := item.GetAnswerSlots()[0].GetAnswerSlot(); place.GetItem() != int32(n+1) {
				t.Errorf("%s: item %d stands at %v", tc.name, n+1, place)
			}
		}
	}
}

// The wavy line of "Show where" goes under the first mistake of the first task
// the Check did not judge correct; none when that task has no mistake, even if
// a later one has.
func TestTheFirstMistakeIsOfTheFirstTaskNotCorrect(t *testing.T) {
	t.Parallel()

	mistake := func(b *observationv1.CropBox) []*paperv1.CheckError {
		return []*paperv1.CheckError{{Box: b}, {Box: box(900, 950)}}
	}
	for _, tc := range []struct {
		name  string
		check *paperv1.Check
		want  *observationv1.CropBox
	}{
		{"none kept", nil, nil},
		{"all correct", &paperv1.Check{Slots: []*paperv1.SlotCheck{{Verdict: "correct", Errors: mistake(box(0, 10))}}}, nil},
		{"past a correct one", &paperv1.Check{Slots: []*paperv1.SlotCheck{{Verdict: "correct", Errors: mistake(box(0, 10))},
			{Verdict: "incorrect", Errors: mistake(box(500, 600))}}}, box(500, 600)},
		{"the first with none", &paperv1.Check{Slots: []*paperv1.SlotCheck{{Verdict: "unreadable"},
			{Verdict: "incorrect", Errors: mistake(box(500, 600))}}}, nil},
	} {
		if got := firstMistake(tc.check); !proto.Equal(got, tc.want) {
			t.Errorf("%s: the first mistake = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A rung of the ladder kept is the model's words, or the fallback text tutor
// put in its place; past the ladder's end it is the last rung, and before the
// first wrong answer the first; with no ladder it is the rung's fallback text.
func TestARungOfTheLadderKept(t *testing.T) {
	t.Parallel()

	ladder := &paperv1.Check{Hints: []string{"What is asked?", tutor.PaperFallback(2, "en")}}
	for _, tc := range []struct {
		name     string
		check    *paperv1.Check
		rung     int
		language string
		want     store.Words
	}{
		{"the first", ladder, 1, "en", store.Words{Source: store.SourceModel, Text: "What is asked?"}},
		{"before a wrong answer", ladder, 0, "en", store.Words{Source: store.SourceModel, Text: "What is asked?"}},
		{"a fallback text", ladder, 2, "en", store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(2, "en")}},
		{"past the end", ladder, 3, "en", store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(2, "en")}},
		{"no ladder", nil, 3, "ru", store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(3, "ru")}},
	} {
		if got := ladderRung(tc.check, tc.rung, tc.language); got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// The opening question is the ladder's first rung when it is the model's, the
// tutor's words when it is a fallback text, that fallback text with no words,
// and the opening question's own fallback text with neither.
func TestTheOpeningWordsOfACheck(t *testing.T) {
	t.Parallel()

	fallback := tutor.PaperFallback(1, "en")
	for _, tc := range []struct {
		name  string
		hints []string
		words string
		want  store.Words
	}{
		{"the first rung", []string{"What is asked?", "Which step?"}, "Let us look.", store.Words{Source: store.SourceModel, Text: "What is asked?"}},
		{"the words", []string{fallback, "Which step?"}, "Let us look.", store.Words{Source: store.SourceModel, Text: "Let us look."}},
		{"the rung's fallback", []string{fallback, "Which step?"}, " ", store.Words{Source: store.SourceFallback, Text: fallback}},
		{"the words alone", nil, "Let us look.", store.Words{Source: store.SourceModel, Text: "Let us look."}},
		{"neither", nil, "", store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(openingRung, "en")}},
	} {
		if got := openingWords(tc.hints, tc.words, "en"); got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// The Check is told as many tasks as the last Check gave, at most 20, else
// none, whatever "Recognise" read; a kept Check this build cannot read fails.
func TestTheTasksOfAFrameAreThoseCountedSoFar(t *testing.T) {
	t.Parallel()

	marshal := func(m proto.Message) []byte {
		b, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	slots := make([]*paperv1.SlotCheck, 25)
	for i := range slots {
		slots[i] = &paperv1.SlotCheck{Task: int32(i + 1)}
	}
	three := marshal(&paperv1.Reading{Tasks: []*paperv1.ReadTask{{Statement: "1"}, {Statement: "2"}, {Statement: "3"}}})
	for _, tc := range []struct {
		name string
		row  store.Attempt
		want int
	}{
		{"by the Check", store.Attempt{PaperCheck: marshal(&paperv1.Check{Slots: slots[:2]}), PaperReading: three}, 2},
		{"not by the reading", store.Attempt{PaperReading: three}, 0},
		{"by none", store.Attempt{}, 0},
		{"at most 20", store.Attempt{PaperCheck: marshal(&paperv1.Check{Slots: slots})}, 20},
	} {
		if got, err := tasksOf(tc.row); err != nil || got != tc.want {
			t.Errorf("%s: %d tasks (%v), want %d", tc.name, got, err, tc.want)
		}
	}
	if _, err := tasksOf(store.Attempt{PaperCheck: []byte{0xff}}); err == nil {
		t.Error("a Check that cannot be read counts tasks")
	}
}

// A Check is kept whole, each slot with its read, box, assumptions, errors,
// hidden solution and answer, beside its words and ladder; a turn of the
// history is its verdicts, words and rungs.
func TestACheckIsKeptWhole(t *testing.T) {
	t.Parallel()

	checked := tutor.PaperChecked{TutorWords: "Look again.", Hints: []string{"What is asked?", "Which step?"},
		Slots: []tutor.SlotChecked{{Task: 1, Verdict: "incorrect", Confidence: "high", Read: "851", ReadBox: box(0, 100),
			Assumptions: []string{"base ten"}, Errors: []tutor.CheckError{{Box: box(40, 60), Where: "the end", Kind: "arithmetic", Note: "a digit more"}},
			Solution: "37 + 48 = 85", Answer: "85"}}}
	want := &paperv1.Check{TutorWords: "Look again.", Hints: []string{"What is asked?", "Which step?"},
		Slots: []*paperv1.SlotCheck{{Task: 1, Verdict: "incorrect", Confidence: "high", Read: "851", ReadBox: box(0, 100),
			Assumptions: []string{"base ten"}, Errors: []*paperv1.CheckError{{Box: box(40, 60), Where: "the end", Kind: "arithmetic", Note: "a digit more"}},
			Solution: "37 + 48 = 85", Answer: "85"}}}
	if got := checkOf(checked); !proto.Equal(got, want) {
		t.Errorf("the Check kept = %v, want %v", got, want)
	}
	turn := turnOf(want)
	if strings.Join(turn.Verdicts, " ") != "incorrect" || turn.Words != "Look again." || strings.Join(turn.Rungs, "|") != "What is asked?|Which step?" {
		t.Errorf("the turn = %+v, want the verdicts, words and rungs", turn)
	}
}
