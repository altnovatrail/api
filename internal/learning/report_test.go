package learning_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"google.golang.org/protobuf/proto"
)

// An adult reports a problem with a reply of the tutor on an attempt, open or
// closed, which is kept with its words and text to be reviewed in 3 days; a
// repeat is the same report, and the deletion of the profile keeps it with no
// ID (R123 spec).
func TestAProblemIsReported(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	l := w.learner(t)
	a := w.begin(t, l, l.adult, "pick").GetAttemptId()
	report := func(key string, req *v1.ReportProblemRequest) (*v1.ReportProblemResponse, error) {
		req.ProfileId, req.IdempotencyKey = l.adult, key
		if req.AttemptId == "" {
			req.AttemptId = a
		}
		return w.attempts.ReportProblem(l.ctx, req)
	}
	ok := &v1.ReportProblemRequest{Reply: v1.TutorReply_TUTOR_REPLY_RUNG, Rung: 1, Words: "The words of the rung.",
		Kind: v1.ProblemKind_PROBLEM_KIND_UNCLEAR, Text: "Not clear to us."}
	before := time.Now()
	got, err := report("k-1", proto.Clone(ok).(*v1.ReportProblemRequest))
	if err != nil || got.GetReportId() == "" || got.GetReviewBy().AsTime().Sub(before) < 71*time.Hour {
		t.Fatalf("ReportProblem = %v, %v; want a report to be reviewed in 3 days", got, err)
	}
	if again, err := report("k-1", proto.Clone(ok).(*v1.ReportProblemRequest)); err != nil || again.GetReportId() != got.GetReportId() {
		t.Errorf("the repeat = %v, %v; want the same report", again, err)
	}
	other := proto.Clone(ok).(*v1.ReportProblemRequest)
	other.Kind = v1.ProblemKind_PROBLEM_KIND_WRONG
	_, err = report("k-1", other)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	if _, err := report("k-2", &v1.ReportProblemRequest{Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER, Words: "Well done.",
		Kind: v1.ProblemKind_PROBLEM_KIND_OTHER}); err != nil {
		t.Errorf("a report on the words after a correct answer = %v", err)
	}
	for _, bad := range []*v1.ReportProblemRequest{
		{Reply: v1.TutorReply_TUTOR_REPLY_UNSPECIFIED, Words: "w", Kind: v1.ProblemKind_PROBLEM_KIND_WRONG},
		{Reply: v1.TutorReply_TUTOR_REPLY_RUNG, Words: "w", Kind: v1.ProblemKind_PROBLEM_KIND_UNSPECIFIED, Rung: 1},
		{Reply: v1.TutorReply_TUTOR_REPLY_RUNG, Words: "w", Kind: v1.ProblemKind_PROBLEM_KIND_WRONG},
		{Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER, Words: "w", Kind: v1.ProblemKind_PROBLEM_KIND_WRONG, Rung: 2},
		{Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER, Kind: v1.ProblemKind_PROBLEM_KIND_WRONG},
		{Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER, Words: strings.Repeat("w", 4001), Kind: v1.ProblemKind_PROBLEM_KIND_WRONG},
		{Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER, Words: "w", Text: strings.Repeat("t", 1001), Kind: v1.ProblemKind_PROBLEM_KIND_WRONG},
	} {
		_, err := report("k-bad", bad)
		refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_UNSPECIFIED)
	}
	for _, attempt := range []string{"not an ID", "00000000-0000-0000-0000-000000000000"} {
		_, err := report("k-"+attempt, &v1.ReportProblemRequest{AttemptId: attempt, Reply: v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER,
			Words: "w", Kind: v1.ProblemKind_PROBLEM_KIND_WRONG})
		refusedAs(t, err, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	}
	w.exec(t, `DELETE FROM profiles WHERE id = $1`, l.adult)
	var kept int
	if err := w.pool.QueryRow(t.Context(), `SELECT count(*) FROM problem_reports
		WHERE profile_id IS NULL AND attempt_id IS NULL AND words = 'The words of the rung.'`).Scan(&kept); err != nil || kept != 1 {
		t.Errorf("reports kept with no ID = %d, %v; want the first", kept, err)
	}
}
