package learning

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The bounds of a question to the tutor and of the conversation before it, in
// code points and turns, as the contract sets them.
const (
	maxQuestion = 500
	maxTurns    = 8
	maxTurnText = 1000
)

// asking is a question to the tutor on a task from paper between the parts of
// its request: the Check of the work as it stands, when the attempt keeps none,
// whose call goes first; the statements "Recognise" read, which the tasks of
// that Check take; the tasks and the language the words are checked by; the
// call of the words with its reserve, and its answer; and an answer with no
// call.
type asking struct {
	attemptID  string
	checking   *paperChecking
	statements []string
	paper      paperTelling
	request    tutor.PaperWordsRequest
	reserve    reserve
	answer     tutor.WordsAnswer
	route      tutor.Route
	ready      *v1.AskTutorResponse
}

// AskTutor answers a question of the learner's own about a task from paper:
// one call of the words of a task from paper, tutor.PaperQuestion, about the
// first task not correct, with the tasks of the Check the attempt keeps, the
// rung given, the hints given and the conversation app holds. While the attempt
// keeps no Check, the work as it stands is checked first and the Check kept, as
// GetOpeningWords keeps it, but with no rung given; both calls reserve the quota
// together. No admission, the quota, the limit and a call that gives no words
// answer the opening question's fallback text with the outcome, and words that
// fail the checks the same text with none. The question, the conversation and
// the words are kept nowhere: the record of the key holds the outcome alone, so
// a repeat gets no words, and the key's fingerprint is argon2id's, as of a
// request that carries a PIN.
func (a *AttemptService) AskTutor(ctx context.Context, req *v1.AskTutorRequest) (*v1.AskTutorResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if ctx, err = askedLanguage(ctx, req.GetWordsLanguage()); err != nil {
		return nil, err
	}
	if err := questionTaken(req.GetQuestion(), req.GetTurns()); err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, req.GetAttemptId())
	defer act.End()
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceAskTutorProcedure,
		Slow:     true,
		GoingFor: s.goingFor,
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.AskTutorResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Repeated = true
			return answer, nil
		},
	}
	route := act.Route(routeOf(category, req.GetOwnKey()))
	provider := req.GetOwnKey().GetProvider()
	admitted := s.paperAdmitted(ctx, category, provider)
	checking := s.admits.Reads(tutor.CallChecking, category, provider) || s.debugChosen(ctx)
	prepare := func(tx pgx.Tx) (asking, error) {
		return s.questionAsked(ctx, tx, req, route, category, admitted, checking)
	}
	var answer *v1.AskTutorResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[asking, *v1.AskTutorResponse]{
			Prepare: func(tx pgx.Tx, _ string) (asking, store.KeyTarget, error) {
				q, err := prepare(tx)
				return q, store.KeyTarget{ProfileID: profileID, AttemptID: q.attemptID}, err
			},
			Call: func(ctx context.Context, q asking) asking { return s.ask(ctx, q, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, q asking) (*v1.AskTutorResponse, store.KeyTarget, error) {
				return s.questionTold(ctx, tx, profileID, q)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.AskTutorResponse, store.KeyTarget, error) {
			q, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return s.questionTold(ctx, tx, profileID, q)
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

// questionTaken checks a question q and the conversation turns before it by
// the contract's bounds: a question not empty, of at most maxQuestion code
// points, with no control character but a line break; at most maxTurns turns,
// each of at most maxTurnText code points. They reach a model as data.
func questionTaken(q string, turns []*v1.TutorTurn) error {
	if strings.TrimSpace(q) == "" || utf8.RuneCountInString(q) > maxQuestion ||
		strings.ContainsFunc(q, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return rpc.Malformed("question")
	}
	if len(turns) > maxTurns {
		return rpc.Malformed("turns")
	}
	for _, t := range turns {
		if utf8.RuneCountInString(t.GetText()) > maxTurnText {
			return rpc.Malformed("turns")
		}
	}
	return nil
}

// questionAsked checks a question before anything is called — an open attempt
// of a task from paper, and its work while it keeps no Check — and gives its
// calls with their reserve: the Check first when the attempt keeps none, then
// the words. A call not admitted, or a reserve the quota or the limit does not
// hold, gives the fallback text with the outcome and no call.
func (s *Service) questionAsked(ctx context.Context, tx pgx.Tx, req *v1.AskTutorRequest, route tutor.Route, category formatv0.AgeCategory, admitted, checking bool) (asking, error) {
	profileID := req.GetProfileId()
	h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), s.clock())
	q := asking{attemptID: h.row.ID, route: route}
	switch {
	case err != nil:
		return q, err
	case !h.row.Paper:
		return q, rpc.Malformed("attempt_id")
	}
	if q.paper.language, _, err = paperLanguage(ctx, tx, profileID); err != nil {
		return q, err
	}
	reading, err := keptReading(h.row)
	if err != nil {
		return q, err
	}
	q.statements = readStatements(reading)
	notAdmitted := func() (asking, error) {
		q.ready = q.fallback(&v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED})
		return q, nil
	}
	check := h.row.PaperCheck == nil
	if check {
		work := req.GetWork().GetPaper()
		if err := paperWorkTaken(work); err != nil {
			return q, err
		}
		if !admitted || !checking {
			return notAdmitted()
		}
		c, err := checkBuilt(ctx, tx, profileID, h.row, work, route, category)
		if err != nil {
			return q, err
		}
		q.checking = &c
	} else if !admitted {
		return notAdmitted()
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return q, connect.NewError(connect.CodeDeadlineExceeded, "too little time to answer the question")
	}
	// The hints the learner was shown, whatever they came of, are what the question follows.
	asked, err := store.ShownRungs(ctx, tx, profileID, h.row.ID)
	if err != nil {
		return q, err
	}
	rung := openingRung
	if !check {
		kept, err := keptCheck(h.row)
		if err != nil {
			return q, err
		}
		q.paper.tasks, q.paper.task = framed(kept, q.statements)
		if rung, err = givenRung(ctx, tx, profileID, h.row); err != nil {
			return q, err
		}
	}
	q.request = tutor.PaperWordsRequest{Route: route, Ask: tutor.PaperQuestion, Tasks: q.paper.tasks, Task: max(q.paper.task, 1),
		Rung: rung, Language: q.paper.language, Style: styles[formatv0.AgeCategory(route.Category)], Asked: asked,
		Question: req.GetQuestion(), Turns: turnsOf(req.GetTurns())}
	day, err := s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
	if err != nil {
		return q, err
	}
	if q.checking != nil {
		q.checking.reserve, q.reserve, err = s.reservePair(ctx, tx, profileID, route, tutor.CallChecking, tutor.CallPaperHint, day)
	} else {
		q.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallPaperHint, 1, day)
	}
	refused := q.reserve.refused
	if q.checking != nil {
		// The pair is refused as its first, the Check.
		refused = q.checking.reserve.refused
	}
	if refused != nil {
		q.ready = q.fallback(refused)
	}
	return q, err
}

// givenRung is the last rung of the ladder given on the attempt row, up to its
// rung now, or 0 while none is.
func givenRung(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt) (int, error) {
	for rung := max(rungOf(row.WrongAnswers), 1); rung >= 1; rung-- {
		_, found, err := store.RungOf(ctx, tx, profileID, row.ID, rung)
		if err != nil {
			return 0, err
		}
		if found {
			return rung, nil
		}
	}
	return openingRung, nil
}

// turnsOf is the conversation of the request as tutor takes it.
func turnsOf(turns []*v1.TutorTurn) []tutor.PaperTurn {
	var out []tutor.PaperTurn
	for _, t := range turns {
		out = append(out, tutor.PaperTurn{Learner: t.GetLearner(), Text: t.GetText()})
	}
	return out
}

// fallback is the answer of the opening question's fallback text, in the
// language of the words, with the outcome o.
func (q asking) fallback(o *v1.Outcome) *v1.AskTutorResponse {
	return &v1.AskTutorResponse{Words: tutor.PaperFallback(openingRung, q.paper.language), Outcome: o}
}

// ask makes the calls of q, between two transactions: the Check, when the
// attempt keeps none, whose tasks the words then speak of, and the words. A
// Check that gives no answer, or leaves too little time for the words, makes no
// call of the words, and their reserve goes back whole.
func (s *Service) ask(ctx context.Context, q asking, own string) asking {
	if q.ready != nil {
		return q
	}
	if c := q.checking; c != nil {
		*c = s.callCheck(ctx, *c, own)
		if c.outcome() != nil {
			s.release(ctx, q.reserve)
			return q
		}
		q.paper.tasks, q.paper.task = framed(checkOf(c.checked), q.statements)
		q.request.Tasks, q.request.Task = q.paper.tasks, max(q.paper.task, 1)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
			s.release(ctx, q.reserve)
			q.answer = tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}}
			return q
		}
	}
	called, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	q.answer = s.paperWords.TellPaperWords(called, q.request, own)
	noteCall(ctx, q.answer.Outcome, time.Since(start))
	s.afterCall(ctx, q.reserve, q.answer.Outcome)
	q.request.Question, q.request.Turns = "", nil
	return q
}

// questionTold writes what came of the calls of q and answers it, on the
// attempt held again after them: the Check kept, when one was asked and gave an
// answer and the attempt keeps none yet, with no rung given; and the words that
// pass the checks, or the fallback text — with the outcome of a call that gave
// none, with no outcome for words that failed a check. The record of the key
// holds the outcome alone.
func (s *Service) questionTold(ctx context.Context, tx pgx.Tx, profileID string, q asking) (*v1.AskTutorResponse, store.KeyTarget, error) {
	answer := q.ready
	if answer == nil {
		row, err := s.holdPaper(ctx, tx, profileID, q.attemptID, s.clock())
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer = q.worded(ctx)
		if c := q.checking; c != nil && c.outcome() == nil && row.PaperCheck == nil {
			if err := keepCheck(ctx, tx, profileID, row, c.checked); err != nil {
				return nil, store.KeyTarget{}, err
			}
		}
	}
	// An answer of the contract always marshals; the record keeps no words.
	written, _ := proto.Marshal(&v1.AskTutorResponse{Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: q.attemptID, Response: written}, nil
}

// worded is the answer of the calls of q: the model's words once they passed
// the checks of a task from paper; the fallback text with no outcome when they
// failed one; else the fallback text with the outcome of the Check that gave no
// answer, or of the call of the words.
func (q asking) worded(ctx context.Context) *v1.AskTutorResponse {
	if c := q.checking; c != nil && c.outcome() != nil {
		return q.fallback(c.outcome())
	}
	if q.answer.Outcome.Kind != tutor.Answered {
		return q.fallback(outcomeOf(q.answer.Outcome, q.route.AI))
	}
	if check := paperTurnedDown(&q.paper, q.answer); check != "" {
		rpc.NoteWordsCheck(ctx, check)
		return q.fallback(nil)
	}
	return &v1.AskTutorResponse{Words: q.answer.Question}
}
