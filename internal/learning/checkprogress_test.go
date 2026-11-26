package learning_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/tutor"
	"github.com/google/uuid"

	"github.com/altnovatrail/api/internal/learning"
	"github.com/altnovatrail/api/internal/tutorstub"
)

// goingOn is the stub of tutor whose Check goes on to the second of three
// models, as a cascade does when the first is not sure, and runs during before
// and after it.
type goingOn struct {
	*tutorstub.Stub
	during func(ctx context.Context, noted bool)
}

func (g goingOn) CheckPaper(ctx context.Context, c tutor.PaperCheck, own string) tutor.PaperChecked {
	g.during(ctx, false)
	tutor.NextModel(ctx, 1, 3)
	g.during(ctx, true)
	return g.Stub.CheckPaper(ctx, c, own)
}

// The progress of a Check of a task from paper is going while its SubmitWork
// is, with no model before the Check goes on and the second of three after it;
// it is not going before or after, nor for another attempt of the profile. A
// profile of another account is "no profile", and an attempt_id not a UUID is
// malformed.
func TestTheProgressOfACheckGoingOnToALaterModel(t *testing.T) {
	t.Parallel()

	g := stubGate(t)
	stub := &goingOn{Stub: tutorstub.New(g)}
	w := worldOf(t, g, []learning.Option{learning.WithTutor(stub)})
	l := w.learner(t)
	pages := w.beginPage(t, l, l.adult, 2)
	a, other := pages[0], pages[1]
	progress := func(ctx context.Context, profile, attempt string) (*v1.GetCheckProgressResponse, error) {
		return w.attempts.GetCheckProgress(ctx, &v1.GetCheckProgressRequest{ProfileId: profile, AttemptId: attempt})
	}
	want := func(t *testing.T, when string, got *v1.GetCheckProgressResponse, err error, going bool, model, models int32) {
		t.Helper()
		if err != nil || got.GetGoing() != going || got.GetModel() != model || got.GetModels() != models {
			t.Errorf("%s: %v %v, want going %v, model %d of %d", when, got, err, going, model, models)
		}
	}
	seen := 0
	stub.during = func(_ context.Context, noted bool) {
		seen++
		ctx := l.alone(t)
		got, err := progress(ctx, l.adult, a)
		if noted {
			want(t, "after the next model", got, err, true, 1, 3)
		} else {
			want(t, "before the next model", got, err, true, 0, 0)
		}
		got, err = progress(ctx, l.adult, other)
		want(t, "another attempt", got, err, false, 0, 0)
	}

	got, err := progress(l.ctx, l.adult, a)
	want(t, "before the Check", got, err, false, 0, 0)
	if _, err := w.attempts.SubmitWork(l.ctx, &v1.SubmitWorkRequest{ProfileId: l.adult, AttemptId: a, IdempotencyKey: uuid.NewString(),
		Work: paperWork(t, formulaStatement, pngOf(t, rightWork[0], rightWork[1]))}); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("the Check ran during %d times, want 2", seen)
	}
	got, err = progress(l.ctx, l.adult, a)
	want(t, "after the Check", got, err, false, 0, 0)

	stranger := w.learner(t)
	_, err = progress(stranger.ctx, l.adult, a)
	refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	_, err = progress(l.ctx, l.adult, "not-a-uuid")
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a malformed attempt_id: %v, want invalid_argument", err)
	}
}
