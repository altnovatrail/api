package learning

import (
	"context"
	"testing"

	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
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
