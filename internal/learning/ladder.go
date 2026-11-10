package learning

import (
	"context"
	"slices"
	"strings"

	"github.com/altnovatrail/module-kit/leak"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/openview"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/words"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/store"
)

// lastRung is the hint ladder's last rung: from the fourth wrong answer on, no
// rung is new, and the ladder speaks as its third (R09).
const lastRung = 3

// rungOf is the rung that wrong answers give: the k-th gives the rung k, up to
// the last; none before the first.
func rungOf(wrongAnswers int) int { return min(wrongAnswers, lastRung) }

// spot is the answer slot a rung speaks of (R10): the first "not_yet" in order
// in the submission that gave the rung — items by place, slots by order in an
// item — with the slot and the trap value its value matched, if any. It is not
// kept: the verdicts of the submission give it again. An item below 0 is no
// slot found, which a rung of a submission this code wrote always has: the rung
// then speaks as its template, and the open attempt stays readable.
type spot struct {
	item int
	slot verdict.AnswerSlot
	trap *formatv0.TrapValue
}

// spotOf is the answer slot rung speaks of on the attempt h: of the first
// submission whose wrong answers reached the rung, the first slot "not_yet",
// whose trap is found again by module-kit's verdict of the value it holds.
func spotOf(ctx context.Context, tx pgx.Tx, profileID string, h held, rung int) (spot, error) {
	answers, err := store.Answers(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return spot{}, err
	}
	for _, written := range answers {
		answer := &v1.SubmitWorkResponse{}
		if err := proto.Unmarshal(written, answer); err != nil {
			return spot{}, err
		}
		if answer.GetWrongAnswers() != int32(rung) {
			continue
		}
		for _, item := range answer.GetItems() {
			for _, s := range item.GetAnswerSlots() {
				if s.GetVerdict() != string(verdict.NotYet) {
					continue
				}
				return spotAt(ctx, h.task, s)
			}
		}
	}
	return noSpot, nil
}

// noSpot is no answer slot found.
var noSpot = spot{item: -1}

// spotAt is the answer slot of the task where s stands, with the trap value of
// the value read in it.
func spotAt(ctx context.Context, task formatv0.Task, s *v1.AnswerSlotVerdict) (spot, error) {
	i, j := int(s.GetAnswerSlot().GetItem())-1, int(s.GetAnswerSlot().GetSlot())-1
	if i < 0 || i >= len(task.Items) || j < 0 || j >= len(verdict.AnswerSlots(task.Items[i])) {
		return noSpot, nil
	}
	slot := verdict.AnswerSlots(task.Items[i])[j]
	v, err := verdict.OfSlot(ctx, slot, s.Value)
	return spot{item: i, slot: slot, trap: v.Trap}, err
}

// fallback is the rung's fallback text on the answer slot at, by the chain of
// the format (§ 19 of its text; R57 spec): at rungs 1 and 2, the text of the
// trap value the wrong value matched, then the advice of its trap, then the
// module's template of the rung; at rung 3, the simpler case of the slot's item,
// then what rung 2 gives.
func fallback(pkg *modulepackage.Package, task formatv0.Task, rung int, at spot) string {
	if rung == lastRung {
		if c := simplerCase(task, at); c != nil {
			return string(*c)
		}
		rung = 2
	}
	if t := at.trap; t != nil {
		if t.Text != nil {
			return string(*t.Text)
		}
		if traps := pkg.View.Traps; traps != nil {
			if trap, ok := traps.Traps[t.Trap]; ok {
				return string(trap.TrapAdvice)
			}
		}
	}
	if templates := pkg.View.Manifest.RungTemplates; len(templates) >= rung {
		return string(templates[rung-1])
	}
	// A module with an item with a verdict has its templates (manifest.rung-templates).
	return ""
}

// simplerCase is the simpler case of the item of the slot at, if any.
func simplerCase(task formatv0.Task, at spot) *formatv0.Text {
	if at.item < 0 {
		return nil
	}
	return task.Items[at.item].SimplerCase
}

// passes says whether the tutor's question may reach the learner (R70 spec):
// the leak check of module-kit finds none of the task's keys or leak phrases in
// it, on the instance the attempt got, and its form is a hint's, by tutor's
// words. Words that fail either give way to the fallback text.
func passes(pkg *modulepackage.Package, task formatv0.Task, question string) bool {
	if strings.TrimSpace(question) == "" {
		return false
	}
	search, err := leak.For(task)
	if err != nil || len(search.In(question)) > 0 {
		return false
	}
	return len(words.Hint(question, string(pkg.View.Manifest.Languages.Explanation), statementOf(task))) == 0
}

// statementOf is the words of the task's statement, whose Latin letters may
// stand in a hint in another script.
func statementOf(task formatv0.Task) string {
	var parts []string
	for _, p := range task.Statement {
		if p.Text != nil {
			parts = append(parts, string(*p.Text))
		}
		if p.Alt != nil {
			parts = append(parts, *p.Alt)
		}
	}
	return strings.Join(parts, "\n")
}

// styles is what of the presentation style changes the tutor's words, by the
// learner's age category: the profile holds nothing else that goes to AI (Д74
// RUN.md spec; R47).
var styles = map[formatv0.AgeCategory]string{
	formatv0.AgeCategoryUnder13:         "a child in primary school; use short, simple, everyday words",
	formatv0.AgeCategoryFrom13ToConsent: "a teenager; use plain words",
	formatv0.AgeCategoryFromConsentTo18: "a teenager; use plain words",
	formatv0.AgeCategoryAdult:           "an adult",
}

// wordsAsked is the call of words of rung on the answer slot at of the attempt
// h, by the route, with the crop when the work went, and the tutor's questions
// asked so far (architecture 4.1 spec): the task's key of the slot and its
// solution, which the model sees here alone, its traps that AI may name with
// the one the program found, and its techniques.
func wordsAsked(h held, route tutor.Route, rung int, at spot, crop []byte, asked []string) (tutor.WordsRequest, error) {
	view, err := openview.Task(h.task)
	if err != nil {
		return tutor.WordsRequest{}, err
	}
	w := tutor.WordsRequest{Route: route, Task: &view, Crop: crop, Rung: rung, Asked: asked,
		Language: string(h.pkg.View.Manifest.Languages.Explanation), Style: styles[formatv0.AgeCategory(route.Category)]}
	if at.slot.Check.Key != nil {
		w.Key = *at.slot.Check.Key
	}
	for _, p := range h.task.Solution {
		if p.Text != nil {
			w.Solution = append(w.Solution, string(*p.Text))
		}
	}
	ids := slices.Clone(h.task.Traps)
	if at.trap != nil && !slices.Contains(ids, at.trap.Trap) {
		ids = append(ids, at.trap.Trap)
	}
	for _, id := range ids {
		if catalog := h.pkg.View.Traps; catalog != nil {
			if trap, ok := catalog.Traps[id]; ok {
				w.Traps = append(w.Traps, tutor.Trap{ID: string(id), Description: string(trap.Description)})
				if at.trap != nil && id == at.trap.Trap {
					// The trap found goes as one of those the model reads described.
					w.Found = string(id)
				}
			}
		}
	}
	for _, id := range h.task.Techniques {
		if catalog := h.pkg.View.Techniques; catalog != nil {
			if t, ok := catalog.Techniques[id]; ok {
				w.Techniques = append(w.Techniques, string(t.Title))
			}
		}
	}
	return w, nil
}
