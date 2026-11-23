package learning

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// paperTeller words the tutor's words of a task from paper, as the gate of
// admission does through tutor: whether they are admitted for a learner — with
// the work, a rung's, or without it, the opening question and a rung put
// another way — and the call. own is as wordsTeller has it.
type paperTeller interface {
	PaperWords(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer
}

func (g gateTutor) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, _ string) tutor.WordsAnswer {
	return g.Gate.TellPaperWords(ctx, w)
}

// openingRung is the rung whose words the attempt keeps of the opening question
// of a task from paper: the model's alone.
const openingRung = 0

// wordsLanguage is the language of the words of a task from paper of a profile
// with no language of its interface.
const wordsLanguage = "en"

// paperTelling is what a request for the words of a task from paper adds to its
// telling: the tasks of the crop, each with its key and the value read of the
// submission that gave the rung; the number of the rung's task, the first whose
// slot is "not_yet" there, from 1, or 0 when none is; the language of the
// profile's interface, which the words are in; whether its hint mode shows
// where; and the call.
type paperTelling struct {
	tasks     []tutor.PaperWordsTask
	task      int
	language  string
	showWhere bool
	request   tutor.PaperWordsRequest
}

// paperAdmitted says whether the words of a task from paper may be called for a
// learner of category: the gate admits them, or the debug mode's first model
// takes them past the list.
func (s *Service) paperAdmitted(ctx context.Context, withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.paperWords.PaperWords(withWork, category, ownKeyProvider) || s.debugChosen(ctx)
}

// holdAny is the profile's open attempt held in tx, of a module as hold holds it
// or of a task from paper as holdPaper does, with its row alone, which hold
// has held already.
func (s *Service) holdAny(ctx context.Context, tx pgx.Tx, profileID, attemptID string, now time.Time) (held, error) {
	h, err := s.hold(ctx, tx, profileID, attemptID, now)
	if errors.Is(err, store.ErrPaper) {
		row, err := s.paperOpen(ctx, tx, profileID, h.row, now)
		return held{row: row}, err
	}
	return h, err
}

// paperTold is what the words of rung on the attempt h of a task from paper
// speak of: the tasks of the key the attempt keeps, none before it has one, with
// the values read of the submission that gave the rung, and the first of them
// "not_yet" there, and what the words take of the profile's settings. The
// opening question reads no submission.
func paperTold(ctx context.Context, tx pgx.Tx, profileID string, h held, rung int) (*paperTelling, error) {
	language, showWhere, err := store.PaperWordsOf(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	k, err := keptKey(h.row)
	if err != nil {
		return nil, err
	}
	p := &paperTelling{language: cmp.Or(language, wordsLanguage), showWhere: showWhere}
	for _, t := range k.GetTasks() {
		p.tasks = append(p.tasks, tutor.PaperWordsTask{Statement: t.GetStatement(), Key: t.GetAnswer()})
	}
	if rung == openingRung {
		return p, nil
	}
	answers, err := store.Answers(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return nil, err
	}
	for _, written := range answers {
		answer := &v1.SubmitWorkResponse{}
		if err := proto.Unmarshal(written, answer); err != nil {
			return nil, err
		}
		if answer.GetWrongAnswers() != int32(rung) {
			continue
		}
		// The task n is the item n, of its one slot (taskPlace).
		for i, item := range answer.GetItems() {
			if i >= len(p.tasks) || len(item.GetAnswerSlots()) == 0 {
				continue
			}
			slot := item.GetAnswerSlots()[0]
			p.tasks[i].Read = slot.GetValue()
			if p.task == 0 && slot.GetVerdict() == string(verdict.NotYet) {
				p.task = i + 1
			}
		}
		break
	}
	return p, nil
}

// paperTurnedDown is the check that turns the words of a task from paper down,
// in a word of logging.WordsChecks, or none when they may reach the learner, as
// turnedDown is of a task of a module: empty words, a key of a task of the crop
// or the model's own answer of one in them, or their form (tutor.PaperBreach).
func paperTurnedDown(p *paperTelling, a tutor.WordsAnswer) string {
	if strings.TrimSpace(a.Question) == "" {
		return logging.WordsCheckEmpty
	}
	switch tutor.PaperBreach(a.Question, p.language, p.tasks, a.Answers) {
	case tutor.BreachLeak:
		return logging.WordsCheckLeak
	case tutor.BreachForm:
		return logging.WordsCheckForm
	}
	return ""
}

// paperRequest is the call of the words of t on a task from paper, by the
// route, with the crop of the work of a rung, none to put it another way, and
// the tutor's questions asked so far.
func paperRequest(t telling, route tutor.Route, crop []byte, asked []string) tutor.PaperWordsRequest {
	p := t.paper
	ask := tutor.PaperRung
	if len(crop) == 0 {
		ask = tutor.PaperAgain
	}
	return tutor.PaperWordsRequest{Route: route, Ask: ask, Crop: crop, Tasks: p.tasks, Task: p.task, Rung: t.rung,
		Language: p.language, Style: styles[formatv0.AgeCategory(route.Category)], Asked: asked}
}

// GetOpeningWords gives the tutor's opening question on a task from paper
// before its first wrong answer: one call of AI on the attempt, with the task's
// crop as printed and the tasks of the key, when the attempt keeps one, whose
// words, once they passed the leak check and the form check, the attempt keeps
// as its rung 0 until it closes, and every later request gets with no AI. The
// fallback text, which a call that gives no words or words that fail a check
// leave, is not kept: a request with a new key calls again. A repeat under the
// key gets the words kept, or the fallback text with the outcome it got. When
// the deployment admits the words, the call runs between two transactions, as
// GetRungWords does (rpc.Across).
func (a *AttemptService) GetOpeningWords(ctx context.Context, req *v1.GetOpeningWordsRequest) (*v1.GetOpeningWordsResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, req.GetAttemptId())
	defer act.End()
	profileID, attemptID := req.GetProfileId(), req.GetAttemptId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceGetOpeningWordsProcedure,
		GoingFor: s.goingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.GetOpeningWordsResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			var err error
			answer.Words, answer.Repeated, err = s.openingKept(ctx, tx, profileID, attemptID)
			return answer, err
		},
	}
	route := act.Route(routeOf(category, req.GetOwnKey()))
	admitted := s.paperAdmitted(ctx, false, category, req.GetOwnKey().GetProvider())
	prepare := func(tx pgx.Tx) (telling, error) {
		t, err := s.openingAsked(ctx, tx, req, route, admitted)
		if err == nil {
			t.day, err = s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
		}
		if err == nil && admitted && t.ready == nil {
			t.reserve, err = s.reserveFor(ctx, tx, profileID, route, wordsCall(t), 1, t.day)
		}
		return t, err
	}
	var answer *v1.GetOpeningWordsResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[telling, *v1.GetOpeningWordsResponse]{
			Prepare: func(tx pgx.Tx, _ string) (telling, store.KeyTarget, error) {
				t, err := prepare(tx)
				return t, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID}, err
			},
			Call: func(ctx context.Context, t telling) telling { return s.call(ctx, t, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, t telling) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
				return s.openingTold(ctx, tx, profileID, t)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
			t, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if t.ready == nil {
				// No model is admitted for the words: no call, and the fallback text.
				t.answer = tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}
			}
			return s.openingTold(ctx, tx, profileID, t)
		})
	}
	if err != nil {
		return nil, refused(ctx, err)
	}
	if c := answer.GetOutcome().GetCause(); c != v1.Cause_CAUSE_UNSPECIFIED {
		rpc.NoteOutcome(ctx, c)
	}
	return answer, nil
}

// openingAsked checks a request for the opening question before anything is
// called: an open attempt of a task from paper with no wrong answer, and the
// task's crop. The words the attempt keeps already are answered with no call.
func (s *Service) openingAsked(ctx context.Context, tx pgx.Tx, req *v1.GetOpeningWordsRequest, route tutor.Route, admitted bool) (telling, error) {
	profileID := req.GetProfileId()
	h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), s.clock())
	switch {
	case err != nil:
		return telling{}, err
	case !h.row.Paper:
		return telling{h: h}, rpc.Malformed("attempt_id")
	case h.row.WrongAnswers > 0:
		return telling{h: h}, notCurrent(ctx, h.row.WrongAnswers)
	case !photoTaken(req.GetStatement()):
		return telling{h: h}, rpc.Malformed("statement")
	}
	t := telling{h: h, rung: openingRung}
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, openingRung)
	if err != nil {
		return t, err
	}
	if t.paper, err = paperTold(ctx, tx, profileID, h, openingRung); err != nil {
		return t, err
	}
	if found && kept.Words.Text != "" {
		words, repeated := openingChecked(kept.Words.Text, t.paper)
		t.ready = &v1.GetOpeningWordsResponse{Words: words, Repeated: repeated}
		return t, nil
	}
	if !admitted {
		return t, err
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return t, connect.NewError(connect.CodeDeadlineExceeded, "too little time to word the opening question")
	}
	p := t.paper
	p.request = tutor.PaperWordsRequest{Route: route, Ask: tutor.PaperOpening, Statement: req.GetStatement(), Tasks: p.tasks,
		Language: p.language, Style: styles[formatv0.AgeCategory(route.Category)]}
	return t, nil
}

// openingTold writes what came of the opening question of t and answers it, on
// the attempt held again after a call: the model's words, which the attempt
// keeps, or the fallback text, which it does not. The record of the key holds
// no words.
func (s *Service) openingTold(ctx context.Context, tx pgx.Tx, profileID string, t telling) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
	answer, ok := t.ready.(*v1.GetOpeningWordsResponse)
	if !ok && t.called {
		_, kept, found, err := s.heldAgain(ctx, tx, profileID, t)
		switch {
		case err != nil:
			return nil, store.KeyTarget{}, err
		case found && kept.Words.Text != "":
			answer, ok = &v1.GetOpeningWordsResponse{Words: kept.Words.Text, Repeated: true}, true
		}
	}
	if !ok {
		words, outcome := worded(ctx, t)
		if words.Source == store.SourceModel {
			if err := store.WriteRung(ctx, tx, profileID, t.h.row.ID, openingRung, words); err != nil {
				return nil, store.KeyTarget{}, err
			}
		}
		answer = &v1.GetOpeningWordsResponse{Words: words.Text, Outcome: outcome}
	}
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.GetOpeningWordsResponse{Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID, Response: written}, nil
}

// openingKept is the opening question a repeat gets, held in tx: the words the
// attempt keeps, and so repeated, or the fallback text.
func (s *Service) openingKept(ctx context.Context, tx pgx.Tx, profileID, attemptID string) (string, bool, error) {
	h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
	if refusal, ok := rpc.Kept(err); ok {
		// The close is the next request's to write: a repeat answers from the record.
		return "", false, refusal
	}
	if err != nil {
		return "", false, err
	}
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, openingRung)
	if err != nil {
		return "", false, err
	}
	if found && kept.Words.Text != "" {
		p, err := paperTold(ctx, tx, profileID, h, openingRung)
		if err != nil {
			return "", false, err
		}
		words, repeated := openingChecked(kept.Words.Text, p)
		return words, repeated, nil
	}
	language, _, err := store.PaperWordsOf(ctx, tx, profileID)
	return tutor.PaperFallback(openingRung, cmp.Or(language, wordsLanguage)), false, err
}

// openingChecked is the kept opening question p gives the learner, and whether
// it is the one kept: words worded before a Check derived a key were checked by
// the model's own answers alone, so a key derived since checks them again, and
// words that give it away are the fallback text in their place.
func openingChecked(kept string, p *paperTelling) (string, bool) {
	if tutor.PaperBreach(kept, p.language, p.tasks, nil) == tutor.BreachLeak {
		return tutor.PaperFallback(openingRung, p.language), false
	}
	return kept, true
}
