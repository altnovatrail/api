package learning

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/langtag"
	"github.com/altnovatrail/api/internal/logging"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// paperTeller words "explain differently" on a task from paper, as the gate of
// admission does through tutor: whether it is admitted for a learner, and the
// call. own is as wordsTeller has it. The other words of a task from paper are
// the Check's.
type paperTeller interface {
	PaperWords(category formatv0.AgeCategory, ownKeyProvider string) bool
	TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, own string) tutor.WordsAnswer
}

func (g gateTutor) TellPaperWords(ctx context.Context, w tutor.PaperWordsRequest, _ string) tutor.WordsAnswer {
	return g.Gate.TellPaperWords(ctx, w)
}

// openingRung is the rung of tutor's fallback text of the opening question of a
// task from paper, which a Check that gave no ladder leaves.
const openingRung = 0

// wordsLanguage is the language of the words of a task from paper when the
// request names none and the profile has no language of its interface.
const wordsLanguage = "en"

// paperTelling is what "explain differently" on a task from paper adds to its
// telling: the tasks of the frame as the Check the attempt keeps gave them, each
// with its statement as "Recognise" read it, the model's hidden solution and
// answer and what was read; the number of the rung's task, the first the Check
// did not judge correct, from 1, or 0 when none is; the language of the
// profile's interface, which the words are in; the words of the rung's ladder;
// and the call.
type paperTelling struct {
	tasks    []tutor.PaperWordsTask
	task     int
	language string
	rung     store.Words
	request  tutor.PaperWordsRequest
}

// paperAdmitted says whether "explain differently" on a task from paper may be
// called for a learner of category: the gate admits it, or the debug mode's
// first model takes it past the list.
func (s *Service) paperAdmitted(ctx context.Context, category formatv0.AgeCategory, ownKeyProvider string) bool {
	return s.paperWords.PaperWords(category, ownKeyProvider) || s.debugChosen(ctx)
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

// paperLanguage is the language of the words of a task from paper of the
// profile — the one the request names (askedLanguage), else the profile's
// interface's, else English — and whether its hint mode shows where.
func paperLanguage(ctx context.Context, tx pgx.Tx, profileID string) (string, bool, error) {
	language, showWhere, err := store.PaperWordsOf(ctx, tx, profileID)
	asked, _ := ctx.Value(wordsKey{}).(string)
	return cmp.Or(asked, language, wordsLanguage), showWhere, err
}

// wordsKey holds, in the context of a request that gives words on a task from
// paper, the language of the screens app shows the learner. The words and the
// voice that says them are then in the language the learner reads, whatever
// the profile keeps.
type wordsKey struct{}

// askedLanguage is ctx with the request's words_language, a well-formed tag,
// which the words of a task from paper are written in; an empty one leaves the
// profile's language. The tag reaches a model as text, so a malformed one is
// refused.
func askedLanguage(ctx context.Context, tag string) (context.Context, error) {
	if tag == "" {
		return ctx, nil
	}
	if !langtag.WellFormed(tag) {
		return ctx, rpc.Malformed("words_language")
	}
	return context.WithValue(ctx, wordsKey{}, tag), nil
}

// paperTold is what "explain differently" on rung of the attempt h of a task
// from paper speaks of: the tasks of the Check the attempt keeps, none before
// its first, each with its statement where "Recognise" read as many tasks as
// the Check gave, and the hidden answer of a task not yet correct alone, the
// answer of one correct being the learner's own; the rung's task; the
// profile's language; and the rung's words of the ladder.
func paperTold(ctx context.Context, tx pgx.Tx, profileID string, h held, rung int) (*paperTelling, error) {
	language, _, err := paperLanguage(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	check, err := keptCheck(h.row)
	if err != nil {
		return nil, err
	}
	reading, err := keptReading(h.row)
	if err != nil {
		return nil, err
	}
	p := &paperTelling{language: language, rung: ladderRung(check, rung, language)}
	p.tasks, p.task = framed(check, readStatements(reading))
	return p, nil
}

// framed is the tasks of the frame as the Check c gave them, each with its
// statement where "Recognise" read as many statements as the Check gave tasks,
// the hidden answer of a task not yet correct alone; and the number of the
// first task not correct, from 1, or 0 when every task is.
func framed(c *paperv1.Check, statements []string) ([]tutor.PaperWordsTask, int) {
	var tasks []tutor.PaperWordsTask
	first := 0
	for i, slot := range c.GetSlots() {
		t := tutor.PaperWordsTask{Solution: slot.GetSolution(), Answer: slot.GetAnswer(), Read: slot.GetRead()}
		if len(statements) == len(c.GetSlots()) {
			t.Statement = statements[i]
		}
		if slot.GetVerdict() == checkCorrect {
			t.Answer = ""
		} else if first == 0 {
			first = i + 1
		}
		tasks = append(tasks, t)
	}
	return tasks, first
}

// ladderRung is the words of rung of the ladder of the Check c, the last rung
// past the ladder's end: the model's, or the fallback text tutor put in place
// of a rung that named a hidden answer; with no ladder, the rung's fallback
// text in language.
func ladderRung(c *paperv1.Check, rung int, language string) store.Words {
	hints := c.GetHints()
	if len(hints) == 0 {
		return store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(rung, language)}
	}
	n := min(max(rung, 1), len(hints))
	if hints[n-1] == tutor.PaperFallback(n, language) {
		return store.Words{Source: store.SourceFallback, Text: hints[n-1]}
	}
	return store.Words{Source: store.SourceModel, Text: hints[n-1]}
}

// paperRung is the rung of the attempt h of a task from paper now — the k-th
// wrong answer's rung k, the first before any — with the words of the ladder
// the attempt keeps for it, and what the attempt keeps of the rung once it was
// given, which found says.
func paperRung(ctx context.Context, tx pgx.Tx, profileID string, h held) (int, store.Words, store.Rung, bool, error) {
	rung := max(rungOf(h.row.WrongAnswers), 1)
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, rung)
	if err != nil {
		return 0, store.Words{}, store.Rung{}, false, err
	}
	language, _, err := paperLanguage(ctx, tx, profileID)
	if err != nil {
		return 0, store.Words{}, store.Rung{}, false, err
	}
	check, err := keptCheck(h.row)
	return rung, ladderRung(check, rung, language), kept, found, err
}

// paperHint is "Hint" on the attempt h of a task from paper, held in tx: before
// its first Check, which gives it its ladder, empty techniques, since it has
// none and asks GetOpeningWords for its words; after it, the current rung of
// the ladder, the first while there is no wrong answer, which the attempt then
// keeps as given, since GetRungWords is asked only after a wrong answer.
func paperHint(ctx context.Context, tx pgx.Tx, profileID string, h held) (*v1.GetHintResponse, error) {
	if h.row.PaperCheck == nil && h.row.WrongAnswers == 0 {
		return &v1.GetHintResponse{Hint: &v1.GetHintResponse_Techniques{Techniques: &v1.Techniques{}}}, nil
	}
	rung, words, kept, found, err := paperRung(ctx, tx, profileID, h)
	if err != nil {
		return nil, err
	}
	if found {
		words = kept.Current()
	} else if h.row.WrongAnswers == 0 {
		if err := store.WriteRung(ctx, tx, profileID, h.row.ID, rung, words); err != nil {
			return nil, err
		}
	}
	return &v1.GetHintResponse{Hint: &v1.GetHintResponse_Rung{Rung: &v1.RungHint{Rung: int32(rung), Words: words.Text}}}, nil
}

// paperRungWords gives the words of the rung a wrong answer gave on a task from
// paper, with no call of AI and no work, under the request's idempotency key:
// the rung of the ladder the attempt keeps, which the attempt keeps as given,
// with the wavy line under the box of the first mistake of the Check in the
// hint mode "Show where", with the model's words alone. A rung given already,
// or a repeat, gets the words the attempt keeps, with no marks.
func (s *Service) paperRungWords(ctx context.Context, req *v1.GetRungWordsRequest, key rpc.Key) (*v1.GetRungWordsResponse, error) {
	profileID := req.GetProfileId()
	return rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetRungWordsResponse, store.KeyTarget, error) {
		h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), s.clock())
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		target := store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID}
		if err := named(ctx, h, req.WrongAnswers, false); err != nil {
			return nil, target, err
		}
		if h.row.WrongAnswers > lastRung {
			return nil, target, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
		}
		rung, words, kept, found, err := paperRung(ctx, tx, profileID, h)
		if err != nil {
			return nil, target, err
		}
		answer := keptRung(rung, kept)
		if !found {
			if err := store.WriteRung(ctx, tx, profileID, h.row.ID, rung, words); err != nil {
				return nil, target, err
			}
			answer = &v1.GetRungWordsResponse{Rung: int32(rung), Words: words.Text}
			_, showWhere, err := store.PaperWordsOf(ctx, tx, profileID)
			if err != nil {
				return nil, target, err
			}
			check, err := keptCheck(h.row)
			if err != nil {
				return nil, target, err
			}
			if box := firstMistake(check); showWhere && box != nil && words.Source == store.SourceModel {
				answer.Marks = []*v1.Mark{{Kind: v1.MarkKind_MARK_KIND_WAVY_LINE, Place: &v1.Mark_CropBox{CropBox: box}}}
			}
		}
		// An answer of the contract always marshals; the record keeps no words.
		target.Response, _ = proto.Marshal(&v1.GetRungWordsResponse{Rung: answer.GetRung()})
		return answer, target, nil
	})
}

// paperTurnedDown is the check that turns "explain differently" on a task from
// paper down, in a word of logging.WordsChecks, or none when the words may
// reach the learner, as turnedDown is of a task of a module: empty words, a
// hidden answer of a task not yet correct in them, or their form
// (tutor.PaperBreach), the statements "Recognise" read and the values read
// being no leak.
func paperTurnedDown(p *paperTelling, a tutor.WordsAnswer) string {
	if strings.TrimSpace(a.Question) == "" {
		return logging.WordsCheckEmpty
	}
	switch tutor.PaperBreach(a.Question, p.language, p.tasks) {
	case tutor.BreachLeak:
		return logging.WordsCheckLeak
	case tutor.BreachForm:
		return logging.WordsCheckForm
	}
	return ""
}

// paperRequest is the call of "explain differently" of t on a task from paper,
// by the route, with the tutor's questions asked so far, the rung's words the
// learner reads last, which the call puts another way.
func paperRequest(t telling, route tutor.Route, asked []string) tutor.PaperWordsRequest {
	p := t.paper
	if n := len(asked); t.read.Text != "" && (n == 0 || asked[n-1] != t.read.Text) {
		asked = append(asked, t.read.Text)
	}
	return tutor.PaperWordsRequest{Route: route, Ask: tutor.PaperAgain, Tasks: p.tasks, Task: max(p.task, 1), Rung: t.rung,
		Language: p.language, Style: styles[formatv0.AgeCategory(route.Category)], Asked: asked}
}

// GetOpeningWords is "Hint" on a task from paper before its first Check: the
// work as it stands goes to the Check, one call of AI on the attempt, which
// keeps the Check and its ladder as after a submission, but shows no verdict
// and counts no wrong answer; the answer is the ladder's first rung, which the
// attempt keeps as given, or the tutor's words when that rung is a fallback
// text. A Check that gives no answer leaves the opening question's fallback
// text, with the outcome, and keeps nothing: a request with a new key calls
// again. Once the attempt keeps a ladder, a request is "not current", and a
// repeat under the key gets the first rung kept. When the deployment admits
// the Check, the call runs between two transactions, as SubmitWork does
// (rpc.Across).
func (a *AttemptService) GetOpeningWords(ctx context.Context, req *v1.GetOpeningWordsRequest) (*v1.GetOpeningWordsResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if ctx, err = askedLanguage(ctx, req.GetWordsLanguage()); err != nil {
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
	admitted := s.admits.Reads(tutor.CallChecking, category, req.GetOwnKey().GetProvider()) || s.debugChosen(ctx)
	prepare := func(tx pgx.Tx) (paperChecking, error) {
		return s.openingAsked(ctx, tx, req, route, category, admitted)
	}
	var answer *v1.GetOpeningWordsResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[paperChecking, *v1.GetOpeningWordsResponse]{
			Prepare: func(tx pgx.Tx, _ string) (paperChecking, store.KeyTarget, error) {
				c, err := prepare(tx)
				return c, store.KeyTarget{ProfileID: profileID, AttemptID: c.attemptID}, err
			},
			Call: func(ctx context.Context, c paperChecking) paperChecking {
				return s.callCheck(ctx, c, req.GetOwnKey().GetKey())
			},
			Finish: func(tx pgx.Tx, c paperChecking) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
				return s.openingTold(ctx, tx, profileID, c)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
			c, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return s.openingTold(ctx, tx, profileID, c)
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
// called: an open attempt of a task from paper that keeps no ladder and has no
// wrong answer, and its work. With the Check admitted, it gives the Check's
// call; without, the fallback text with the outcome "not admitted", and no
// call.
func (s *Service) openingAsked(ctx context.Context, tx pgx.Tx, req *v1.GetOpeningWordsRequest, route tutor.Route, category formatv0.AgeCategory, admitted bool) (paperChecking, error) {
	profileID := req.GetProfileId()
	h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), s.clock())
	c := paperChecking{attemptID: h.row.ID}
	switch {
	case err != nil:
		return c, err
	case !h.row.Paper:
		return c, rpc.Malformed("attempt_id")
	case h.row.WrongAnswers > 0 || h.row.PaperCheck != nil:
		return c, notCurrent(ctx, h.row.WrongAnswers)
	}
	work := req.GetWork().GetPaper()
	if err := paperWorkTaken(work); err != nil {
		return c, err
	}
	if !admitted {
		language, _, err := paperLanguage(ctx, tx, profileID)
		c.ready = &v1.GetOpeningWordsResponse{Words: tutor.PaperFallback(openingRung, language),
			Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}}
		return c, err
	}
	return s.checkAsked(ctx, tx, profileID, h.row, work, route, category, req.ClockOffsetMinutes)
}

// openingTold writes what came of the Check of the opening question c and
// answers it, on the attempt held again after the call: the Check kept, and
// its first rung given (openingWords); or the fallback text with the outcome
// of a Check that gave none, nothing kept. A Check another request kept
// meanwhile answers with its own first rung, repeated. The record of the key
// holds no words.
func (s *Service) openingTold(ctx context.Context, tx pgx.Tx, profileID string, c paperChecking) (*v1.GetOpeningWordsResponse, store.KeyTarget, error) {
	answer := c.ready
	if answer == nil {
		row, err := s.holdPaper(ctx, tx, profileID, c.attemptID, s.clock())
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		language := c.check.Language
		switch outcome := c.outcome(); {
		case row.PaperCheck != nil:
			words, err := openingOf(ctx, tx, profileID, held{row: row}, language)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			answer = &v1.GetOpeningWordsResponse{Words: words, Repeated: true}
		case outcome != nil:
			answer = &v1.GetOpeningWordsResponse{Words: tutor.PaperFallback(openingRung, language), Outcome: outcome}
		default:
			if err := keepCheck(ctx, tx, profileID, row, c.checked); err != nil {
				return nil, store.KeyTarget{}, err
			}
			words := openingWords(c.checked.Hints, c.checked.TutorWords, language)
			if err := store.WriteRung(ctx, tx, profileID, row.ID, 1, words); err != nil {
				return nil, store.KeyTarget{}, err
			}
			answer = &v1.GetOpeningWordsResponse{Words: words.Text}
		}
	}
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.GetOpeningWordsResponse{Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: c.attemptID, Response: written}, nil
}

// openingWords is what the opening question gives of a Check's ladder hints
// and tutor's words: its first rung, or the tutor's words where that rung is a
// fallback text, or there is no ladder; the fallback text of the opening
// question when the Check gave neither.
func openingWords(hints []string, tutorWords, language string) store.Words {
	first := ladderRung(&paperv1.Check{Hints: hints}, 1, language)
	switch {
	case first.Source == store.SourceModel:
		return first
	case strings.TrimSpace(tutorWords) != "":
		return store.Words{Source: store.SourceModel, Text: tutorWords}
	case len(hints) > 0:
		return first
	}
	return store.Words{Source: store.SourceFallback, Text: tutor.PaperFallback(openingRung, language)}
}

// openingOf is the opening question of the attempt h, which keeps a Check: the
// first rung as the attempt keeps it once given, or as the Check gives it.
func openingOf(ctx context.Context, tx pgx.Tx, profileID string, h held, language string) (string, error) {
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, 1)
	if err != nil || found {
		return kept.Current().Text, err
	}
	check, err := keptCheck(h.row)
	if err != nil {
		return "", err
	}
	return openingWords(check.GetHints(), check.GetTutorWords(), language).Text, nil
}

// openingKept is the opening question a repeat gets, held in tx: the first rung
// of the ladder the attempt keeps, and so repeated, or the fallback text.
func (s *Service) openingKept(ctx context.Context, tx pgx.Tx, profileID, attemptID string) (string, bool, error) {
	h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
	if refusal, ok := rpc.Kept(err); ok {
		// The close is the next request's to write: a repeat answers from the record.
		return "", false, refusal
	}
	if err != nil {
		return "", false, err
	}
	language, _, err := paperLanguage(ctx, tx, profileID)
	if err != nil {
		return "", false, err
	}
	if h.row.PaperCheck == nil {
		return tutor.PaperFallback(openingRung, language), false, nil
	}
	words, err := openingOf(ctx, tx, profileID, h, language)
	return words, true, err
}
