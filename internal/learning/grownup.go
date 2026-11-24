package learning

import (
	"context"
	"math"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The bounds of the strokes a request of grown-up mode sends, as the contract
// sets them: how many, and how many points each, in pairs of coordinates.
const (
	maxSheetStrokes = 3000
	maxStrokePoints = 128
	// maxPixelsPerMM bounds the scale of the crop a request gives.
	maxPixelsPerMM = 100
)

// sheetDrawer is the drawing of grown-up mode on a task from paper, as the gate of
// admission does it through tutor; own is as wordsTeller has it. Whether it is
// admitted the admitter says, by CallDrawing.
type sheetDrawer interface {
	GrownUp(ctx context.Context, g tutor.GrownUpRequest, own string) tutor.GrownUpAnswer
}

func (g gateTutor) GrownUp(ctx context.Context, r tutor.GrownUpRequest, _ string) tutor.GrownUpAnswer {
	return g.Gate.GrownUp(ctx, r)
}

// drawing is a request of grown-up mode between the parts of its request: the
// attempt, the call with its reserve and its route, the IDs of the strokes the
// request named, which alone an act may take off, the answer of the call, and
// an answer with no call.
type drawing struct {
	attemptID string
	request   tutor.GrownUpRequest
	route     tutor.Route
	named     []int32
	reserve   reserve
	answer    tutor.GrownUpAnswer
	ready     *v1.AskGrownUpResponse
}

// AskGrownUp answers a grown-up's request in grown-up mode on a task from
// paper: one call of the drawing, CallDrawing, which reads the work's crop,
// the task's crop, the strokes the work shows, the conversation and the
// request, and gives the tutor's words with its act on the sheet. It needs no
// Check, and none is asked. The right is an adult's, can_manage, which the
// table of methods checks. No admission, the quota, the limit and a call that
// gives no act answer the outcome alone. Nothing of the request or the answer
// is kept: the record of the key holds nothing, so a repeat gets repeated
// alone, and the key's fingerprint is argon2id's, as of a learner's question.
func (a *AttemptService) AskGrownUp(ctx context.Context, req *v1.AskGrownUpRequest) (*v1.AskGrownUpResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if ctx, err = askedLanguage(ctx, req.GetWordsLanguage()); err != nil {
		return nil, err
	}
	if err := grownUpTaken(req); err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_WORDS, req.GetAttemptId())
	defer act.End()
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceAskGrownUpProcedure,
		Slow:     true,
		GoingFor: s.grownUpGoingFor,
		Repeat: func(context.Context, pgx.Tx, store.KeyRecord) (proto.Message, error) {
			return &v1.AskGrownUpResponse{Repeated: true}, nil
		},
	}
	route := act.Route(routeOf(category, req.GetOwnKey()))
	admitted := s.admits.Reads(tutor.CallDrawing, category, req.GetOwnKey().GetProvider()) || s.debugChosen(ctx)
	prepare := func(tx pgx.Tx) (drawing, error) { return s.drawingAsked(ctx, tx, req, route, admitted) }
	var answer *v1.AskGrownUpResponse
	if admitted {
		answer, err = rpc.Across(ctx, s.db, key, req, rpc.Phases[drawing, *v1.AskGrownUpResponse]{
			Prepare: func(tx pgx.Tx, _ string) (drawing, store.KeyTarget, error) {
				d, err := prepare(tx)
				return d, store.KeyTarget{ProfileID: profileID, AttemptID: d.attemptID}, err
			},
			Call: func(ctx context.Context, d drawing) drawing { return s.draw(ctx, d, req.GetOwnKey().GetKey()) },
			Finish: func(tx pgx.Tx, d drawing) (*v1.AskGrownUpResponse, store.KeyTarget, error) {
				return s.drawingTold(ctx, tx, profileID, d)
			},
		})
	} else {
		answer, err = rpc.Once(ctx, s.db, key, req, func(tx pgx.Tx, _ string) (*v1.AskGrownUpResponse, store.KeyTarget, error) {
			d, err := prepare(tx)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return s.drawingTold(ctx, tx, profileID, d)
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

// grownUpTaken checks req by the contract's bounds before anything is read: the
// request and the conversation as a question to the tutor has them, the work
// of paper, and at most maxSheetStrokes strokes, each of pairs of finite
// coordinates, at most maxStrokePoints points, and a scale of 0 or above 0 to
// maxPixelsPerMM. They reach a model as data.
func grownUpTaken(req *v1.AskGrownUpRequest) error {
	if err := questionTaken(req.GetQuestion(), req.GetTurns()); err != nil {
		return err
	}
	if err := paperWorkTaken(req.GetWork().GetPaper()); err != nil {
		return err
	}
	strokes := req.GetStrokes()
	if len(strokes) > maxSheetStrokes {
		return rpc.Malformed("strokes")
	}
	for _, st := range strokes {
		points := st.GetPoints()
		if len(points)%2 != 0 || len(points) > 2*maxStrokePoints || slices.ContainsFunc(points, notFinite) {
			return rpc.Malformed("strokes")
		}
	}
	if ppm := req.GetPixelsPerMm(); notFinite(ppm) || ppm < 0 || ppm > maxPixelsPerMM {
		return rpc.Malformed("pixels_per_mm")
	}
	return nil
}

// notFinite says whether a coordinate is no number of the plane.
func notFinite(v float32) bool {
	f := float64(v)
	return math.IsNaN(f) || math.IsInf(f, 0)
}

// drawingAsked checks a request of grown-up mode before anything is called — an
// open attempt of a task from paper — and gives its call with its reserve, in
// the language of the words. A call not admitted, or a reserve the quota or
// the limit does not hold, gives the outcome and no call.
func (s *Service) drawingAsked(ctx context.Context, tx pgx.Tx, req *v1.AskGrownUpRequest, route tutor.Route, admitted bool) (drawing, error) {
	profileID := req.GetProfileId()
	h, err := s.holdAny(ctx, tx, profileID, req.GetAttemptId(), s.clock())
	d := drawing{attemptID: h.row.ID, route: route}
	switch {
	case err != nil:
		return d, err
	case !h.row.Paper:
		return d, rpc.Malformed("attempt_id")
	case !admitted:
		d.ready = &v1.AskGrownUpResponse{Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_NOT_ADMITTED}}
		return d, nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		return d, connect.NewError(connect.CodeDeadlineExceeded, "too little time to draw")
	}
	language, _, err := paperLanguage(ctx, tx, profileID)
	if err != nil {
		return d, err
	}
	work := req.GetWork().GetPaper()
	d.request = tutor.GrownUpRequest{Route: route, Question: req.GetQuestion(), Turns: turnsOf(req.GetTurns()),
		Statement: work.GetStatement(), Work: work.GetCrop(), Language: language, PixelsPerMM: req.GetPixelsPerMm()}
	for _, st := range req.GetStrokes() {
		d.request.Strokes = append(d.request.Strokes, tutor.SheetStroke{ID: st.GetId(), Points: st.GetPoints()})
		d.named = append(d.named, st.GetId())
	}
	day, err := s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
	if err != nil {
		return d, err
	}
	d.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallDrawing, 1, day)
	if d.reserve.refused != nil {
		d.ready = &v1.AskGrownUpResponse{Outcome: d.reserve.refused}
	}
	return d, err
}

// draw makes the call of d, between two transactions, and lets go of what it
// read: the request is the grown-up's words and the learner's work.
func (s *Service) draw(ctx context.Context, d drawing, own string) drawing {
	if d.ready != nil {
		return d
	}
	called, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	d.answer = s.drawer.GrownUp(called, d.request, own)
	noteCall(ctx, d.answer.Outcome, time.Since(start))
	s.afterCall(ctx, d.reserve, d.answer.Outcome)
	d.request = tutor.GrownUpRequest{}
	return d
}

// drawingTold answers d, on the attempt held again after its call, which is
// still to be open: the act, or the outcome. The record of the key holds
// nothing of either.
func (s *Service) drawingTold(ctx context.Context, tx pgx.Tx, profileID string, d drawing) (*v1.AskGrownUpResponse, store.KeyTarget, error) {
	answer := d.ready
	if answer == nil {
		if _, err := s.holdPaper(ctx, tx, profileID, d.attemptID, s.clock()); err != nil {
			return nil, store.KeyTarget{}, err
		}
		answer = d.drawn()
	}
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: d.attemptID, Response: []byte{}}, nil
}

// drawn is the answer of the call of d: the tutor's words and its act as the
// contract takes them — each stroke of at least two points of finite
// coordinates, a colour #rrggbb and a width, and each ID to take off once, one
// the request named —, or the outcome of a call that gave none. An act with
// nothing left of it is a failure of AI.
func (d drawing) drawn() *v1.AskGrownUpResponse {
	if d.answer.Outcome.Kind != tutor.Answered {
		return &v1.AskGrownUpResponse{Outcome: outcomeOf(d.answer.Outcome, d.route.AI)}
	}
	answer := &v1.AskGrownUpResponse{Words: d.answer.Words}
	for _, st := range d.answer.Strokes {
		if strokeTaken(st) {
			answer.Strokes = append(answer.Strokes, &v1.TutorStroke{Points: st.Points, Color: st.Color, Width: st.Width})
		}
	}
	for _, id := range d.answer.Erase {
		if slices.Contains(d.named, id) && !slices.Contains(answer.Erase, id) {
			answer.Erase = append(answer.Erase, id)
		}
	}
	if answer.Words == "" && len(answer.Strokes) == 0 && len(answer.Erase) == 0 {
		return &v1.AskGrownUpResponse{Outcome: &v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE}}
	}
	return answer
}

// strokeTaken says whether a stroke of the tutor's is one the contract takes.
func strokeTaken(st tutor.TutorStroke) bool {
	if len(st.Points) < 4 || len(st.Points)%2 != 0 || slices.ContainsFunc(st.Points, notFinite) ||
		notFinite(st.Width) || st.Width <= 0 || len(st.Color) != 7 || st.Color[0] != '#' {
		return false
	}
	for _, c := range st.Color[1:] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}
