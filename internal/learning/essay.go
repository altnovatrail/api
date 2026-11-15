package learning

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/module-kit/observation"
	"github.com/altnovatrail/module-kit/openview"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/words"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// essayReader reads the essay of a task with a rubric, as tutor does; task and
// own are as sheetReader has them (R82).
type essayReader interface {
	ReadEssay(ctx context.Context, r tutor.EssayReading, task formatv0.Task, own string) tutor.EssayRead
}

// rubricAssessor assesses the text of an essay by the criteria of its rubric,
// as tutor does; own is as sheetReader has it (R82).
type rubricAssessor interface {
	AssessEssay(ctx context.Context, r tutor.RubricAssessment, own string) tutor.RubricAssessed
}

func (g gateTutor) ReadEssay(ctx context.Context, r tutor.EssayReading, _ formatv0.Task, _ string) tutor.EssayRead {
	return g.Gate.ReadEssay(ctx, r)
}

func (g gateTutor) AssessEssay(ctx context.Context, r tutor.RubricAssessment, _ string) tutor.RubricAssessed {
	return g.Gate.AssessEssay(ctx, r)
}

// unadmitted reads and assesses no essay: "not admitted", with no call, for the
// dev mode's stub until it reads essays of its own, so that it sends nothing to
// a provider (R66).
type unadmitted struct{}

func (unadmitted) ReadEssay(context.Context, tutor.EssayReading, formatv0.Task, string) tutor.EssayRead {
	return tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}
}

func (unadmitted) AssessEssay(context.Context, tutor.RubricAssessment, string) tutor.RubricAssessed {
	return tutor.RubricAssessed{Outcome: tutor.Outcome{Kind: tutor.NotAdmitted}}
}

// essaying is a submission of an essay between the parts of its request: the
// attempt and the instance and package it goes on, the crop of the sheet, the
// route and the family day, the reserves of the reading and the assessment, and
// what came of the two calls.
type essaying struct {
	attemptID string
	task      formatv0.Task
	pkg       *modulepackage.Package
	crop      []byte
	route     tutor.Route
	day       quota.Day
	// reading and assessing are the two parts of the reserve of the essay
	// (reserveEssay); a refusal is the reading's.
	reading, assessing reserve
	read               tutor.EssayRead
	// assessment is what the assessment gave, once the essay was read.
	assessment tutor.RubricAssessed
}

// SubmitEssay sends the sheet of a task with a rubric for its feedback, under
// its idempotency key: one request, two calls of tutor (R14, R82). The crop of
// the sheet is read by one reader, and the text read is assessed by the
// criteria of the rubric, with the statement as its topic, the style of the
// learner's category and the module's language of explanations; the feedback
// goes once it passes the leak check and the rule of feedback, words.Feedback,
// and without it the answer has none, and app shows "no feedback this time".
// The submission ends "feedback", "unreadable" — the reading said the sheet as
// a whole cannot be read, and the assessment is not called — or "unchecked",
// with the outcome of a call that gave nothing, or none when the assessment
// assessed no criterion. The request goes in three parts (rpc.Across): the
// checks of the attempt and the crop, and the reserve of the reading together
// with the assessment (R12), beside the key's record going; the two calls; and
// the attempt held again, with the submission recorded — how it ended, and
// beside feedback the marks and the source of the words, never the text, the
// evidence or the words (R15). An attempt closed meanwhile by another device is
// no refusal: the answer comes from the request's memory, names the close, and
// nothing is recorded. A repeat under the key gets the answer without the text
// and the words, repeated.
func (a *AttemptService) SubmitEssay(ctx context.Context, req *v1.SubmitEssayRequest) (*v1.SubmitEssayResponse, error) {
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
		Method:   apiv1connect.AttemptServiceSubmitEssayProcedure,
		GoingFor: s.goingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			written := record.Response
			if record.SubmissionID != "" {
				var err error
				if written, err = store.SubmissionAnswer(ctx, tx, profileID, record.SubmissionID); err != nil {
					return nil, err
				}
			}
			answer := &v1.SubmitEssayResponse{}
			if err := proto.Unmarshal(written, answer); err != nil {
				return nil, err
			}
			answer.Text, answer.Feedback, answer.Repeated = nil, nil, true
			return answer, nil
		},
	}
	answer, err := rpc.Across(ctx, s.db, key, req, rpc.Phases[essaying, *v1.SubmitEssayResponse]{
		Prepare: func(tx pgx.Tx, _ string) (essaying, store.KeyTarget, error) {
			e, err := s.essayed(ctx, tx, req, category)
			return e, store.KeyTarget{ProfileID: profileID, AttemptID: e.attemptID}, err
		},
		Call: func(ctx context.Context, e essaying) essaying { return s.assess(ctx, e, req.GetOwnKey().GetKey()) },
		Finish: func(tx pgx.Tx, e essaying) (*v1.SubmitEssayResponse, store.KeyTarget, error) {
			return s.essayRecorded(ctx, tx, profileID, e)
		},
	})
	if err != nil {
		return nil, refused(ctx, err)
	}
	if c := answer.GetOutcome().GetCause(); c != v1.Cause_CAUSE_UNSPECIFIED {
		rpc.NoteOutcome(ctx, c)
	}
	return answer, nil
}

// essayed checks a submission of an essay on the attempt it names, held, before
// anything is called, and takes the reserve of the reading with the assessment:
// an open attempt of the profile, of a task with a rubric that tutor reads as an
// essay, and a crop that is a PNG tutor would read (cropTaken). An attempt
// closed is "attempt closed", before any call of AI (R14).
func (s *Service) essayed(ctx context.Context, tx pgx.Tx, req *v1.SubmitEssayRequest, category formatv0.AgeCategory) (essaying, error) {
	profileID := req.GetProfileId()
	h, err := s.hold(ctx, tx, profileID, req.GetAttemptId(), s.clock())
	if err != nil {
		return essaying{}, err
	}
	e := essaying{attemptID: h.row.ID, task: h.task, pkg: h.pkg, crop: req.GetCrop()}
	switch {
	case !rubric(h.task) || !essayRead(h.task):
		// A task with a verdict goes by SubmitWork, and one with a rubric tutor does
		// not read as an essay does not begin.
		return e, rpc.Malformed("attempt_id")
	case !cropTaken(e.crop):
		return e, rpc.Malformed("crop")
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
		// Too little time is left to read, assess and write: a call cut short would
		// be recorded as a failure of AI under the key for good.
		return e, connect.NewError(connect.CodeDeadlineExceeded, "too little time to read the essay")
	}
	e.route = routeOf(category, req.GetOwnKey())
	if e.day, err = s.familyDay(ctx, tx, e.route, req.ClockOffsetMinutes); err != nil {
		return e, err
	}
	e.reading, e.assessing, err = s.reserveEssay(ctx, tx, profileID, e.route, e.day)
	return e, err
}

// assess reads the essay of e and, once it is read, assesses its text, until
// finishReserve before the request's deadline, each call noted in the request's
// log and settling its part of the reserve by what it cost; own is the value of
// the request's own key, for the stub. A reserve the quota did not hold calls
// nothing, and an essay that was not read gives the assessment's part back. The
// crop goes no further than the reading.
func (s *Service) assess(ctx context.Context, e essaying, own string) essaying {
	crop := e.crop
	e.crop = nil
	if e.reading.refused != nil {
		return e
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	view, err := openview.Task(e.task)
	start := time.Now()
	if err != nil {
		e.read = tutor.EssayRead{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: err}}
	} else {
		e.read = s.essay.ReadEssay(ctx, tutor.EssayReading{Route: e.route, Task: &view, Crop: crop}, e.task, own)
	}
	noteCall(ctx, e.read.Outcome, time.Since(start))
	s.afterCall(ctx, e.reading, e.read.Outcome)
	text, ok := e.text()
	if !ok {
		s.release(ctx, e.assessing)
		return e
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < finishReserve {
		// The reading left too little time to assess: a call cut short would spend
		// its reserve with no record of usage, so none goes, and the essay is
		// "unchecked" as a failure of AI.
		s.release(ctx, e.assessing)
		e.assessment = tutor.RubricAssessed{Outcome: tutor.Outcome{Kind: tutor.Failed, Err: context.DeadlineExceeded}}
		return e
	}
	language := string(e.pkg.View.Manifest.Languages.Explanation)
	start = time.Now()
	e.assessment = s.assessor.AssessEssay(ctx, tutor.RubricAssessment{Route: e.route, Text: text,
		Topic: statementOf(e.task), Criteria: criteriaOf(e.task), Style: styles[formatv0.AgeCategory(e.route.Category)],
		Language: language}, own)
	noteCall(ctx, e.assessment.Outcome, time.Since(start))
	s.afterCall(ctx, e.assessing, e.assessment.Outcome)
	return e
}

// end is how the reading of e came out, by module-kit (observation.Essay): the
// text to assess, or the end of a submission that reaches no assessment, with
// its answer — "unreadable" with its reason, or "unchecked" with the outcome of
// the reading that gave nothing, of the quota, or of an observation that is not
// valid — a value empty or only white space among it —, as a failure of AI.
func (e essaying) end() (string, *v1.SubmitEssayResponse) {
	unchecked := func(o *v1.Outcome) (string, *v1.SubmitEssayResponse) {
		return "", &v1.SubmitEssayResponse{End: v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, Outcome: o}
	}
	switch {
	case e.reading.refused != nil:
		return unchecked(e.reading.refused)
	case e.read.Outcome.Kind != tutor.Answered:
		return unchecked(outcomeOf(e.read.Outcome, e.route.AI))
	}
	ended, err := observation.Essay(e.task.Items, e.read.Observation)
	switch {
	case err != nil || ended == observation.SubmissionUnchecked:
		return unchecked(&v1.Outcome{Cause: v1.Cause_CAUSE_AI_FAILURE})
	case ended == observation.SubmissionUnreadable:
		reason := verdict.Illegible
		if e.read.Observation.GetAnswerSlots()[0].GetUnreadable() == observationv1.Unreadable_UNREADABLE_BLANK {
			reason = verdict.Blank
		}
		return "", &v1.SubmitEssayResponse{End: v1.SubmissionEnd_SUBMISSION_END_UNREADABLE, Reason: string(reason)}
	}
	return e.read.Observation.GetAnswerSlots()[0].GetValue(), nil
}

// text is the text of the essay of e as it was read, and whether it goes on to
// the assessment.
func (e essaying) text() (string, bool) {
	text, answer := e.end()
	return text, answer == nil
}

// marks are the marks of a criterion in the schema's words, by whether the
// essay meets it; one with no assessment is "not assessed".
var marks = map[bool]string{true: store.MarkMet, false: store.MarkNotMet}

// essayAnswer is the answer of the submission of e and how it ended, as the
// server keeps it (R14, R15): with the text read when there was reading;
// feedback once the assessment assessed a criterion at least, with the mark of
// each criterion by its number and the words, once they pass the leak check
// and the rule of feedback against the text read, or else none and the source
// "no feedback", the check named in the request's line; "unchecked" with the
// outcome of an assessment that gave nothing, or with none when it assessed no
// criterion.
func essayAnswer(ctx context.Context, e essaying) (*v1.SubmitEssayResponse, store.Essay) {
	text, answer := e.end()
	if answer != nil {
		return answer, store.Essay{End: essayEnds[answer.GetEnd()]}
	}
	answer = &v1.SubmitEssayResponse{End: v1.SubmissionEnd_SUBMISSION_END_UNCHECKED, Text: &text}
	got := e.assessment
	if got.Outcome.Kind != tutor.Answered {
		answer.Outcome = outcomeOf(got.Outcome, e.route.AI)
		return answer, store.Essay{End: store.EssayUnchecked}
	}
	kept := store.Essay{End: store.EssayFeedback, Marks: make([]string, len(criteriaOf(e.task)))}
	assessed := false
	for i := range kept.Marks {
		kept.Marks[i] = store.MarkNotAssessed
		if i < len(got.Criteria) && got.Criteria[i].Met != nil {
			kept.Marks[i], assessed = marks[*got.Criteria[i].Met], true
		}
	}
	if !assessed {
		// No criterion with an assessment: no check, and no outcome to name (R14).
		return answer, store.Essay{End: store.EssayUnchecked}
	}
	answer.End = v1.SubmissionEnd_SUBMISSION_END_FEEDBACK
	source := store.SourceModel
	feedback := func(t, language, _ string) []words.Breach { return words.Feedback(t, text, language) }
	if check := turnedDown(e.pkg, e.task, got.Feedback, feedback); check != "" {
		rpc.NoteWordsCheck(ctx, check)
		source = store.SourceNoFeedback
	} else {
		answer.Feedback = &got.Feedback
	}
	kept.Source = &source
	return answer, kept
}

// submissionEnds is each end of a submission of an essay of the schema in the
// contract's words, and essayEnds the same the other way.
var (
	submissionEnds = map[string]v1.SubmissionEnd{
		store.EssayFeedback:   v1.SubmissionEnd_SUBMISSION_END_FEEDBACK,
		store.EssayUnreadable: v1.SubmissionEnd_SUBMISSION_END_UNREADABLE,
		store.EssayUnchecked:  v1.SubmissionEnd_SUBMISSION_END_UNCHECKED,
	}
	essayEnds = map[v1.SubmissionEnd]string{
		v1.SubmissionEnd_SUBMISSION_END_FEEDBACK:   store.EssayFeedback,
		v1.SubmissionEnd_SUBMISSION_END_UNREADABLE: store.EssayUnreadable,
		v1.SubmissionEnd_SUBMISSION_END_UNCHECKED:  store.EssayUnchecked,
	}
)

// criterionMarks is each mark of a criterion of the schema in the contract's
// words.
var criterionMarks = map[string]v1.CriterionMark{
	store.MarkMet:         v1.CriterionMark_CRITERION_MARK_MET,
	store.MarkNotMet:      v1.CriterionMark_CRITERION_MARK_NOT_MET,
	store.MarkNotAssessed: v1.CriterionMark_CRITERION_MARK_NOT_ASSESSED,
}

// essayRecorded answers what came of the submission of e, on its attempt held
// again after the calls, and records it: how it ended, and beside feedback the
// marks and the source of the words, with its answer without the text read and
// the words, which the key's record names. An attempt closed meanwhile records
// nothing: the answer from the request's memory names its close, and the key's
// record holds the answer without the text and the words (R14).
func (s *Service) essayRecorded(ctx context.Context, tx pgx.Tx, profileID string, e essaying) (*v1.SubmitEssayResponse, store.KeyTarget, error) {
	answer, kept := essayAnswer(ctx, e)
	// An answer of the contract always marshals.
	written, _ := proto.Marshal(&v1.SubmitEssayResponse{End: answer.GetEnd(), Reason: answer.GetReason(), Outcome: answer.GetOutcome()})
	target := store.KeyTarget{ProfileID: profileID, AttemptID: e.attemptID}
	// The run first, as hold takes them: a request on the run holds it so.
	if err := store.HoldAttemptRun(ctx, tx, profileID, e.attemptID); err != nil {
		return nil, store.KeyTarget{}, err
	}
	row, err := store.HoldAttempt(ctx, tx, profileID, e.attemptID)
	if err != nil {
		return nil, store.KeyTarget{}, err
	}
	if row.CloseReason != nil {
		answer.Closed = closeReasons[*row.CloseReason]
		written, _ = proto.Marshal(&v1.SubmitEssayResponse{End: answer.GetEnd(), Reason: answer.GetReason(),
			Outcome: answer.GetOutcome(), Closed: answer.GetClosed()})
		target.Response = written
		return answer, target, nil
	}
	if target.SubmissionID, err = store.SubmitEssay(ctx, tx, profileID, e.attemptID, written, kept, s.clock()); err != nil {
		return nil, store.KeyTarget{}, err
	}
	return answer, target, nil
}
