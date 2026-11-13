package learning

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

var _ apiv1connect.ProgressServiceHandler = (*ProgressService)(nil)

// progressPage is how many attempts a page of the progress holds.
const progressPage = 50

// ProgressService is what the learner model and the history of a profile show
// (R49): the skill map and the adult's progress. Both write first the evidence
// the profile's closed attempts owe, so that they read the journal whole.
type ProgressService struct {
	apiv1connect.UnimplementedProgressServiceHandler
	s      *Service
	rights canner
}

// Progress is the ProgressService of s, on db, which asks rights of an adult's
// view of the skill map.
func (s *Service) Progress(db rpc.DB, rights canner) *ProgressService {
	return &ProgressService{s: s.on(db), rights: rights}
}

// GetSkillMap is the state of the skills of a connected module's current
// version, as learner-model reads it at the time of the request: by a learning
// action, whose profile's consent is confirmed, or by an adult's view, with the
// right can_view, which the handler checks — the checks before it hold
// can_learn alone. A module not connected is "not found", and one with no
// current version for the learner says so. Each anchor skill has the modules
// of the index that teach it, suit the learner and are not connected (R63 spec).
func (p *ProgressService) GetSkillMap(ctx context.Context, req *v1.GetSkillMapRequest) (*v1.GetSkillMapResponse, error) {
	s := p.s
	profileID := req.GetProfileId()
	var category formatv0.AgeCategory
	switch req.GetAction() {
	case v1.Action_ACTION_LEARNING:
		c, err := s.learner(ctx, profileID)
		if err != nil {
			return nil, err
		}
		category = c
	case v1.Action_ACTION_ADULT:
		caller, _ := rpc.CallerFrom(ctx)
		can, err := p.rights.Can(ctx, caller.AccountID, authz.CanView, profileID)
		switch {
		case err != nil:
			return nil, rpc.Failed(ctx, err)
		case !can:
			return nil, connect.NewError(connect.CodePermissionDenied, "")
		}
		l, err := store.LearnerOf(ctx, s.db, caller.AccountID, profileID)
		if err != nil {
			return nil, refused(ctx, err)
		}
		category = formatv0.AgeCategory(l.AgeCategory)
	default:
		return nil, rpc.Malformed("action")
	}
	// The index does not change while serve runs: no need to read it in the transaction.
	teachers := s.set.Teachers(category)
	var answer *v1.GetSkillMapResponse
	err := s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		connected, err := store.ConnectedModules(ctx, tx, profileID)
		if err != nil {
			return err
		}
		module := formatv0.ModuleID(req.GetModuleId())
		if !slices.ContainsFunc(connected, func(c store.ConnectedModule) bool { return c.ModuleID == string(module) }) {
			return store.ErrNotFound
		}
		pkg, ok := s.set.CurrentFor(module, category)
		if !ok {
			return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_CURRENT_VERSION)
		}
		// The modules of the engine: those connected and fit for the learner.
		var engine []string
		for _, c := range connected {
			if _, ok := s.set.CurrentFor(formatv0.ModuleID(c.ModuleID), category); ok {
				engine = append(engine, c.ModuleID)
			}
		}
		settled, err := s.model.Settle(ctx, tx, profileID, now)
		if err != nil {
			return err
		}
		read, err := s.model.Read(settled, now, engine)
		if err != nil {
			return err
		}
		answer = skillMap(pkg.View, string(pkg.Digest), read, offered(teachers, connected))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// offered gives, of an anchor skill, the modules of teachers that teach it and
// are not connected, in their order: those the skill map offers beside it
// (R63 spec; contract, AnchorSkillState).
func offered(teachers map[formatv0.AnchorSkillID][]formatv0.ModuleID, connected []store.ConnectedModule) func(anchor string) []string {
	ids := map[string]bool{}
	for _, c := range connected {
		ids[c.ModuleID] = true
	}
	return func(anchor string) []string {
		var out []string
		for _, id := range teachers[formatv0.AnchorSkillID(anchor)] {
			if !ids[string(id)] {
				out = append(out, string(id))
			}
		}
		return out
	}
}

// skillMap is the map of the module of view, of the package digest: the state
// of each skill of its graph with a verdict, by local ID — with no practice, the
// count of its evidence alone —, and of each anchor skill its graph names as a
// prerequisite or a skill it teaches, with the modules offered that teach it
// (contract, GetSkillMapResponse).
func skillMap(view formatv0.CanonicalView, digest string, read learnermodel.Readout, offers func(anchor string) []string) *v1.GetSkillMapResponse {
	out := &v1.GetSkillMapResponse{PackageDigest: digest}
	graph := view.SkillGraph
	if graph == nil {
		return out
	}
	module := string(view.Manifest.ID)
	practised := map[string]learnermodel.SkillReadout{}
	for _, m := range read.Modules {
		if m.Module != module {
			continue
		}
		for _, sk := range m.Skills {
			practised[sk.Skill] = sk
		}
	}
	anchors := map[string]bool{}
	var ids []string
	for id, skill := range graph.Skills {
		for _, a := range skill.Teaches {
			anchors[string(a)] = true
		}
		for _, a := range skill.Prerequisites {
			if model.IsAnchor(a) {
				anchors[a] = true
			}
		}
		if skill.Assessability == formatv0.SkillAssessabilityVerdict {
			ids = append(ids, string(id))
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		out.Skills = append(out.Skills, skillState(id, practised[model.SkillID(view.Manifest.ID, id)]))
	}
	for _, a := range read.Anchors {
		if anchors[a.Anchor] {
			out.AnchorSkills = append(out.AnchorSkills, anchorState(a, offers(a.Anchor)))
		}
	}
	return out
}

// skillState is the state of the skill id, by what reading gave of it: the
// count of its evidence, and, once practised, its estimate, uncertainty and
// retention, and, once mastered, its band, its cautious probability and its
// review date.
func skillState(id string, r learnermodel.SkillReadout) *v1.SkillState {
	out := &v1.SkillState{Skill: id, EvidenceCount: int32(r.EvidenceCount)}
	if r.EvidenceCount == 0 {
		return out
	}
	out.LastPracticedAt = timestamppb.New(time.Unix(r.LastPractice, 0))
	out.Estimate, out.Uncertainty, out.Retention = &r.Estimate, &r.Uncertainty, &r.Retention
	if !r.Mastered {
		return out
	}
	out.MasteryBand, out.CautiousProbability = r.MasteryBand.ID, &r.CautiousProbability
	if r.Review.Already {
		out.ReviewDue = true
	} else {
		out.ReviewDate = timestamppb.New(time.Unix(r.Review.At, 0))
	}
	return out
}

// The holdings and reasons of an anchor skill, in the contract's words.
var (
	holdings = map[learnermodel.AnchorStatus]v1.AnchorSkillHolding{
		learnermodel.AnchorHas:     v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_HELD,
		learnermodel.AnchorHasNot:  v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_NOT_HELD,
		learnermodel.AnchorUnknown: v1.AnchorSkillHolding_ANCHOR_SKILL_HOLDING_UNKNOWN,
	}
	anchorReasons = map[learnermodel.AnchorReason]v1.AnchorSkillReason{
		learnermodel.AnchorReasonNotMastered:      v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NOT_MASTERED,
		learnermodel.AnchorReasonNoEvidence:       v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NO_EVIDENCE,
		learnermodel.AnchorReasonNoTeacher:        v1.AnchorSkillReason_ANCHOR_SKILL_REASON_NOBODY_TEACHES,
		learnermodel.AnchorReasonNoVerdictTeacher: v1.AnchorSkillReason_ANCHOR_SKILL_REASON_ONLY_UNASSESSABLE,
	}
)

// anchorState is the state of an anchor skill, as reading gave it, with the
// modules offered that teach it, whatever its holding.
func anchorState(a learnermodel.AnchorReadout, offered []string) *v1.AnchorSkillState {
	out := &v1.AnchorSkillState{AnchorSkill: a.Anchor, Holding: holdings[a.Status], Reason: anchorReasons[a.Reason],
		TeachingModules: offered}
	for _, t := range a.Teachers {
		out.TeachingSkills = append(out.TeachingSkills, t.Skill)
	}
	return out
}

// GetProgress is the progress of a profile, which an adult views with the right
// can_view, which the checks hold (R49): its attempts, newest first, a page at a
// time, and, on the first page, the traps that repeat in its closed attempts.
// Each attempt has when the engine chose its task, or its run's lesson, and why
// — but one begun again (R50); an essay's submissions come with the path of a
// task with a rubric (R14).
func (p *ProgressService) GetProgress(ctx context.Context, req *v1.GetProgressRequest) (*v1.GetProgressResponse, error) {
	s := p.s
	profileID := req.GetProfileId()
	after, err := cursorOf(req.GetPageToken())
	if err != nil {
		return nil, rpc.Malformed("page_token")
	}
	answer := &v1.GetProgressResponse{}
	err = s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		if _, err := s.model.Settle(ctx, tx, profileID, now); err != nil {
			return err
		}
		records, err := store.ProgressPage(ctx, tx, profileID, after, progressPage+1)
		if err != nil {
			return err
		}
		if len(records) > progressPage {
			records = records[:progressPage]
			last := records[len(records)-1]
			answer.NextPageToken = tokenOf(store.Cursor{StartedAt: last.StartedAt, ID: last.ID})
		}
		for _, r := range records {
			answer.Attempts = append(answer.Attempts, attemptRecord(r, s.set.Packages[formatv0.Digest(r.PackageDigest)]))
		}
		if after != nil {
			return nil
		}
		journal, err := store.Journal(ctx, tx, profileID, 0)
		if err != nil {
			return err
		}
		traps, err := s.model.RepeatingTraps(journal)
		if err != nil {
			return err
		}
		for _, t := range traps {
			answer.RepeatingTraps = append(answer.RepeatingTraps, &v1.RepeatingTrap{
				Trap: t.ID, Description: t.Description, TrapAdvice: t.Advice,
				AttemptCount: int32(t.Attempts), LastClosedAt: timestamppb.New(t.LastClosedAt),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// solvedBy is the close reasons of an attempt solved: those whose progress names
// the skills its task trains (R99 spec).
var solvedBy = map[string]bool{closeCorrect: true, closeShown: true, closeDone: true}

// attemptRecord is the attempt r in the progress, with the titles of pkg, the
// package it went on: of its lesson, and of the skills of its task once it is
// solved. pkg is nil when the image no longer holds it, and the titles are
// empty (R99 spec).
func attemptRecord(r store.Record, pkg *modulepackage.Package) *v1.AttemptRecord {
	out := &v1.AttemptRecord{
		Task: model.TaskID(formatv0.ModuleID(r.ModuleID), formatv0.TaskID(r.TaskID)), ModuleVersion: r.ModuleVersion,
		WrongAnswers: int32(r.WrongAnswers), BegunAt: timestamppb.New(r.StartedAt),
	}
	if r.LessonID != nil {
		out.Lesson = *r.LessonID
		if pkg != nil {
			if lesson, ok := lessonOf(pkg, *r.LessonID); ok {
				out.LessonTitle = string(lesson.Title)
			}
		}
	}
	if r.CloseReason != nil {
		out.CloseReason = closeReasons[*r.CloseReason]
		out.ClosedAt = timestamppb.New(*r.ClosedAt)
		if pkg != nil && solvedBy[*r.CloseReason] {
			if task, ok := pkg.View.Tasks[formatv0.TaskID(r.TaskID)]; ok {
				out.SkillTitles = skillTitles(pkg.View.SkillGraph, task)
			}
		}
	}
	if r.ChosenAt != nil {
		out.ChosenAt, out.ReasonCode = timestamppb.New(*r.ChosenAt), v1.ReasonCode(*r.ReasonCode)
	}
	return out
}

// skillTitles is the titles in graph of the skills the task trains: the primary
// skills of its items, each item's own or else the task's, each once, in the
// order of the items and of their lists (contract, AttemptRecord) — not
// primarySkills, which sorts them.
func skillTitles(graph *formatv0.SkillGraph, task formatv0.Task) []string {
	if graph == nil {
		return nil
	}
	seen := map[formatv0.SkillID]bool{}
	var out []string
	for _, item := range task.Items {
		primary := task.Skills.Primary
		if item.Skills != nil {
			primary = item.Skills.Primary
		}
		for _, id := range primary {
			skill, ok := graph.Skills[id]
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, string(skill.Title))
		}
	}
	return out
}

// errToken is a page token this service did not give.
var errToken = errors.New("learning: not a page token of the progress")

// tokenOf is the page token of a cursor: opaque to app, which gives it back as
// it got it.
func tokenOf(c store.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.StartedAt.UnixMicro(), 10) + "/" + c.ID))
}

// lastMicros is the last microsecond of the year 9999, the last a Timestamp of
// protobuf holds, in which the service gives an attempt's beginning.
var lastMicros = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro() - 1

// cursorOf is the cursor of a page token, nil for the first page. An attempt
// begins by the service's clock, between 1970 and the year 9999: a time out of
// them is no token the service gave, and one before 4713 BC the database would
// not take.
func cursorOf(token string) (*store.Cursor, error) {
	if token == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, errToken
	}
	at, id, ok := strings.Cut(string(data), "/")
	micros, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || micros < 0 || micros > lastMicros || !store.CanonicalID(id) {
		return nil, errToken
	}
	return &store.Cursor{StartedAt: time.UnixMicro(micros).UTC(), ID: id}, nil
}
