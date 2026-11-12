package learning

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// closeShown is the close of an attempt by the solution shown (R09).
const closeShown = "solution_shown"

// ladderPassed is the wrong answers from which a child is shown the solution:
// the fourth, past the ladder's three rungs (R09).
const ladderPassed = lastRung + 1

// GetHint is a hint the learner asks for, with no AI and no work (R10): before
// the first wrong answer, and on a task with a rubric, the IDs of the task's
// techniques and its leading question, of the instance the attempt got, when it
// has one — which the attempt records as given, each only when the task has
// it, and which is an action on the attempt, as a step is; after it, the words
// of the current rung that the attempt keeps, or its fallback text, with no
// question (R47; R108 spec). It repeats by the attempt's state.
func (a *AttemptService) GetHint(ctx context.Context, req *v1.GetHintRequest) (*v1.GetHintResponse, error) {
	s := a.s
	if _, err := s.learner(ctx, req.GetProfileId()); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	var answer *v1.GetHintResponse
	err := s.act(ctx, profileID, req.GetAttemptId(), func(tx pgx.Tx, h held, now time.Time) error {
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
// the words the attempt keeps for it or its fallback text, and whether
// "explain differently" was asked for on it.
func rungHint(ctx context.Context, tx pgx.Tx, profileID string, h held) (*v1.RungHint, bool, error) {
	rung := rungOf(h.row.WrongAnswers)
	kept, found, err := store.RungOf(ctx, tx, profileID, h.row.ID, rung)
	if err != nil {
		return nil, false, err
	}
	if words := kept.Current().Text; found && words != "" {
		return &v1.RungHint{Rung: int32(rung), Words: words}, kept.Explained, nil
	}
	at, err := spotOf(ctx, tx, profileID, h, rung)
	if err != nil {
		return nil, false, err
	}
	return &v1.RungHint{Rung: int32(rung), Words: fallback(h.pkg, h.task, rung, at)}, found && kept.Explained, nil
}

// wordsTeller words the tutor's words, as the gate of admission does through
// tutor: whether they are admitted for a learner, and the call.
type wordsTeller interface {
	Words(withWork bool, category formatv0.AgeCategory, ownKeyProvider string) bool
	TellWords(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer
}

// telling is a request for words between the parts of its request: the attempt
// and its rung, the answer slot the rung speaks of, the call of words and what
// came of it — or, when no call goes, the answer it gets already.
type telling struct {
	h       held
	rung    int
	wrong   int
	at      spot
	read    store.Words
	request tutor.WordsRequest
	answer  tutor.WordsAnswer
	// ready is an answer with no call: the words the attempt keeps.
	ready proto.Message
	// called says whether the words were called, between two transactions.
	called bool
}

// call calls the words of t, when it has no answer ready, until finishReserve
// before the request's deadline, and notes the call in the request's log. The
// crop goes no further than the call.
func (s *Service) call(ctx context.Context, t telling) telling {
	if t.ready != nil {
		return t
	}
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-finishReserve))
		defer cancel()
	}
	start := time.Now()
	t.answer, t.called = s.teller.TellWords(ctx, t.request), true
	t.request.Crop = nil
	noteCall(ctx, t.answer.Outcome, time.Since(start))
	return t
}

// noteCall notes a call of AI in the request's log: its outcome, how long it
// took, and what each model it asked spent (R12).
func noteCall(ctx context.Context, o tutor.Outcome, took time.Duration) {
	call := rpc.AICall{Outcome: aiOutcomes[o.Kind], Duration: took}
	for _, u := range o.Usage {
		call.Usage = append(call.Usage, rpc.AIUsage{Provider: string(u.Provider), Model: u.Model,
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, ThinkingTokens: u.ThinkingTokens})
	}
	rpc.NoteAICall(ctx, call)
}

// worded is the words that came of the call of t on its rung: the tutor's
// question, when it passed the leak check and the form check, or the rung's
// fallback text, with the cause of an outcome that gave no words; none when the
// words came and failed a check (contract, § 15).
func worded(t telling) (store.Words, v1.Cause) {
	if t.answer.Outcome.Kind == tutor.Answered && passes(t.h.pkg, t.h.task, t.answer.Question) {
		return store.Words{Source: store.SourceModel, Text: t.answer.Question}, v1.Cause_CAUSE_UNSPECIFIED
	}
	words := store.Words{Source: store.SourceFallback, Text: fallback(t.h.pkg, t.h.task, t.rung, t.at)}
	if t.answer.Outcome.Kind == tutor.Answered {
		return words, v1.Cause_CAUSE_UNSPECIFIED
	}
	return words, outcomeCause(t.answer.Outcome.Kind, t.request.Route.AI)
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
// work the learner has written over (R97 spec).
func notCurrent(ctx context.Context, wrong int) error {
	return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_CURRENT, func(r *v1.Refusal) {
		r.Data = &v1.Refusal_WrongAnswers{WrongAnswers: int32(wrong)}
	})
}

// current says whether a request that names named wrong answers is for the
// words of the attempt's last, of wrong: the same count, or, for "explain
// differently" (thirdOn), any count from the third on — from the fourth wrong
// answer on no rung is new, and the learner reads the third's words still (R09).
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
	h, err := s.hold(ctx, tx, profileID, attemptID, s.clock())
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

// ownKeyFits refuses an own key that a request sends for a learner who is not
// an adult: only the owner's own profile goes by its key (contract, § 15).
func ownKeyFits(ctx context.Context, category formatv0.AgeCategory, sent bool) error {
	if sent && category != formatv0.AgeCategoryAdult {
		return rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	}
	return nil
}

// ownKeySent is ownKeyFits of a request that sends an own key whole, and
// refuses one with no provider or no key: the gate asks the route of the key's
// provider, and the call takes the key too, so a key short of either would pass
// the one and fail the other (R54).
func ownKeySent(ctx context.Context, category formatv0.AgeCategory, own *v1.OwnKey) error {
	if own != nil && (own.GetProvider() == "" || own.GetKey() == "") {
		return rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	return ownKeyFits(ctx, category, own != nil)
}

// GetRungWords gives the words of the rung a wrong answer gave, which the
// request names by the count of wrong answers of its submission's answer and
// sends the work of again (R10, R47): the tutor's question, once it passed the
// leak check and the form check, with the box of the first mistake as a mark,
// or the rung's fallback text. The attempt keeps them until it closes. Words
// that do not come are no refusal: the fallback text comes with the outcome.
// From the fourth wrong answer on no rung is new, and the words of the same
// wrong answer asked again, or a repeat under the key, are the words the
// attempt keeps, with no AI and no marks. When the deployment admits the words,
// the call runs between two transactions, as the reading does (rpc.Across).
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
	withWork := s.readsAI(ctx, profileID, attemptID)
	admitted := s.teller.Words(withWork, category, req.GetOwnKey().GetProvider())
	prepare := func(tx pgx.Tx) (telling, error) {
		h, err := s.hold(ctx, tx, profileID, attemptID, s.clock())
		if err != nil {
			return telling{}, err
		}
		return s.rungAsked(ctx, tx, h, req, route, admitted)
	}
	var answer *v1.GetRungWordsResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[telling, *v1.GetRungWordsResponse]{
			Prepare: func(tx pgx.Tx, _ string) (telling, store.KeyTarget, error) {
				t, err := prepare(tx)
				return t, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID}, err
			},
			Call: s.call,
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
// work, which goes when AI reads a slot of the task, and only then, as the crop
// of a sheet. The rung whose words the attempt keeps already is answered with
// them, with no call.
func (s *Service) rungAsked(ctx context.Context, tx pgx.Tx, h held, req *v1.GetRungWordsRequest, route tutor.Route, admitted bool) (telling, error) {
	if err := named(ctx, h, req.WrongAnswers, false); err != nil {
		return telling{h: h}, err
	}
	if h.row.WrongAnswers > lastRung {
		return telling{h: h}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
	}
	t := telling{h: h, rung: rungOf(h.row.WrongAnswers), wrong: h.row.WrongAnswers}
	ai := !byTap(h.task)
	if ai != (req.GetWork() != nil) {
		return t, rpc.Malformed("work")
	}
	var crop []byte
	if ai {
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
	if t.at, err = spotOf(ctx, tx, req.GetProfileId(), h, t.rung); err != nil {
		return t, err
	}
	if !admitted {
		return t, nil
	}
	t.request, err = wordsCalled(ctx, tx, req.GetProfileId(), t, route, crop)
	return t, err
}

// keptRung is the answer of the words of rung that the attempt keeps already:
// a repeat, with no marks.
func keptRung(rung int, kept store.Rung) *v1.GetRungWordsResponse {
	return &v1.GetRungWordsResponse{Rung: int32(rung), Words: kept.Current().Text, Repeated: true}
}

// wordsCalled is the call of words of t, by the route, with the crop when the
// work goes and the tutor's questions asked so far, once the request's deadline
// leaves time for the call and the transaction after it.
func wordsCalled(ctx context.Context, tx pgx.Tx, profileID string, t telling, route tutor.Route, crop []byte) (tutor.WordsRequest, error) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return tutor.WordsRequest{}, connect.NewError(connect.CodeDeadlineExceeded, "too little time to word the rung")
	}
	asked, err := store.AskedQuestions(ctx, tx, profileID, t.h.row.ID)
	if err != nil {
		return tutor.WordsRequest{}, err
	}
	return wordsAsked(t.h, route, t.rung, t.at, crop, asked)
}

// heldAgain is the attempt of t held again after its call of words, and the
// rung as the attempt keeps it now: another device may have closed the
// attempt, sent another wrong answer, or had the rung worded, while the words
// were called. The attempt's row, held, puts such requests one after the other.
func (s *Service) heldAgain(ctx context.Context, tx pgx.Tx, profileID string, t telling) (held, store.Rung, bool, error) {
	h, err := s.hold(ctx, tx, profileID, t.h.row.ID, s.clock())
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
		words, cause := worded(t)
		if err := store.WriteRung(ctx, tx, profileID, t.h.row.ID, t.rung, words); err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer = &v1.GetRungWordsResponse{Rung: int32(t.rung), Words: words.Text}
		if cause != v1.Cause_CAUSE_UNSPECIFIED {
			answer.Outcome = &v1.Outcome{Cause: cause}
		}
		if box := t.answer.Box; box != nil && words.Source == store.SourceModel {
			answer.Marks = []*v1.Mark{{Kind: v1.MarkKind_MARK_KIND_FRAME, Place: &v1.Mark_CropBox{CropBox: box}}}
		}
	}
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.GetRungWordsResponse{Rung: answer.GetRung(), Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID, Response: written}, nil
}

// ExplainDifferently words the current rung anew, without the work (R10, R47):
// one call of AI on a rung, for the wrong answer the request names, with the
// tutor's questions asked so far. Its words, once they passed the leak check and
// the form check, or the rung's fallback text, are what the learner reads on
// the rung from then on; when they are the words the learner reads already,
// the outcome says so, and the right is spent all the same, as it is once the
// call went. On platform AI a deployment that admits no model for the words
// makes no call and refuses "not admitted", and the right stays; on an own key
// that is an outcome. From the fourth wrong answer on it words the third rung,
// if it was not asked for there already, for the count of any wrong answer from
// the third on. A repeat under the key gets the words the attempt keeps, with
// the outcome it got, and no AI.
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
	admitted := s.teller.Words(false, category, req.GetOwnKey().GetProvider())
	prepare := func(tx pgx.Tx) (telling, error) {
		h, err := s.hold(ctx, tx, profileID, attemptID, s.clock())
		if err != nil {
			return telling{}, err
		}
		return s.otherAsked(ctx, tx, h, req, route, admitted)
	}
	var answer *v1.ExplainDifferentlyResponse
	switch {
	case admitted:
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[telling, *v1.ExplainDifferentlyResponse]{
			Prepare: func(tx pgx.Tx, _ string) (telling, store.KeyTarget, error) {
				t, err := prepare(tx)
				return t, store.KeyTarget{ProfileID: profileID, AttemptID: t.h.row.ID}, err
			},
			Call: s.call,
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
	if t.at, err = spotOf(ctx, tx, req.GetProfileId(), h, t.rung); err != nil {
		return t, err
	}
	t.read = readWords(h, t, kept, found)
	if !admitted {
		return t, nil
	}
	t.request, err = wordsCalled(ctx, tx, req.GetProfileId(), t, route, nil)
	return t, err
}

// readWords is the words the learner reads on the rung of t: those the attempt
// keeps, or its fallback text.
func readWords(h held, t telling, kept store.Rung, found bool) store.Words {
	if found && kept.Words.Text != "" {
		return kept.Words
	}
	return store.Words{Source: store.SourceFallback, Text: fallback(h.pkg, h.task, t.rung, t.at)}
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
	t.read = readWords(h, t, kept, found)
	other, cause := worded(t)
	if cause == v1.Cause_CAUSE_UNSPECIFIED && other.Text == t.read.Text {
		cause = v1.Cause_CAUSE_NO_OTHER_WORDS
	}
	if err := store.ExplainRung(ctx, tx, profileID, h.row.ID, t.rung, t.read, other, s.clock()); err != nil {
		return nil, store.KeyTarget{}, err
	}
	answer := &v1.ExplainDifferentlyResponse{Words: other.Text}
	if cause != v1.Cause_CAUSE_UNSPECIFIED {
		answer.Outcome = &v1.Outcome{Cause: cause}
	}
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID, Response: noWords(answer)}, nil
}

// GetCorrectAnswerWords gives the tutor's words after a correct answer, once,
// on the attempt that the answer has just closed, while the profile has begun
// no other (R09): an attempt closed otherwise, or open, and one the profile
// left for another, get "no more words". tutor has no call for these words yet
// (В47 RUN.md), so the answer holds none, with the outcome "not admitted": no
// call went, and nothing is spent. The work is checked for its form and goes
// nowhere. A repeat under the key gets the same, without words: the server
// keeps none.
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
		Method: apiv1connect.AttemptServiceGetCorrectAnswerWordsProcedure,
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.GetCorrectAnswerWordsResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Words, answer.Repeated = nil, true
			return answer, nil
		},
	}
	answer, err := rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.GetCorrectAnswerWordsResponse, store.KeyTarget, error) {
		if !store.CanonicalID(req.GetAttemptId()) {
			return nil, store.KeyTarget{}, store.ErrNotFound
		}
		row, err := store.HoldAttempt(ctx, tx, profileID, req.GetAttemptId())
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		last, err := store.LastAttempt(ctx, tx, profileID, row.ID)
		switch {
		case err != nil:
			return nil, store.KeyTarget{}, err
		case row.CloseReason == nil || *row.CloseReason != closeCorrect || !last:
			return nil, store.KeyTarget{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_MORE_WORDS)
		}
		pkg, ok := s.set.Packages[formatv0.Digest(row.PackageDigest)]
		if !ok {
			return nil, store.KeyTarget{}, errPackageUnknown
		}
		task, err := instanceAgain(pkg, formatv0.TaskID(row.TaskID), row.Instance)
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		ai := !byTap(task)
		if ai != (req.GetWork() != nil) || (ai && !cropTaken(req.GetWork().GetSheet().GetCrop())) {
			return nil, store.KeyTarget{}, rpc.Malformed("work")
		}
		out := &v1.GetCorrectAnswerWordsResponse{Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}}
		return out, store.KeyTarget{ProfileID: profileID, AttemptID: row.ID}, nil
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	rpc.NoteOutcome(ctx, answer.GetOutcome().GetCause())
	return answer, nil
}

// canner is what the showing of a solution asks of the rights: authz.Rights.
type canner interface {
	Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error)
}

// ShowSolution shows the solution and closes the attempt "solution shown"
// (R09), under its idempotency key, by a learning action or by an action of an
// adult, whose right can_manage the handler checks: the checks before it hold
// can_learn alone. A task with a rubric has nothing to show this way; a child is
// shown it from the fourth wrong answer on, by either kind of action, and,
// while the child's setting "ask before showing" is on, by an adult's alone; a
// learner of the category adult from the beginning. The close deletes the
// words of the rungs (R05). The answer, the task solved, is what the key's
// record holds, with what comes next: in a lesson, the next segment (R48);
// outside a lesson, the engine's choice (R50).
func (a *AttemptService) ShowSolution(ctx context.Context, req *v1.ShowSolutionRequest) (*v1.ShowSolutionResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
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
			next, err := s.nextAfter(ctx, tx, profileID, h.row, category, now, false)
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
// of category by action (R09).
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
