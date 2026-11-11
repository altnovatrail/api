package model

import (
	"context"
	"fmt"

	learnermodel "github.com/altnovatrail/learner-model"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/store"
)

// Told is what an attempt's rows tell of it once the package of its version
// reads them, as its export gives it (R52; R135 spec): what came of each item,
// the traps the program found in its wrong values, the IDs of the task's
// techniques, and its leading question.
type Told struct {
	Items      []learnermodel.ItemAnswer
	Traps      []string
	Techniques []string
	Question   string
	// Read says the package was in the image, so that the traps, the
	// techniques and the question were read: the items are read of the answers
	// alone.
	Read bool
}

// Tell is what the rows of the attempt o tell: its items of its answers alone,
// and the rest when the image holds its package — one that left the image tells
// no trap, technique or question (Д27).
func (m *Model) Tell(ctx context.Context, o store.Owed) (Told, error) {
	answers := make([]*v1.SubmitWorkResponse, len(o.Answers))
	for i, written := range o.Answers {
		answers[i] = &v1.SubmitWorkResponse{}
		if err := proto.Unmarshal(written, answers[i]); err != nil {
			return Told{}, fmt.Errorf("model: read a submission: %w", err)
		}
	}
	n := 0
	if len(answers) > 0 {
		n = len(answers[0].GetItems())
	}
	told := Told{Items: itemsOf(n, answers), Traps: []string{}, Techniques: []string{}}
	pkg, ok := m.set.Packages[formatv0.Digest(o.PackageDigest)]
	if !ok {
		return told, nil
	}
	task, ok := pkg.View.Tasks[formatv0.TaskID(o.TaskID)]
	if !ok {
		return told, nil
	}
	got, err := instanceOf(pkg, task, o.Instance)
	if err != nil {
		return Told{}, err
	}
	if len(answers) > 0 {
		traps, err := trapsOf(ctx, pkg, got, answers)
		if err != nil {
			return Told{}, err
		}
		for _, t := range traps {
			told.Traps = append(told.Traps, t.ID)
		}
	}
	for _, id := range got.Techniques {
		told.Techniques = append(told.Techniques, string(id))
	}
	if got.LeadingQuestion != nil {
		told.Question = string(*got.LeadingQuestion)
	}
	told.Read = true
	return told, nil
}
