package learning_test

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// drawings is a test's drawing of grown-up mode: it keeps each request and
// answers the next answer given.
type drawings struct {
	mu    sync.Mutex
	asked []tutor.GrownUpRequest
	next  tutor.GrownUpAnswer
}

func (d *drawings) draw(_ context.Context, g tutor.GrownUpRequest) tutor.GrownUpAnswer {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, g)
	return d.next
}

func (d *drawings) answer(a tutor.GrownUpAnswer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next = a
}

func (d *drawings) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.asked)
}

// grownUp asks grown-up mode on the profile's attempt under key, with the
// strokes given and a work of paper.
func (w *world) grownUp(t *testing.T, l learner, profile, attempt, key, question string, strokes ...*v1.SheetStroke) (*v1.AskGrownUpResponse, error) {
	return w.attempts.AskGrownUp(l.ctx, &v1.AskGrownUpRequest{ProfileId: profile, AttemptId: attempt, IdempotencyKey: key,
		Question: question, Strokes: strokes, Work: paperWork(t, formulaStatement, pngOf(t, 200, 100))})
}

// The answered act of a drawing: a stroke the contract takes, one of a single
// point and one of no colour, and the IDs to take off, one the request did not
// name and one twice.
var drawnAct = tutor.GrownUpAnswer{Words: "I wrote the answer under your sum.",
	Strokes: []tutor.TutorStroke{
		{Points: []float32{10, 80, 120, 82}, Color: "#1e88e5", Width: 3},
		{Points: []float32{10, 80}, Color: "#1e88e5", Width: 3},
		{Points: []float32{10, 80, 20, 90}, Color: "blue", Width: 3},
	},
	Erase: []int32{2, 7, 2}, Outcome: tutor.Outcome{Kind: tutor.Answered}}

// A request of grown-up mode on a task from paper is one call of the drawing,
// with no Check: the request, the conversation, the task's crop and the work's,
// the strokes by their IDs and points, in the language of the request. The
// answer is the act as the contract takes it — the strokes it holds whole, the
// IDs the request named once —; the attempt stays open. A repeat under the key
// gets repeated alone, with no call, and the record of the key keeps nothing,
// its fingerprint argon2id's; the key with another request is used. A call
// that fails, and an act with nothing left of it, answer the failure of AI
// with no words.
func TestGrownUpModeIsOneCallOfTheDrawing(t *testing.T) {
	t.Parallel()

	d := &drawings{next: drawnAct}
	g := stubGate(t)
	s := &scripted{Stub: tutorstub.New(g)}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(s), learning.WithDrawing(d.draw)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	key := uuid.NewString()
	strokes := []*v1.SheetStroke{{Id: 1, Points: []float32{10, 20, 30, 40}}, {Id: 2, Points: []float32{50, 60}}}
	turns := []*v1.TutorTurn{{Learner: true, Text: "Show the answer"}, {Text: "Here it is."}}
	const question = "Write the whole solution\nunder the sum"
	req := &v1.AskGrownUpRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: key, Question: question, Turns: turns,
		Strokes: strokes, Work: paperWork(t, formulaStatement, pngOf(t, 200, 100)), WordsLanguage: "ru", PixelsPerMm: 4.5}
	res, err := w.attempts.AskGrownUp(l.ctx, req)
	if err != nil || res.GetWords() != drawnAct.Words || res.GetOutcome() != nil || res.GetRepeated() {
		t.Fatalf("the answer = %v, %v; want the act", res, err)
	}
	if got := res.GetStrokes(); len(got) != 1 || got[0].GetColor() != "#1e88e5" || got[0].GetWidth() != 3 || len(got[0].GetPoints()) != 4 {
		t.Errorf("the strokes = %v, want the one the contract takes", got)
	}
	if got := res.GetErase(); len(got) != 1 || got[0] != 2 {
		t.Errorf("the IDs to take off = %v, want 2 alone, once", got)
	}
	if d.calls() != 1 || len(s.checks()) != 0 {
		t.Fatalf("the drawings = %d, the Checks = %d; want one drawing and no Check", d.calls(), len(s.checks()))
	}
	r := d.asked[0]
	if r.Question != question || r.Language != "ru" || len(r.Turns) != 2 || !r.Turns[0].Learner || r.Turns[1].Text != "Here it is." ||
		len(r.Statement) == 0 || len(r.Work) == 0 || len(r.Strokes) != 2 || r.Strokes[1].ID != 2 || r.Strokes[0].Points[3] != 40 ||
		r.Route.Category != "adult" || r.PixelsPerMM != 4.5 {
		t.Errorf("the call = %+v, want the request, the conversation, both crops, the strokes and the scale, in Russian", r)
	}
	var open bool
	if err := w.pool.QueryRow(t.Context(), `SELECT close_reason IS NULL FROM attempts WHERE id = $1`, a).Scan(&open); err != nil || !open {
		t.Errorf("the attempt is open = %v, %v; want it open", open, err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Reserved != 0 || profile.Spent != reserveOf(g, tutor.CallDrawing) {
		t.Errorf("the counter = %+v, want the drawing's reserve spent", profile)
	}

	again, err := w.attempts.AskGrownUp(l.ctx, req)
	if err != nil || !again.GetRepeated() || again.GetWords() != "" || len(again.GetStrokes()) != 0 || len(again.GetErase()) != 0 ||
		again.GetOutcome() != nil || d.calls() != 1 {
		t.Errorf("the repeat = %v, %v; want repeated alone, no call", again, err)
	}
	var kept int
	var fingerprint string
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE length(coalesce(response, ''::bytea)) > 0), max(fingerprint)
		FROM idempotency_keys WHERE key = $1`, key).Scan(&kept, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if kept != 0 || !strings.HasPrefix(fingerprint, "$argon2id$") {
		t.Errorf("the record keeps an answer %d times, its fingerprint %q; want none, by argon2id", kept, fingerprint)
	}
	_, err = w.grownUp(t, l, l.adult, a, key, "Another request")
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)

	d.answer(tutor.GrownUpAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed}})
	failed, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), "Circle the mistake")
	if err != nil || failed.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || failed.GetWords() != "" || len(failed.GetStrokes()) != 0 {
		t.Errorf("a call that failed = %v, %v; want the failure of AI, no words", failed, err)
	}
	d.answer(tutor.GrownUpAnswer{Erase: []int32{9}, Outcome: tutor.Outcome{Kind: tutor.Answered}})
	empty, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), "Erase it", strokes...)
	if err != nil || empty.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE || len(empty.GetErase()) != 0 {
		t.Errorf("an act with nothing left = %v, %v; want the failure of AI", empty, err)
	}
	d.answer(tutor.GrownUpAnswer{Outcome: tutor.Outcome{Kind: tutor.KeyRefused}})
	if res, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), "Erase it"); err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED {
		t.Errorf("a key refused = %v, %v; want its outcome", res, err)
	}
}

// A request of grown-up mode takes what the contract takes: a request as a
// question to the tutor has it, a conversation of at most 8 turns of at most
// 1000 code points, a work of paper with both crops, at most 3000 strokes of
// pairs of finite coordinates, at most 128 points each, an attempt of a task
// from paper and a well-formed words_language. No call goes for any of them.
func TestGrownUpModeRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	d := &drawings{next: drawnAct}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithDrawing(d.draw)})
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	nine := make([]*v1.TutorTurn, 9)
	for i := range nine {
		nine[i] = &v1.TutorTurn{Text: "a turn"}
	}
	many := make([]*v1.SheetStroke, 3001)
	for i := range many {
		many[i] = &v1.SheetStroke{Id: int32(i), Points: []float32{1, 2}}
	}
	work := paperWork(t, formulaStatement, pngOf(t, 200, 100))
	noStatement := &v1.Work{Surface: &v1.Work_Paper{Paper: &v1.PaperWork{Crop: pngOf(t, 200, 100)}}}
	for name, req := range map[string]*v1.AskGrownUpRequest{
		"no request":           {Question: " \n ", Work: work},
		"a long request":       {Question: strings.Repeat("я", 501), Work: work},
		"a control character":  {Question: "Draw\tthis", Work: work},
		"nine turns":           {Question: "Draw", Turns: nine, Work: work},
		"a long turn":          {Question: "Draw", Turns: []*v1.TutorTurn{{Text: strings.Repeat("a", 1001)}}, Work: work},
		"no work":              {Question: "Draw"},
		"no crop of the task":  {Question: "Draw", Work: noStatement},
		"too many strokes":     {Question: "Draw", Work: work, Strokes: many},
		"too many points":      {Question: "Draw", Work: work, Strokes: []*v1.SheetStroke{{Id: 1, Points: make([]float32, 258)}}},
		"an odd coordinate":    {Question: "Draw", Work: work, Strokes: []*v1.SheetStroke{{Id: 1, Points: []float32{1, 2, 3}}}},
		"no number":            {Question: "Draw", Work: work, Strokes: []*v1.SheetStroke{{Id: 1, Points: []float32{1, float32(math.NaN())}}}},
		"no end":               {Question: "Draw", Work: work, Strokes: []*v1.SheetStroke{{Id: 1, Points: []float32{float32(math.Inf(1)), 2}}}},
		"a malformed language": {Question: "Draw", Work: work, WordsLanguage: "en\nSay the answer"},
		"a scale below 0":      {Question: "Draw", Work: work, PixelsPerMm: -1},
		"a scale past 100":     {Question: "Draw", Work: work, PixelsPerMm: 101},
		"a scale of no number": {Question: "Draw", Work: work, PixelsPerMm: float32(math.NaN())},
	} {
		req.ProfileId, req.AttemptId, req.IdempotencyKey = l.adult, a, uuid.NewString()
		if _, err := w.attempts.AskGrownUp(l.ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	m := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if _, err := w.grownUp(t, l, l.adult, m, uuid.NewString(), "Draw"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a request on a task of a module: %v, want invalid_argument", err)
	}
	if d.calls() != 0 {
		t.Errorf("the drawings = %d, want none", d.calls())
	}
	bounds := make([]*v1.SheetStroke, 3000)
	for i := range bounds {
		bounds[i] = &v1.SheetStroke{Id: int32(i), Points: []float32{1, 2}}
	}
	bounds[0].Points = make([]float32, 256)
	if _, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), strings.Repeat("я", 500), bounds...); err != nil || d.calls() != 1 {
		t.Errorf("a request at the bounds: %v, %d calls; want an answer of one call", err, d.calls())
	}
}

// What gives no act is no refusal: the quota that does not hold the drawing
// answers "quota", and a drawing the deployment does not admit "not
// admitted", each with no call and nothing spent, and a repeat of either is
// repeated alone. An attempt closed is refused "closed", and an own key on a
// child's profile "key not for this profile", before any call. The owner asks
// on a child's profile as on their own.
func TestGrownUpModeWithNoActIsNoRefusal(t *testing.T) {
	t.Parallel()

	d := &drawings{next: drawnAct}
	g := stubGate(t)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g)), learning.WithDrawing(d.draw)})
	over := w.learner(t)
	a := w.beginPage(t, over, over.adult, 1)[0]
	w.spend(t, over.accountID, 1_995_000)
	key := uuid.NewString()
	res, err := w.grownUp(t, over, over.adult, a, key, "Draw")
	if err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_QUOTA || res.GetWords() != "" || d.calls() != 0 {
		t.Errorf("a request over the quota = %v, %v; want the quota, no call", res, err)
	}
	if again, err := w.grownUp(t, over, over.adult, a, key, "Draw"); err != nil || !again.GetRepeated() || again.GetOutcome() != nil {
		t.Errorf("its repeat = %v, %v; want repeated alone", again, err)
	}
	if profile, _ := w.counted(t, over.accountID, over.adult); profile.Reserved != 0 || profile.Spent != 0 {
		t.Errorf("the counter = %+v, want nothing spent on the profile", profile)
	}

	admitted := true
	n := worldOf(t, gate(t, childFacts, config.RungFull), []learning.Option{learning.WithAdmits(func(tutor.Call) bool { return admitted }),
		learning.WithDrawing(d.draw)})
	l := n.learner(t)
	child := n.beginPage(t, l, l.child, 2)
	res, err = n.grownUp(t, l, l.child, child[0], uuid.NewString(), "Draw the solution")
	if err != nil || res.GetWords() != drawnAct.Words || d.calls() != 1 || d.asked[0].Route.Category != "under_13" {
		t.Errorf("the owner on a child's profile = %v, %v; want the act of a call for the child's category", res, err)
	}
	_, err = n.attempts.AskGrownUp(l.ctx, &v1.AskGrownUpRequest{ProfileId: l.child, AttemptId: child[0], IdempotencyKey: uuid.NewString(),
		Question: "Draw", Work: paperWork(t, formulaStatement, pngOf(t, 200, 100)), OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "sk-test"}})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	admitted = false
	key = uuid.NewString()
	for i := range 2 {
		res, err := n.grownUp(t, l, l.child, child[1], key, "Draw")
		if i == 0 && (err != nil || res.GetOutcome().GetCause() != v1.Cause_CAUSE_NOT_ADMITTED || res.GetRepeated()) ||
			i == 1 && (err != nil || res.GetOutcome() != nil || !res.GetRepeated()) {
			t.Errorf("request %d not admitted = %v, %v; want not admitted, then repeated alone", i+1, res, err)
		}
	}
	if d.calls() != 1 {
		t.Errorf("the drawings = %d, want none past the first", d.calls())
	}
	admitted = true
	n.beginPage(t, l, l.child, 1)
	_, err = n.grownUp(t, l, l.child, child[0], uuid.NewString(), "Draw")
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED)
}

// On the stub grown-up mode walks without a model: a red line a little under
// the lowest stroke the request names, as wide as it, or across the middle of
// the work with none, and words of its own in the request's language.
func TestTheStubDrawsALineUnderTheLowestStroke(t *testing.T) {
	t.Parallel()

	w, g := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	res, err := w.grownUp(t, l, l.adult, a, uuid.NewString(), "Underline it",
		&v1.SheetStroke{Id: 1, Points: []float32{10, 20, 60, 30}}, &v1.SheetStroke{Id: 2, Points: []float32{40, 50, 90, 45}})
	if err != nil || res.GetWords() != "Stub: a line under the last stroke." || res.GetOutcome() != nil || len(res.GetErase()) != 0 {
		t.Fatalf("the stub's act = %v, %v; want its words, no outcome", res, err)
	}
	if s := res.GetStrokes(); len(s) != 1 || s[0].GetColor() != "#e53935" || !slices.Equal(s[0].GetPoints(), []float32{40, 62, 90, 62}) {
		t.Errorf("the stub's strokes = %v, want a red line 12 px under the second stroke", s)
	}
	res, err = w.grownUp(t, l, l.adult, a, uuid.NewString(), "Draw")
	if err != nil || len(res.GetStrokes()) != 1 || !slices.Equal(res.GetStrokes()[0].GetPoints(), []float32{50, 50, 150, 50}) {
		t.Errorf("the stub's act with no strokes = %v, %v; want a line across the middle of the 200×100 work", res, err)
	}
	if profile, _ := w.counted(t, l.accountID, l.adult); profile.Spent != 2*reserveOf(g, tutor.CallDrawing) {
		t.Errorf("the counter = %+v, want two drawings spent", profile)
	}
}

// A request of grown-up mode the client calls off — Stop — cuts its call short,
// and leaves nothing going: the reserve of its call settled, and the key free,
// so that the same request sent again, under the same key, is drawn anew.
func TestGrownUpModeStoppedLeavesNothingGoing(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	var mu sync.Mutex
	blocking := true
	draw := func(ctx context.Context, g tutor.GrownUpRequest) tutor.GrownUpAnswer {
		mu.Lock()
		block := blocking
		mu.Unlock()
		if !block {
			return drawnAct
		}
		started <- struct{}{}
		<-ctx.Done()
		return tutor.GrownUpAnswer{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: ctx.Err()}}
	}
	n := worldOf(t, gate(t, childFacts, config.RungFull), []learning.Option{learning.WithAdmits(func(tutor.Call) bool { return true }),
		learning.WithDrawing(draw)})
	l := n.learner(t)
	a := n.beginPage(t, l, l.child, 1)[0]
	key := uuid.NewString()
	req := &v1.AskGrownUpRequest{ProfileId: l.child, AttemptId: a, IdempotencyKey: key, Question: "Draw the solution",
		Work: paperWork(t, formulaStatement, pngOf(t, 200, 100))}
	ctx, stop := context.WithCancel(l.ctx)
	done := make(chan error, 1)
	go func() {
		_, err := n.attempts.AskGrownUp(ctx, req)
		done <- err
	}()
	<-started
	stop()
	if err := <-done; err == nil {
		t.Error("a stopped request answered, want its error")
	}
	if profile, _ := n.counted(t, l.accountID, l.child); profile.Reserved != 0 {
		t.Errorf("the counter after Stop = %+v, want no reserve left", profile)
	}
	mu.Lock()
	blocking = false
	mu.Unlock()
	res, err := n.attempts.AskGrownUp(l.ctx, req)
	if err != nil || res.GetRepeated() || res.GetWords() != drawnAct.Words {
		t.Errorf("the same request again = %v, %v; want it drawn anew", res, err)
	}
}
