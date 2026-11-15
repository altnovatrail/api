package learning

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"

	"github.com/altnovatrail/api/internal/packages/packagestest"
)

// The gate's adapters pass a reading and an assessment to tutor through the
// gate, which fails both without a task and a text before any call; the stub's
// ports read and assess nothing, "not admitted", with no usage.
func TestTheEssayPortsGoToTheGateOrNowhere(t *testing.T) {
	t.Parallel()

	g := gateTutor{noAI(t)}
	if got := g.ReadEssay(context.Background(), tutor.EssayReading{}, formatv0.Task{}, "sk-own"); got.Outcome.Kind != tutor.Failed || len(got.Outcome.Usage) != 0 {
		t.Errorf("the gate read an essay of no task as %+v, want a failure with no call", got.Outcome)
	}
	if got := g.AssessEssay(context.Background(), tutor.RubricAssessment{}, "sk-own"); got.Outcome.Kind != tutor.Failed || len(got.Outcome.Usage) != 0 {
		t.Errorf("the gate assessed no text as %+v, want a failure with no call", got.Outcome)
	}
	var stub unadmitted
	if got := stub.ReadEssay(context.Background(), tutor.EssayReading{}, formatv0.Task{}, ""); got.Outcome.Kind != tutor.NotAdmitted || got.Observation != nil {
		t.Errorf("the stub read an essay as %+v", got)
	}
	if got := stub.AssessEssay(context.Background(), tutor.RubricAssessment{Criteria: []string{"one"}}, ""); got.Outcome.Kind != tutor.NotAdmitted || got.Feedback != "" {
		t.Errorf("the stub assessed an essay as %+v", got)
	}
}

// A task with a rubric solved gives the texts of its criteria in order, with
// no keys, and its solution when it has one (R87 spec); a task with a verdict
// gives no criteria.
func TestATaskWithARubricSolvedGivesItsCriteria(t *testing.T) {
	t.Parallel()

	pkg := packagestest.OwnEdited(t, "choices", "1.0.1", func(path string, data []byte) []byte {
		if path == "tasks/story.yaml" {
			data = append(data, "solution:\n  - text: Each of three friends gets two of the six sweets.\n"...)
		}
		return data
	})
	got, err := solved(pkg, pkg.View.Tasks["story"])
	if err != nil || !slices.Equal(got.GetCriteria(), []string{"The story shares all the sweets.", "Every friend gets as many sweets as the others."}) ||
		len(got.GetKeys()) != 0 || !strings.Contains(string(got.GetSolution()), "two of the six sweets") {
		t.Errorf("story solved = %v, %v; want its two criteria and its solution, no keys", got, err)
	}
	if got, err := solved(pkg, pkg.View.Tasks["pick"]); err != nil || len(got.GetCriteria()) != 0 || len(got.GetKeys()) == 0 {
		t.Errorf("pick solved = %v, %v; want its keys and no criteria", got, err)
	}
}
