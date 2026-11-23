package learning

import (
	"cmp"
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/words"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// closeShown is the close of an attempt by the solution shown.
const closeShown = "solution_shown"

// ladderPassed is the wrong answers from which a child is shown the solution:
// the fourth, past the ladder's three rungs.
const ladderPassed = lastRung + 1

// GetHint is a hint the learner asks for, with no AI and no work: before the
// first wrong answer, and on a task with a rubric, the IDs of the task's
// techniques and its leading question, of the instance the attempt got, when it
// has one — which the attempt records as given, each only when the task has it,
// and which is an action on the attempt, as a step is —, none on a task from
// paper, which has GetOpeningWords; after it, the words of the current rung
// that the attempt keeps, or its fallback text, with no question. It repeats by
// the attempt's state.
func (a *AttemptService) GetHint(ctx context.Context, req *v1.GetHintRequest) (*v1.GetHintResponse, error) {
	s := a.s
	if _, err := s.learner(ctx, req.GetProfileId()); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	var answer *v1.GetHintResponse
	err := s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), now)
		switch {
		case err != nil:
			return err
		case h.row.Paper && h.row.WrongAnswers == 0:
			answer = &v1.GetHintResponse{Hint: &v1.GetHintResponse_Techniques{Techniques: &v1.Techniques{}}}
			return nil
		}
		if h.row.WrongAnswers == 0 || rubric(h.task) {
			question := leadingQuestion(h.task)
			given := techniques(h.task)
			if err := store.GiveRequested(ctx, tx, profileID, h.row.ID, len(given) > 0, question != "", now); err != nil {
				return err
			}
			answer = &v1.GetHintResponse{LeadingQuestion: question,
				Hint: &v1.GetHintResponse_Techniques{Techniques: &v1.Techniques{TechniqueIds: given}}}
			return nil
		}
		hint, _, err := rungHint(ctx, tx, profileID, h)
		answer = &v1.GetHintResponse{Hint: &v1.GetHintResponse_Rung{Rung: hint}}
		return err
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// leadingQuestion is the task's leading question, empty when it has none.
func leadingQuestion(task formatv0.Task) string {
	if task.LeadingQuestion == nil {
		return ""
	}
	return string(*task.LeadingQuestion)
}

// rungHint is the current rung of the attempt h, which has a wrong answer, with
// the words the attempt keeps for it or its fallback text — on a task from
// paper, the platform's in the language of the profile's interface — and
// whether "explain differently" was asked for on it.
func rungHint(ctx context.Context, tx pgx.Tx, profileID string, h held) (*v1.RungHint, bool, error) {
	rung := rungOf(h.row.WrongAnswers)
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, rung)
	if err != nil {
		return nil, false, err
	}
	if words := kept.Current().Text; found && words != "" {
		return &v1.RungHint{Rung: int32(rung), Words: words}, kept.Explained, nil
	}
	if h.row.Paper {
		language, _, err := store.PaperWordsOf(ctx, tx, profileID)
		return &v1.RungHint{Rung: int32(rung), Words: tutor.PaperFallback(rung, cmp.Or(language, wordsLanguage))}, found && kept.Explained, err
	}
	at, err := spotOf(ctx, tx, profileID, h, rung)
	if err != nil {
		return nil, false, err
	}
	return &v1.RungHint{Rung: int32(rung), Words: fallback(h.pkg, h.task, rung, at)}, found && kept.Explained, nil
}

// wordsTeller words the tutor's words, as the gate of admission does through
// tutor: whether they are admitted for a learner, and the call. own is the
// value of the request's own key, which the dev mode's stub alone reads, as
// sheetReader has it.
type wordsTeller interface {
	Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	TellWords(ctx context.Context, w tutor.WordsRequest, own string) tutor.WordsAnswer
}

// correctTeller words the tutor's words after a correct answer, as the gate of
// admission does through tutor: whether they are admitted for a learner, by
// their kind of call, CallCorrect, and the call. own is as wordsTeller has it.
type correctTeller interface {
	CorrectWords(category formatv0.AgeCategory, ownKeyProvider string) bool
	TellCorrectWords(ctx context.Context, c tutor.CorrectRequest, own string) tutor.CorrectAnswer
}

// telling is a request for words between the parts of its request.
type telling struct {
	h       held
	rung    int
	wrong   int
	at      spot
	read    store.Words
	request tutor.WordsRequest
	// paper is the words of a task from paper in place of at and request, nil on a
	// task of a module.
	paper  *paperTelling
	answer tutor.WordsAnswer
	// ready is an answer with no call: the words the attempt keeps.
	ready proto.Message
	// called says whether the words were called, between two transactions.
	called bool
	// day is the family day of the account on platform AI, and reserve what the
	// call took of its quota.
	day     quota.Day
	reserve reserve
}

// wordsCall is the kind of call the words of t make, by the request they will
// send: the words with the work when the crop goes, the hint without it else,
// as tutor picks it, and so of a task from paper. The quota reserves by it.
func wordsCall(t telling) tutor.Call {
	if p := t.paper; p != nil {
		return admission.PaperWordsCall(p.request.Ask == tutor.PaperRung)
	}
	return admission.WordsCall(len(t.request.Crop) > 0)
}

// route is the route the words of t are called by.
func (t telling) route() tutor.Route {
	if p := t.paper; p != nil {
		return p.request.Route
	}
	return t.request.Route
}

// call calls the words of t, when it has no answer ready, until finishReserve
// before the request's deadline; own is the value of the request's own key, for
// the stub. The crop goes no further than the call.
func (s *Service) call(ctx context.Context, t telling, own string) telling {
	if t.ready != nil || t.reserve.refused != nil {
		return t
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	if p := t.paper; p != nil {
		t.answer, t.called = s.paperWords.TellPaperWords(ctx, p.request, own), true
		p.request.Statement, p.request.Crop = nil, nil
	} else {
		t.answer, t.called = s.teller.TellWords(ctx, t.request, own), true
		t.request.Crop = nil
	}
	noteCall(ctx, t.answer.Outcome, time.Since(start))
	s.afterCall(ctx, t.reserve, t.answer.Outcome)
	return t
}

// finishing is ctx ended finishReserve before the request's deadline, if it has
// one: a call of AI on it leaves the time of the transaction after it.
func finishing(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(ctx, deadline.Add(-finishReserve))
	}
	return ctx, func() {}
}

// noteCall notes a call of AI in the request's log, with what each model it
// asked spent.
func noteCall(ctx context.Context, o tutor.Outcome, took time.Duration) {
	call := rpc.AICall{Outcome: aiOutcomes[o.Kind], Duration: took}
	for _, u := range o.Usage {
		var written int64
		for _, w := range u.CacheWrites {
			written += w.Tokens
		}
		call.Usage = append(call.Usage, rpc.AIUsage{Provider: string(u.Provider), Model: u.Model,
			InputTokens: u.InputTokens, CacheWriteTokens: written, CacheReadTokens: u.CacheReadTokens,
			OutputTokens: u.OutputTokens, ThinkingTokens: u.ThinkingTokens,
			CostMicroUSD: int64(quota.USD(u.USD)), Billed: u.Billed, Took: u.Latency, Retries: u.Retries, FirstToken: u.FirstToken})
	}
	rpc.NoteAICall(ctx, call)
}

// worded is the words that came of the call of t on its rung: the tutor's
// question, when it passed the leak check and the form check, or the rung's
// fallback text, with the outcome of a call that gave no words — or that the
// quota did not hold, and made none; none when the words came and failed a
// check, which goes to the request's line of the log.
func worded(ctx context.Context, t telling) (store.Words, *v1.Outcome) {
	if t.answer.Outcome.Kind == tutor.Answered {
		var check string
		if p := t.paper; p != nil {
			check = paperTurnedDown(p, t.answer)
		} else {
			check = turnedDown(t.h.pkg, t.h.task, t.answer.Question, words.Hint)
		}
		if check == "" {
			return store.Words{Source: store.SourceModel, Text: t.answer.Question}, nil
		}
		rpc.NoteWordsCheck(ctx, check)
	}
	words := store.Words{Source: store.SourceFallback, Text: fallbackOf(t)}
	switch {
	case t.reserve.refused != nil:
		return words, t.reserve.refused
	case t.answer.Outcome.Kind == tutor.Answered:
		return words, nil
	}
	return words, outcomeOf(t.answer.Outcome, t.route().AI)
}

// fallbackOf is the fallback text of the rung of t: on a task of a module the
// chain of the format (fallback), on a task from paper the platform's in the
// language of the profile's interface (tutor.PaperFallback).
func fallbackOf(t telling) string {
	if p := t.paper; p != nil {
		return tutor.PaperFallback(t.rung, p.language)
	}
	return fallback(t.h.pkg, t.h.task, t.rung, t.at)
}

// routeOf is the route of a learner of category to AI: the own key, when the
// request sends one, or platform AI.
func routeOf(category formatv0.AgeCategory, own *v1.OwnKey) tutor.Route {
	route := tutor.Route{AI: admissionv0.AiRoutePlatformAi, Category: admissionv0.AgeCategory(category)}
	if own != nil {
		route.AI = admissionv0.AiRouteOwnKey
		route.Own = tutor.NewOwnKey(admissionv0.Provider(own.GetProvider()), own.GetKey())
	}
	return route
}

// notCurrent refuses a request that names a wrong answer that is not the
// attempt's last, with how many there are, so that the words never speak of a
// work the learner has written over.
func notCurrent(ctx context.Context, wrong int) error {
	return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT, func(r *v1.Refusal) {
		r.Data = &v1.Refusal_WrongAnswers{WrongAnswers: int32(wrong)}
	})
}

// current says whether a request that names named wrong answers is for the
// words of the attempt's last, of wrong: the same count, or, for "explain
// differently" (thirdOn), any count from the third on — from the fourth wrong
// answer on no rung is new, and the learner reads the third's words still.
func current(named, wrong int, thirdOn bool) bool {
	return named == wrong || (thirdOn && named >= lastRung && named <= wrong)
}

// named checks the wrong answer a request for words names, by its count, on the
// attempt h: a task with a rubric, or no wrong answer yet, has no rung, and one
// that is not the last is "not current".
func named(ctx context.Context, h held, wrongAnswers *int32, thirdOn bool) error {
	if rubric(h.task) {
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	}
	if wrongAnswers == nil {
		return rpc.Malformed("wrong_answers")
	}
	if !current(int(*wrongAnswers), h.row.WrongAnswers, thirdOn) {
		return notCurrent(ctx, h.row.WrongAnswers)
	}
	if h.row.WrongAnswers == 0 {
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_RUNG)
	}
	return nil
}

// keptWords is the words the attempt keeps for the rung of wrongAnswers, held
// in tx: what a repeat gets, while the attempt is open and the wrong answer is
// still current.
func (s *Service) keptWords(ctx context.Context, tx pgx.Tx, profileID, attemptID string, wrongAnswers int32, thirdOn bool) (string, error) {
	h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
	if refusal, ok := rpc.Kept(err); ok {
		// The close is the next request's to write: a repeat answers from the record.
		return "", refusal
	}
	if err != nil {
		return "", err
	}
	if !current(int(wrongAnswers), h.row.WrongAnswers, thirdOn) {
		return "", notCurrent(ctx, h.row.WrongAnswers)
	}
	hint, _, err := rungHint(ctx, tx, profileID, h)
	if err != nil {
		return "", err
	}
	return hint.GetWords(), nil
}

// ownKeyFits refuses an own key, or its provider alone, that a request sends
// for a learner who is not an adult: only the owner's own profile goes by its
// key.
func ownKeyFits(ctx context.Context, category formatv0.AgeCategory, sent bool) error {
	if sent && category != formatv0.AgeCategoryAdult {
		return rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	return nil
}

// ownKeySent is ownKeyFits of a request that sends an own key whole, and
// refuses one with no provider or no key, a form the contract refuses with no
// Refusal: the gate asks the route of the key's provider, and the call takes
// the key too, so a key short of either would pass the one and fail the other.
func ownKeySent(ctx context.Context, category formatv0.AgeCategory, own *v1.OwnKey) error {
	if own != nil && (own.GetProvider() == "" || own.GetKey() == "") {
		return rpc.Malformed("own_key")
	}
	return ownKeyFits(ctx, category, own != nil)
}

// GetRungWords gives the words of the rung a wrong answer gave, which the
// request names by the count of wrong answers of its submission's answer and
// sends the work of again: the tutor's question, once it passed the leak check
// and the form check, with the box of the first mistake as a mark (markOf), or
// the rung's fallback text. The attempt keeps them until it closes. Words that
// do not come are no refusal: the fallback text comes with the outcome. From
// the fourth wrong answer on no rung is new, and the words of the same wrong
// answer asked again, or a repeat under the key, are the words the attempt
// keeps, with no AI and no marks. When the deployment admits the words, the
// call runs between two transactions, as the reading does (rpc.Across).
func (a *AttemptService) GetRungWords(ctx context.Context, req *v1.GetRungWordsRequest) (*v1.GetRungWordsResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	profileID, attemptID := req.GetProfileId(), req.GetAttemptId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceGetRungWordsProcedure,
		GoingFor: s.goingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.GetRungWordsResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			words, err := s.keptWords(ctx, tx, profileID, attemptID, req.GetWrongAnswers(), false)
			answer.Words, answer.Repeated = words, true
			return answer, err
		},
	}
	route := routeOf(category, req.GetOwnKey())
	var admitted bool
	task, paper, known := s.attemptOf(ctx, profileID, attemptID)
	if paper {
		// The words of a task from paper go to the debug mode's log.
		act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, attemptID)
		defer act.End()
		route = act.Route(route)
		admitted = s.paperAdmitted(ctx, true, category, req.GetOwnKey().GetProvider())
	} else {
		// The words take the work of a sheet alone: a workbook page asks them as a
		// task a tap checks, with no work.
		admitted = s.teller.Words(known && sheetRead(task), category, req.GetOwnKey().GetProvider())
	}
	prepare := func(tx pgx.Tx) (telling, error) {
		h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
		if err != nil {
			return telling{}, err
		}
		t, err := s.rungAsked(ctx, tx, h, req, route, admitted)
		if err == nil {
			t.day, err = s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
		}
		if err == nil && admitted && t.ready == nil {
			t.reserve, err = s.reserveFor(ctx, tx, profileID, route, wordsCall(t), 1, t.day)
		}
		return t, err
	}
	var answer *v1.GetRungWordsResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[telling, *v1.GetRungWordsResponse]{
			Prepare: func(tx pgx.Tx, _ string) (telling, store.KeyTarget, error) {
				t, err := prepare(tx)
				return t, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID}, err
			},
			Call: func(ctx context.Context, t telling) telling { return s.call(ctx, t, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, t telling) (*v1.GetRungWordsResponse, store.KeyTarget, error) {
				return s.rungTold(ctx, tx, profileID, t)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetRungWordsResponse, store.KeyTarget, error) {
			t, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if t.ready == nil {
				// No model is admitted for the words: no call, and the fallback text.
				t.answer = tutor.WordsAnswer{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}
			}
			return s.rungTold(ctx, tx, profileID, t)
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

// rungAsked checks a request for the words of a rung on the attempt h, before
// anything is called: the wrong answer it names, a rung that is new, and the
// work, which goes when AI reads the task as a sheet or from paper, and only
// then, as its crop: a workbook page asks as a task a tap checks, with no work.
// The rung whose words the attempt keeps already is answered with them, with no
// call.
func (s *Service) rungAsked(ctx context.Context, tx pgx.Tx, h held, req *v1.GetRungWordsRequest, route tutor.Route, admitted bool) (telling, error) {
	if err := named(ctx, h, req.WrongAnswers, false); err != nil {
		return telling{h: h}, err
	}
	if h.row.WrongAnswers > lastRung {
		return telling{h: h}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	}
	t := telling{h: h, rung: rungOf(h.row.WrongAnswers), wrong: h.row.WrongAnswers}
	var crop []byte
	switch ai := sheetRead(h.task); {
	case h.row.Paper:
		if crop = req.GetWork().GetPaper().GetCrop(); !cropTaken(crop) {
			return t, rpc.Malformed("work")
		}
	case ai != (req.GetWork() != nil):
		return t, rpc.Malformed("work")
	case ai:
		if crop = req.GetWork().GetSheet().GetCrop(); !cropTaken(crop) {
			return t, rpc.Malformed("work")
		}
	}
	kept, found, err := store.RungOf(ctx, tx, req.GetProfileId(), h.row.ID, t.rung)
	if err != nil {
		return t, err
	}
	if found {
		t.ready = keptRung(t.rung, kept)
		return t, nil
	}
	if t, err = spotted(ctx, tx, req.GetProfileId(), t); err != nil || !admitted {
		return t, err
	}
	return wordsCalled(ctx, tx, req.GetProfileId(), t, route, crop)
}

// spotted is t with what its words speak of: the answer slot of its rung on a
// task of a module, and on a task from paper the tasks of the crop with the
// rung's (paperTold).
func spotted(ctx context.Context, tx pgx.Tx, profileID string, t telling) (telling, error) {
	var err error
	if t.h.row.Paper {
		t.paper, err = paperTold(ctx, tx, profileID, t.h, t.rung)
	} else {
		t.at, err = spotOf(ctx, tx, profileID, t.h, t.rung)
	}
	return t, err
}

// keptRung is the answer of the words of rung that the attempt keeps already:
// a repeat, with no marks.
func keptRung(rung int, kept store.Rung) *v1.GetRungWordsResponse {
	return &v1.GetRungWordsResponse{Rung: int32(rung), Words: kept.Current().Text, Repeated: true}
}

// wordsCalled is t with the call of its words, by the route, with the crop when
// the work goes and the tutor's questions asked so far, once the request's
// deadline leaves time for the call and the transaction after it.
func wordsCalled(ctx context.Context, tx pgx.Tx, profileID string, t telling, route tutor.Route, crop []byte) (telling, error) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return t, connect.NewError(connect.CodeDeadlineExceeded, "too little time to word the rung")
	}
	asked, err := store.AskedQuestions(ctx, tx, profileID, t.h.row.ID)
	if err != nil {
		return t, err
	}
	if t.paper != nil {
		t.paper.request = paperRequest(t, route, crop, asked)
		return t, nil
	}
	t.request, err = wordsAsked(t.h, route, t.rung, t.at, crop, asked)
	return t, err
}

// heldAgain is the attempt of t held again after its call of words, and the
// rung as the attempt keeps it now: another device may have closed the
// attempt, sent another wrong answer, or had the rung worded, while the words
// were called. The attempt's row, held, puts such requests one after the other.
func (s *Service) heldAgain(ctx context.Context, tx pgx.Tx, profileID string, t telling) (held, store.Rung, bool, error) {
	h, err := s.holdAny(ctx, tx, profileID, t.h.row.ID, s.clock())
	if err != nil {
		return held{}, store.Rung{}, false, err
	}
	if h.row.WrongAnswers != t.wrong {
		return held{}, store.Rung{}, false, notCurrent(ctx, h.row.WrongAnswers)
	}
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, t.rung)
	return h, kept, found, err
}

// rungTold writes what came of the words of t and answers it, on the attempt
// held again after a call. The record of the key holds no words: a repeat reads
// those the attempt keeps.
func (s *Service) rungTold(ctx context.Context, tx pgx.Tx, profileID string, t telling) (*v1.GetRungWordsResponse, store.KeyTarget, error) {
	answer, ok := t.ready.(*v1.GetRungWordsResponse)
	if !ok && t.called {
		_, kept, found, err := s.heldAgain(ctx, tx, profileID, t)
		switch {
		case err != nil:
			return nil, store.KeyTarget{}, err
		case found:
			answer, ok = keptRung(t.rung, kept), true
		}
	}
	if !ok {
		words, outcome := worded(ctx, t)
		if err := store.WriteRung(ctx, tx, profileID, t.h.row.ID, t.rung, words); err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer = &v1.GetRungWordsResponse{Rung: int32(t.rung), Words: words.Text, Outcome: outcome}
		if kind := markOf(t); t.answer.Box != nil && words.Source == store.SourceModel && kind != v1.MarkKind_MARK_KIND_UNSPECIFIED {
			answer.Marks = []*v1.Mark{{Kind: kind, Place: &v1.Mark_CropBox{CropBox: t.answer.Box}}}
		}
	}
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.GetRungWordsResponse{Rung: answer.GetRung(), Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID, Response: written}, nil
}

// markOf is the kind of the mark of the box of the first mistake that the
// model's words of t come with: the frame on a task of a module, and on a task
// from paper the wavy line in the hint mode "Show where" and none in "Questions
// only".
func markOf(t telling) v1.MarkKind {
	switch p := t.paper; {
	case p == nil:
		return v1.MarkKind_MARK_KIND_FRAME
	case p.showWhere:
		return v1.MarkKind_MARK_KIND_WAVY_LINE
	}
	return v1.MarkKind_MARK_KIND_UNSPECIFIED
}

// ExplainDifferently words the current rung anew, without the work: one call of
// AI on a rung, for the wrong answer the request names, with the tutor's
// questions asked so far. Its words, once they passed the leak check and the
// form check, or the rung's fallback text, are what the learner reads on the
// rung from then on; when they are the words the learner reads already, the
// outcome says so, and the right is spent all the same, as it is once the call
// went. On platform AI a deployment that admits no model for the words makes no
// call and refuses "not admitted", and the right stays; on an own key that is
// an outcome. From the fourth wrong answer on it words the third rung, if it
// was not asked for there already, for the count of any wrong answer from the
// third on. A repeat under the key gets the words the attempt keeps, with the
// outcome it got, and no AI.
func (a *AttemptService) ExplainDifferently(ctx context.Context, req *v1.ExplainDifferentlyRequest) (*v1.ExplainDifferentlyResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	profileID, attemptID := req.GetProfileId(), req.GetAttemptId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceExplainDifferentlyProcedure,
		GoingFor: s.goingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.ExplainDifferentlyResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Words, err = s.keptWords(ctx, tx, profileID, attemptID, req.GetWrongAnswers(), true)
			return answer, err
		},
	}
	route := routeOf(category, req.GetOwnKey())
	var admitted bool
	if s.paperAttempt(ctx, profileID, attemptID) {
		act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, attemptID)
		defer act.End()
		route = act.Route(route)
		admitted = s.paperAdmitted(ctx, false, category, req.GetOwnKey().GetProvider())
	} else {
		admitted = s.teller.Words(false, category, req.GetOwnKey().GetProvider())
	}
	prepare := func(tx pgx.Tx) (telling, error) {
		h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
		if err != nil {
			return telling{}, err
		}
		t, err := s.otherAsked(ctx, tx, h, req, route, admitted)
		if err == nil {
			t.day, err = s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
		}
		if err == nil && admitted {
			t.reserve, err = s.reserveFor(ctx, tx, profileID, route, wordsCall(t), 1, t.day)
		}
		if err == nil && t.reserve.refused != nil {
			// "Explain differently" makes no call without the reserve, and the right stays.
			err = refusal(ctx, t.reserve.refused)
		}
		return t, err
	}
	var answer *v1.ExplainDifferentlyResponse
	switch {
	case admitted:
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[telling, *v1.ExplainDifferentlyResponse]{
			Prepare: func(tx pgx.Tx, _ string) (telling, store.KeyTarget, error) {
				t, err := prepare(tx)
				return t, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID}, err
			},
			Call: func(ctx context.Context, t telling) telling { return s.call(ctx, t, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, t telling) (*v1.ExplainDifferentlyResponse, store.KeyTarget, error) {
				return s.otherTold(ctx, tx, profileID, t)
			},
		})
	case req.GetOwnKey() == nil:
		// On platform AI no call goes without the admission, and the right stays.
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.ExplainDifferentlyResponse, store.KeyTarget, error) {
			if _, err := prepare(tx); err != nil {
				return nil, store.KeyTarget{}, err
			}
			return nil, store.KeyTarget{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
		})
	default:
		// On the own key it is an outcome: the learner reads the same words, and
		// the right stays, since no call went.
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.ExplainDifferentlyResponse, store.KeyTarget, error) {
			t, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			out := &v1.ExplainDifferentlyResponse{Words: t.read.Text, Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}}
			return out, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID, Response: noWords(out)}, nil
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

// noWords is what the record of a key holds of an answer of "explain
// differently": its outcome, and no words.
func noWords(answer *v1.ExplainDifferentlyResponse) []byte {
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.ExplainDifferentlyResponse{Outcome: answer.GetOutcome()})
	return written
}

// otherAsked checks a request to explain the rung differently on the attempt h,
// before anything is called: the wrong answer it names, and a rung where it was
// not asked for yet. It gives the words the learner reads on the rung, and, when
// the words are admitted, the call.
func (s *Service) otherAsked(ctx context.Context, tx pgx.Tx, h held, req *v1.ExplainDifferentlyRequest, route tutor.Route, admitted bool) (telling, error) {
	if err := named(ctx, h, req.WrongAnswers, true); err != nil {
		return telling{h: h}, err
	}
	t := telling{h: h, rung: rungOf(h.row.WrongAnswers), wrong: h.row.WrongAnswers}
	kept, found, err := store.RungOf(ctx, tx, req.GetProfileId(), h.row.ID, t.rung)
	switch {
	case err != nil:
		return t, err
	case found && kept.Explained:
		return t, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	}
	if t, err = spotted(ctx, tx, req.GetProfileId(), t); err != nil {
		return t, err
	}
	t.read = readWords(t, kept, found)
	if !admitted {
		return t, nil
	}
	return wordsCalled(ctx, tx, req.GetProfileId(), t, route, nil)
}

// readWords is the words the learner reads on the rung of t: those the attempt
// keeps, or its fallback text.
func readWords(t telling, kept store.Rung, found bool) store.Words {
	if found && kept.Words.Text != "" {
		return kept.Words
	}
	return store.Words{Source: store.SourceFallback, Text: fallbackOf(t)}
}

// otherTold writes what came of "explain differently" of t, on the attempt held
// again, and answers it: the right on the rung is spent, since the call went. A
// request beside it that had the rung explained meanwhile takes the right, and
// this one is "already asked".
func (s *Service) otherTold(ctx context.Context, tx pgx.Tx, profileID string, t telling) (*v1.ExplainDifferentlyResponse, store.KeyTarget, error) {
	h, kept, found, err := s.heldAgain(ctx, tx, profileID, t)
	switch {
	case err != nil:
		return nil, store.KeyTarget{}, err
	case found && kept.Explained:
		return nil, store.KeyTarget{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ALREADY_ASKED)
	}
	t.read = readWords(t, kept, found)
	other, outcome := worded(ctx, t)
	if outcome == nil && other.Text == t.read.Text {
		outcome = &v1.Outcome{Cause: v1.Cause_CAUSE_NO_OTHER_WORDS}
	}
	if err := store.ExplainRung(ctx, tx, profileID, h.row.ID, t.rung, t.read, other, s.clock()); err != nil {
		return nil, store.KeyTarget{}, err
	}
	answer := &v1.ExplainDifferentlyResponse{Words: other.Text, Outcome: outcome}
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID, Response: noWords(answer)}, nil
}

// correctPolicy is the version of the privacy policy from which the words after
// a correct answer go to a provider: the first that names what goes to AI for
// them — the key, the solution and the techniques of the task.
const correctPolicy = 3

// praising is a request for the words after a correct answer between the parts
// of its request.
type praising struct {
	row     store.Attempt
	pkg     *modulepackage.Package
	task    formatv0.Task
	request tutor.CorrectRequest
	answer  tutor.CorrectAnswer
	// called says whether the words were called, between two transactions.
	called bool
	// day is the family day of the account on platform AI, and reserve what the
	// call took of its quota.
	day     quota.Day
	reserve reserve
}

// policyCovers says whether the words after a correct answer may go on the day
// of at, in UTC: on the stub always, since nothing reaches a provider; to a
// provider once the privacy policy in force on that day of the texts api serves
// is of version correctPolicy or later.
func (s *Service) policyCovers(at time.Time) bool {
	if s.stubbed {
		return true
	}
	version, ok := s.texts.On(at).Current(legal.PrivacyPolicy)
	return ok && version >= correctPolicy
}

// GetCorrectAnswerWords gives the tutor's words after a correct answer, once,
// on the attempt that the answer has just closed, while the profile has begun
// no other: an attempt closed otherwise, or open, one the profile left for
// another, and one whose words were given already get "no more words". The
// words are the model's praise and its question on the technique that helped,
// once they passed the leak check and the form check of tutor's words.Correct;
// words that fail a check, or a call that gives none, leave the answer without
// words, with the outcome of a call that gave none, and the right stays: a
// later request may ask again. The work goes when AI read the task, as the crop
// of a sheet, and only to the call. When the deployment admits the words and
// the privacy policy in force on the day covers them, the call runs between two
// transactions (rpc.Across); otherwise the answer holds no words, with the
// outcome "not admitted", and nothing is spent. A repeat under the key gets the
// answer without words: the server keeps none.
func (a *AttemptService) GetCorrectAnswerWords(ctx context.Context, req *v1.GetCorrectAnswerWordsRequest) (*v1.GetCorrectAnswerWordsResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceGetCorrectAnswerWordsProcedure,
		GoingFor: s.goingFor,
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.GetCorrectAnswerWordsResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Words, answer.Repeated = nil, true
			return answer, nil
		},
	}
	route := routeOf(category, req.GetOwnKey())
	admitted := s.praiser.CorrectWords(category, req.GetOwnKey().GetProvider()) && s.policyCovers(s.clock())
	prepare := func(tx pgx.Tx) (praising, error) {
		p, err := s.praiseAsked(ctx, tx, req, route, admitted)
		if err == nil {
			p.day, err = s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
		}
		if err == nil && admitted {
			p.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallCorrect, 1, p.day)
		}
		return p, err
	}
	var answer *v1.GetCorrectAnswerWordsResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[praising, *v1.GetCorrectAnswerWordsResponse]{
			Prepare: func(tx pgx.Tx, _ string) (praising, store.KeyTarget, error) {
				p, err := prepare(tx)
				return p, store.KeyTarget{ProfileID: profileID, AttemptID: p.row.ID}, err
			},
			Call: func(ctx context.Context, p praising) praising { return s.praise(ctx, p, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, p praising) (*v1.GetCorrectAnswerWordsResponse, store.KeyTarget, error) {
				return s.praised(ctx, tx, profileID, p)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetCorrectAnswerWordsResponse, store.KeyTarget, error) {
			p, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			// No model is admitted for the words, or the policy in force does not
			// cover them yet: no call, and nothing is spent.
			out := &v1.GetCorrectAnswerWordsResponse{Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}}
			return out, store.KeyTarget{ProfileID: profileID, AttemptID: p.row.ID}, nil
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

// praiseAsked checks a request for the words after a correct answer on the
// attempt it names, held, before anything is called: an attempt the profile
// has, whose words may still be given, on a package the index knows, and the
// work, which goes when AI reads the task as a sheet, and only then, as its
// crop: a workbook page asks as a task a tap checks, with no work. When the
// words are admitted, it gives their call.
func (s *Service) praiseAsked(ctx context.Context, tx pgx.Tx, req *v1.GetCorrectAnswerWordsRequest, route tutor.Route, admitted bool) (praising, error) {
	profileID := req.GetProfileId()
	if !store.CanonicalID(req.GetAttemptId()) {
		return praising{}, store.ErrNotFound
	}
	row, err := store.HoldAttempt(ctx, tx, profileID, req.GetAttemptId())
	switch {
	case err != nil:
		return praising{}, err
	case row.Paper:
		return praising{}, store.ErrPaper
	}
	p := praising{row: row}
	if err := praisable(ctx, tx, profileID, row); err != nil {
		return p, err
	}
	pkg, ok := s.set.Packages[formatv0.Digest(row.PackageDigest)]
	if !ok {
		return p, errPackageUnknown
	}
	if p.task, err = instanceAgain(pkg, formatv0.TaskID(row.TaskID), row.Instance); err != nil {
		return p, err
	}
	p.pkg = pkg
	ai := sheetRead(p.task)
	var crop []byte
	if ai {
		crop = req.GetWork().GetSheet().GetCrop()
	}
	if ai != (req.GetWork() != nil) || (ai && !cropTaken(crop)) {
		return p, rpc.Malformed("work")
	}
	if !admitted {
		return p, nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return p, connect.NewError(connect.CodeDeadlineExceeded, "too little time to word the praise")
	}
	p.request, err = correctAsked(pkg, p.task, route, crop)
	return p, err
}

// praisable refuses the words after a correct answer on the attempt row, held,
// with "no more words": an attempt no correct answer closed, or open, one the
// profile has begun another after, and one whose words were given already.
func praisable(ctx context.Context, tx pgx.Tx, profileID string, row store.Attempt) error {
	last, err := store.LastAttempt(ctx, tx, profileID, row.ID)
	switch {
	case err != nil:
		return err
	case row.CloseReason == nil || *row.CloseReason != closeCorrect || !last || row.CorrectWordsGiven:
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	}
	return nil
}

// praise calls the words after a correct answer of p, when the quota holds the
// call, until finishReserve before the request's deadline; own is the value of
// the request's own key, for the stub. The crop goes no further than the call.
func (s *Service) praise(ctx context.Context, p praising, own string) praising {
	if p.reserve.refused != nil {
		return p
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	p.answer, p.called = s.praiser.TellCorrectWords(ctx, p.request, own), true
	p.request.Crop = nil
	noteCall(ctx, p.answer.Outcome, time.Since(start))
	s.afterCall(ctx, p.reserve, p.answer.Outcome)
	return p
}

// praised answers what came of the words after a correct answer of p, on the
// attempt held again after the call: a request beside it that had the words
// given meanwhile, or a new attempt of the profile, leaves this one "no more
// words". Words that passed the checks are given, and the attempt records so;
// those that failed one go nowhere, and the request's line names the check. The
// record of the key holds no words.
func (s *Service) praised(ctx context.Context, tx pgx.Tx, profileID string, p praising) (*v1.GetCorrectAnswerWordsResponse, store.KeyTarget, error) {
	if p.called {
		row, err := store.HoldAttempt(ctx, tx, profileID, p.row.ID)
		if err == nil {
			err = praisable(ctx, tx, profileID, row)
		}
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
	}
	answer := &v1.GetCorrectAnswerWordsResponse{}
	switch {
	case p.reserve.refused != nil:
		answer.Outcome = p.reserve.refused
	case p.answer.Outcome.Kind != tutor.Answered:
		answer.Outcome = outcomeOf(p.answer.Outcome, p.request.Route.AI)
	default:
		if check := turnedDown(p.pkg, p.task, p.answer.Words, words.Correct); check != "" {
			rpc.NoteWordsCheck(ctx, check)
			break
		}
		if err := store.GiveCorrectWords(ctx, tx, profileID, p.row.ID); err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer.Words = &p.answer.Words
	}
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.GetCorrectAnswerWordsResponse{Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: p.row.ID, Response: written}, nil
}

// canner is what the showing of a solution asks of the rights: authz.Rights.
type canner interface {
	Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error)
}

// ShowSolution shows the solution and closes the attempt "solution shown",
// under its idempotency key, by a learning action or by an action of an adult,
// whose right can_manage the handler checks: the checks before it hold
// can_learn alone. A task with a rubric has nothing to show this way; a child
// is shown it from the fourth wrong answer on, by either kind of action, and,
// while the child's setting "ask before showing" is on, by an adult's alone; a
// learner of the category adult from the beginning. The close deletes the words
// of the rungs. The answer, the task solved, is what the key's record holds,
// with what comes next: in a lesson, the next segment; outside a lesson, the
// engine's choice, on the route own_key_provider names.
func (a *AttemptService) ShowSolution(ctx context.Context, req *v1.ShowSolutionRequest) (*v1.ShowSolutionResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeyFits(ctx, category, req.GetOwnKeyProvider() != ""); err != nil {
		return nil, err
	}
	switch req.GetAction() {
	case v1.Action_ACTION_LEARNING:
	case v1.Action_ACTION_ADULT:
		caller, _ := rpc.CallerFrom(ctx)
		can, err := a.rights.Can(ctx, caller.AccountID, authz.CanManage, req.GetProfileId())
		switch {
		case err != nil:
			return nil, rpc.Failed(ctx, err)
		case !can:
			return nil, connect.NewError(connect.CodePermissionDenied, "")
		}
	default:
		return nil, rpc.Malformed("action")
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.AttemptServiceShowSolutionProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.ShowSolutionResponse, store.KeyTarget, error) {
			now := s.clock()
			h, err := s.hold(ctx, tx, profileID, req.GetAttemptId(), now)
			if errors.Is(err, store.ErrPaper) {
				// The solution of a task from paper is never shown.
				err = rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SHOWING)
			}
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if err := s.showable(ctx, tx, profileID, h, category, req.GetAction()); err != nil {
				return nil, store.KeyTarget{}, err
			}
			out, err := solved(h.pkg, h.task)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			if err := s.close(ctx, tx, profileID, h.row.ID, closeShown, now); err != nil {
				return nil, store.KeyTarget{}, err
			}
			next, err := s.nextAfter(ctx, tx, profileID, h.row, category, req.GetOwnKeyProvider(), now, false)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return &v1.ShowSolutionResponse{Solved: out, Next: next}, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID}, nil
		})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}

// showable checks that the solution of the attempt h may be shown to a learner
// of category by action.
func (s *Service) showable(ctx context.Context, tx pgx.Tx, profileID string, h held, category formatv0.AgeCategory, action v1.Action) error {
	if rubric(h.task) {
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SHOWING)
	}
	if category == formatv0.AgeCategoryAdult {
		return nil
	}
	if h.row.WrongAnswers < ladderPassed {
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_LADDER_NOT_PASSED)
	}
	asks, err := store.AsksBeforeShowing(ctx, tx, profileID)
	switch {
	case err != nil:
		return err
	case asks && action == v1.Action_ACTION_LEARNING:
		return rpc.Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	}
	return nil
}
