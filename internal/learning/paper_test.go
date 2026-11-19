package learning_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// photoOf is a grey JPEG of width by height, the photo of a page as app sends
// it: the stub finds five tasks on one taller than wide, none on one wider than
// tall, and fails on a square one.
func photoOf(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (w *world) findTasks(l learner, profile string, photo []byte, key string) (*v1.FindTasksResponse, error) {
	return w.paper.FindTasks(l.ctx, &v1.FindTasksRequest{ProfileId: profile, IdempotencyKey: key, Photo: photo})
}

// beginPage begins n attempts of tasks from paper on the profile.
func (w *world) beginPage(t *testing.T, l learner, profile string, n int32) []string {
	t.Helper()
	res, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: profile, IdempotencyKey: uuid.NewString(), Tasks: n})
	if err != nil {
		t.Fatalf("BeginPaperAttempts %d: %v", n, err)
	}
	return res.GetAttemptIds()
}

// The finding on the stub (R66; R162 spec): the frames of the tasks in the order
// of the page, none as no outcome, a failure of AI as an outcome; each call
// spends the reserve of the finding, which tutor expects no price of, and a
// repeat under the key is "repeated" without frames, with no call. The record of
// the key keeps no frame, and the log no photo.
func TestTheTasksAreFoundOnThePhotoOfAPage(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	key := uuid.NewString()
	found, err := w.findTasks(l, l.adult, photoOf(t, 300, 400), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.GetFrames()) != 5 || found.GetOutcome() != nil || found.GetRepeated() {
		t.Fatalf("a portrait page = %v, want five frames", found)
	}
	for i, f := range found.GetFrames() {
		if i > 0 && f.GetBox().GetYmin() < found.GetFrames()[i-1].GetBox().GetYmax() {
			t.Errorf("frame %d = %v, want the order of the page", i+1, f)
		}
	}
	var kept []byte
	if err := w.pool.QueryRow(t.Context(), `SELECT response FROM idempotency_keys WHERE key = $1`, key).Scan(&kept); err != nil || len(kept) != 0 {
		t.Errorf("the record of the key holds %x (%v), want nothing of the frames", kept, err)
	}
	again, err := w.findTasks(l, l.adult, photoOf(t, 300, 400), key)
	if err != nil || !again.GetRepeated() || len(again.GetFrames()) != 0 || again.GetOutcome() != nil {
		t.Errorf("the repeat = %v (%v), want repeated, with no frames", again, err)
	}
	if none, err := w.findTasks(l, l.adult, photoOf(t, 400, 300), uuid.NewString()); err != nil || len(none.GetFrames()) != 0 || none.GetOutcome() != nil {
		t.Errorf("a landscape page = %v (%v), want no frames and no outcome", none, err)
	}
	failed, err := w.findTasks(l, l.adult, photoOf(t, 300, 300), uuid.NewString())
	if err != nil || len(failed.GetFrames()) != 0 || failed.GetOutcome().GetCause() != v1.Cause_CAUSE_AI_FAILURE {
		t.Errorf("a square page = %v (%v), want the failure of AI", failed, err)
	}
	reserve := quota.Reserve(tutor.CallFinding, tutor.CallPrice{}, 1)
	if profile, account := w.counted(t, l.accountID, l.adult); profile.Spent != 3*reserve || profile.Reserved != 0 || account != profile {
		t.Errorf("the counters = %+v, %+v; want three reserves of the finding spent", profile, account)
	}
	if log := w.log.String(); strings.Count(log, `"model":"stub"`) != 3 || !strings.Contains(log, `"outcome_cause":"CAUSE_AI_FAILURE"`) {
		t.Errorf("the log = %s, want three calls of the stub and the failure", log)
	}
	// The key of the finding is the finding's alone.
	_, err = w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: key, Tasks: 1})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
}

// What the finding refuses: a photo that is no JPEG or PNG, or larger than 4
// MiB; an own key of a child's profile; and, on platform AI, a quota spent or
// the platform's limit, before any call. A world that admits no AI refuses it
// "not admitted". The own key ends the call by its ending, as an outcome.
func TestTheFindingRefusesWhatTheContractRefuses(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	big := append(photoOf(t, 30, 40), make([]byte, 4<<20)...)
	for name, photo := range map[string][]byte{"none": nil, "no picture": []byte("a page"), "too large": big,
		"a PNG cut short": pngOf(t, 30, 40)[:30]} {
		if _, err := w.findTasks(l, l.adult, photo, uuid.NewString()); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if found, err := w.findTasks(l, l.adult, pngOf(t, 30, 40), uuid.NewString()); err != nil || len(found.GetFrames()) != 5 {
		t.Errorf("a PNG = %v (%v), want five frames", found, err)
	}
	own := &v1.OwnKey{Provider: "anthropic", Key: "sk-ant-synthetic" + tutorstub.KeyRefused}
	_, err := w.paper.FindTasks(l.ctx, &v1.FindTasksRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(), Photo: photoOf(t, 30, 40), OwnKey: own})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
	refused, err := w.paper.FindTasks(l.ctx, &v1.FindTasksRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(), Photo: photoOf(t, 30, 40), OwnKey: own})
	if err != nil || refused.GetOutcome().GetCause() != v1.Cause_CAUSE_KEY_REFUSED || len(refused.GetFrames()) != 0 {
		t.Errorf("a refused own key = %v (%v), want the key refused", refused, err)
	}
	if spent, _ := w.counted(t, l.accountID, l.adult); spent.Spent != quota.Reserve(tutor.CallFinding, tutor.CallPrice{}, 1) {
		t.Errorf("the profile spent %+v, want the one call on platform AI, and none of the own key", spent)
	}

	day := w.spend(t, l.accountID, 1_500_000)
	_, err = w.findTasks(l, l.adult, photoOf(t, 30, 40), uuid.NewString())
	if r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA); !r.GetNextFamilyDay().AsTime().Equal(day.End) {
		t.Errorf("the quota's refusal = %v, want the next day", r)
	}
	w.exec(t, `UPDATE account_quota SET spent_micro_usd = 0 WHERE account_id = $1`, l.accountID)
	w.exec(t, `UPDATE platform_ai SET closed_at = now()`)
	_, err = w.findTasks(l, l.adult, photoOf(t, 30, 40), uuid.NewString())
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PLATFORM_LIMIT)

	closed := newWorld(t)
	m := closed.learner(t)
	_, err = closed.findTasks(m, m.adult, photoOf(t, 30, 40), uuid.NewString())
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
}

// The attempts of a page begin one for each task, in order, beside the open
// attempt of a module, which stays; a new page closes the one before "left",
// and a task of a module closes no attempt of paper (R10). The list of profiles
// names the attempt of the module alone, and a repeat gets the same attempts.
func TestThePagesAttemptsGoBesideTheModulesAttempt(t *testing.T) {
	t.Parallel()

	// A child reads handwriting on the facts of dev-check.
	g := stubGateOf(t, childFacts)
	w := worldOf(t, g, []learning.Option{learning.WithTutor(tutorstub.New(g))})
	l := w.learner(t)
	moduleAttempt := w.begin(t, l, l.adult, "apples").GetAttemptId()
	key := uuid.NewString()
	first, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: key, Tasks: 3})
	if err != nil || len(first.GetAttemptIds()) != 3 {
		t.Fatalf("a page of three = %v (%v)", first, err)
	}
	if again, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: key, Tasks: 3}); err != nil ||
		!slices.Equal(again.GetAttemptIds(), first.GetAttemptIds()) {
		t.Errorf("the repeat = %v (%v), want the same attempts", again, err)
	}
	var order []string
	rows, err := w.pool.Query(t.Context(), `SELECT id FROM attempts WHERE profile_id = $1 AND kind = 'paper' ORDER BY id`, l.adult)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		order = append(order, id)
	}
	if len(order) != 3 || w.closeOf(t, moduleAttempt) != "" {
		t.Errorf("the page's attempts = %v, the module's closed %q; want three, and the module's open", order, w.closeOf(t, moduleAttempt))
	}
	account, err := w.accounts.GetAccount(l.ctx, &v1.GetAccountRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range account.GetAccount().GetProfiles() {
		if p.GetProfileId() == l.adult && p.GetOpenAttempt().GetAttemptId() != moduleAttempt {
			t.Errorf("the open attempt of the list = %v, want the module's", p.GetOpenAttempt())
		}
	}

	next := w.begin(t, l, l.adult, "apples").GetAttemptId()
	if w.closeOf(t, moduleAttempt) != "left" || w.closeOf(t, first.GetAttemptIds()[0]) != "" {
		t.Errorf("a new task of a module closed the module's %q and the page's %q; want left and open", w.closeOf(t, moduleAttempt), w.closeOf(t, first.GetAttemptIds()[0]))
	}
	second := w.beginPage(t, l, l.adult, 20)
	for _, a := range first.GetAttemptIds() {
		if w.closeOf(t, a) != "left" {
			t.Errorf("the attempt %s of the page before is %q, want left", a, w.closeOf(t, a))
		}
	}
	if len(second) != 20 || w.closeOf(t, second[19]) != "" || w.closeOf(t, next) != "" {
		t.Errorf("a page of twenty = %d attempts; the module's %q", len(second), w.closeOf(t, next))
	}
	if n := w.beginPage(t, l, l.child, 1); len(n) != 1 || w.closeOf(t, second[0]) != "" {
		t.Errorf("a page of another profile = %v, and closed %q of this one's", n, w.closeOf(t, second[0]))
	}
	for _, n := range []int32{0, 21, -1} {
		_, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(), Tasks: n})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("a page of %d: %v, want invalid_argument", n, err)
		}
	}
	_, err = w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.child, IdempotencyKey: uuid.NewString(), Tasks: 1, OwnKeyProvider: "anthropic"})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_NOT_FOR_PROFILE)
}

// A page begins as a task of handwriting does: with room for a typical task
// left of the family day on platform AI, and on an own key without it; a world
// that admits no reading of handwriting refuses it.
func TestAPageBeginsAsATaskOfHandwriting(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	day := w.spend(t, l.accountID, 1_500_000)
	_, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(), Tasks: 2})
	if r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_QUOTA); !r.GetNextFamilyDay().AsTime().Equal(day.End) {
		t.Errorf("the quota's refusal = %v, want the next day", r)
	}
	if res, err := w.paper.BeginPaperAttempts(l.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(), Tasks: 2,
		OwnKeyProvider: "anthropic"}); err != nil || len(res.GetAttemptIds()) != 2 {
		t.Errorf("a page on the own key = %v (%v), want two attempts", res, err)
	}

	closed := newWorld(t)
	m := closed.learner(t)
	_, err = closed.paper.BeginPaperAttempts(m.ctx, &v1.BeginPaperAttemptsRequest{ProfileId: m.adult, IdempotencyKey: uuid.NewString(), Tasks: 1})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
}

// An attempt of a task from paper has no solution to show, no steps, no rubric,
// no open view and no stored work (R162 spec); nor is it begun again as a task
// of a module.
func TestAnAttemptOfPaperTakesNoRequestOfAModule(t *testing.T) {
	t.Parallel()

	w, _ := newStubWorld(t)
	l := w.learner(t)
	a := w.beginPage(t, l, l.adult, 1)[0]
	_, err := w.attempts.ShowSolution(l.ctx, &v1.ShowSolutionRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Action: v1.Action_ACTION_LEARNING})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_SHOWING)
	for name, call := range map[string]func() error{
		"BeginStep": func() error {
			_, err := w.attempts.BeginStep(l.ctx, &v1.BeginStepRequest{ProfileId: l.adult, AttemptId: a, Step: 1})
			return err
		},
		"Done": func() error {
			_, err := w.attempts.Done(l.ctx, &v1.DoneRequest{ProfileId: l.adult, AttemptId: a})
			return err
		},
		"GetOpenAttempt": func() error {
			_, err := w.learning.GetOpenAttempt(l.ctx, &v1.GetOpenAttemptRequest{ProfileId: l.adult, AttemptId: a})
			return err
		},
		"SubmitEssay": func() error {
			_, err := w.attempts.SubmitEssay(l.ctx, &v1.SubmitEssayRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
				Crop: pngOf(t, 40, 10)})
			return err
		},
		"PutWork": func() error {
			_, err := w.put(l, l.adult, storedWork(a, "strokes"))
			return err
		},
		"BeginAttempt again": func() error {
			_, err := w.learning.BeginAttempt(l.ctx, &v1.BeginAttemptRequest{ProfileId: l.adult, IdempotencyKey: uuid.NewString(),
				Start: &v1.BeginAttemptRequest_PreviousAttemptId{PreviousAttemptId: a}})
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s on an attempt of paper: %v, want invalid_argument", name, err)
		}
	}
	if w.closeOf(t, a) != "" {
		t.Errorf("the attempt of paper closed %q, want it open", w.closeOf(t, a))
	}
}
