package learning

import (
	"context"
	"unicode/utf8"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The bounds of a report (contract, ReportProblem): the words of the reply as
// app showed them, and what the adult wrote, in code points.
const (
	maxReportWords = 4000
	maxReportText  = 1000
)

// replies and kinds are the reply reported on and the kind of the problem, as
// the schema keeps them.
var (
	replies = map[v1.TutorReply]string{
		v1.TutorReply_TUTOR_REPLY_RUNG:                  "rung",
		v1.TutorReply_TUTOR_REPLY_EXPLAINED_DIFFERENTLY: "explained_differently",
		v1.TutorReply_TUTOR_REPLY_CORRECT_ANSWER:        "correct_answer",
		v1.TutorReply_TUTOR_REPLY_ESSAY_FEEDBACK:        "essay_feedback",
	}
	kinds = map[v1.ProblemKind]string{
		v1.ProblemKind_PROBLEM_KIND_WRONG:       "wrong",
		v1.ProblemKind_PROBLEM_KIND_UNCLEAR:     "unclear",
		v1.ProblemKind_PROBLEM_KIND_NOT_FOR_AGE: "not_for_age",
		v1.ProblemKind_PROBLEM_KIND_HARMFUL:     "harmful",
		v1.ProblemKind_PROBLEM_KIND_PERSONAL:    "personal",
		v1.ProblemKind_PROBLEM_KIND_OTHER:       "other",
	}
)

// ReportProblem records an adult's report of a problem with a reply of the
// tutor, on an attempt of the profile, open or closed, which the operator reads
// by a command of api and reviews in 3 days (R123 spec). It keeps the words and
// the text as they were written, nothing of the work beyond them, and outlives
// the deletion of the profile and the account to its term, without their IDs.
// Neither a consent that waits nor an age to give again refuses it. A repeat
// under its key is the same report.
func (a *AttemptService) ReportProblem(ctx context.Context, req *v1.ReportProblemRequest) (*v1.ReportProblemResponse, error) {
	reply, ok := replies[req.GetReply()]
	if !ok {
		return nil, rpc.Malformed("reply")
	}
	kind, ok := kinds[req.GetKind()]
	if !ok {
		return nil, rpc.Malformed("kind")
	}
	r := store.Report{AttemptID: req.GetAttemptId(), Reply: reply, Words: req.GetWords(), Kind: kind, Text: req.GetText()}
	switch withRung := reply == "rung" || reply == "explained_differently"; {
	case withRung && (req.GetRung() < 1 || req.GetRung() > 3):
		return nil, rpc.Malformed("rung")
	case withRung:
		rung := int16(req.GetRung())
		r.Rung = &rung
	case req.GetRung() != 0:
		return nil, rpc.Malformed("rung")
	}
	if n := utf8.RuneCountInString(req.GetWords()); n < 1 || n > maxReportWords || !utf8.ValidString(req.GetWords()) {
		return nil, rpc.Malformed("words")
	}
	if utf8.RuneCountInString(req.GetText()) > maxReportText || !utf8.ValidString(req.GetText()) {
		return nil, rpc.Malformed("text")
	}
	if !store.CanonicalID(req.GetAttemptId()) {
		return nil, refused(ctx, store.ErrNotFound)
	}
	s := a.s
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.AttemptServiceReportProblemProcedure}, req,
		func(tx pgx.Tx, accountID string) (*v1.ReportProblemResponse, store.KeyTarget, error) {
			there, err := store.AttemptOfProfile(ctx, tx, profileID, req.GetAttemptId())
			switch {
			case err != nil:
				return nil, store.KeyTarget{}, err
			case !there:
				return nil, store.KeyTarget{}, store.ErrNotFound
			}
			r.CreatedAt = s.clock()
			id, err := store.WriteReport(ctx, tx, accountID, profileID, r)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return &v1.ReportProblemResponse{ReportId: id, ReviewBy: timestamppb.New(r.CreatedAt.Add(store.ReviewWithin))},
				store.KeyTarget{ProfileID: profileID}, nil
		})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}
