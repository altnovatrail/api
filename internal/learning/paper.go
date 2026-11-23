package learning

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The bounds of a page from paper, the contract's: the photo, a JPEG or a PNG,
// and how many attempts it begins; and of the learner's request about it, in
// code points, and of the frames that go with it.
const (
	maxPhoto      = 4 << 20
	maxPaperTasks = 20
	maxRequest    = 200
	maxDrawn      = 40
)

// PaperService is the requests of a task from the learner's paper: the finding
// of the tasks on the photo of a page, and the beginning of the attempts of the
// tasks picked, which AttemptService then checks.
type PaperService struct {
	apiv1connect.UnimplementedPaperServiceHandler
	s *Service
}

// Paper is the PaperService of s, on db.
func (s *Service) Paper(db rpc.DB) *PaperService { return &PaperService{s: s.on(db)} }

// taskFinder finds the tasks on the photo of a page, as tutor does; own is as
// sheetReader has it.
type taskFinder interface {
	FindTasks(ctx context.Context, f tutor.TaskFinding, own string) tutor.TasksFound
}

// finding is a request to find the tasks as it goes: the call, the reserve it
// took, and what came of it.
type finding struct {
	request tutor.TaskFinding
	reserve reserve
	found   tutor.TasksFound
}

// FindTasks finds the tasks on the photo of a page, under its idempotency key,
// by one call of AI between two transactions (rpc.Across): their frames, in the
// order of the page, on the photo turned by the turn tutor gave, which goes
// with them. It makes no call without the admission of the finding on the
// request's route, nor on platform AI without the reserve of the quota: either
// is a refusal, and the family day the request wrote stays. A call that gives
// no frames is an outcome, and the learner frames the tasks by hand. The photo
// goes no further than the call, and the record of the key holds the outcome
// alone: a repeat gets an answer "repeated" without frames or turn. With the
// learner's request the call gets it and the frames on the page, which go no
// further either.
func (p *PaperService) FindTasks(ctx context.Context, req *v1.FindTasksRequest) (*v1.FindTasksResponse, error) {
	s := p.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	if !photoTaken(req.GetPhoto()) {
		return nil, rpc.Malformed("photo")
	}
	if field := requestTaken(req); field != "" {
		return nil, rpc.Malformed(field)
	}
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.PaperServiceFindTasksProcedure,
		GoingFor: s.paperGoingFor,
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.FindTasksResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Repeated = true
			return answer, nil
		},
	}
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_FIND_TASKS, "")
	defer act.End()
	route := act.Route(routeOf(category, req.GetOwnKey()))
	// A first model of the debug mode takes the call past the list, as tutor does.
	admitted := act.Chosen() || s.admits.Reads(tutor.CallFinding, category, req.GetOwnKey().GetProvider())
	answer, err := rpc.Across(ctx, s.db, key, req, rpc.Phases[finding, *v1.FindTasksResponse]{
		Prepare: func(tx pgx.Tx, _ string) (finding, store.KeyTarget, error) {
			target := store.KeyTarget{ProfileID: profileID}
			if !admitted {
				return finding{}, target, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
			}
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
				return finding{}, target, connect.NewError(connect.CodeDeadlineExceeded, "too little time to find the tasks")
			}
			day, err := s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
			if err != nil {
				return finding{}, target, err
			}
			f := finding{request: tutor.TaskFinding{Route: route, Photo: req.GetPhoto(), Request: req.GetRequest(), Found: boxesOf(req.GetFound()), Drawn: boxesOf(req.GetDrawn())}}
			if f.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallFinding, 1, day); err == nil && f.reserve.refused != nil {
				err = refusal(ctx, f.reserve.refused)
			}
			return f, target, err
		},
		Call: func(ctx context.Context, f finding) finding { return s.find(ctx, f, req.GetOwnKey().GetKey()) },
		Finish: func(_ pgx.Tx, f finding) (*v1.FindTasksResponse, store.KeyTarget, error) {
			answer := &v1.FindTasksResponse{}
			if f.found.Outcome.Kind == tutor.Answered {
				for _, box := range f.found.Frames {
					answer.Frames = append(answer.Frames, &v1.TaskFrame{Box: box})
				}
				if len(answer.Frames) > 0 {
					answer.TurnDegrees = int32(f.found.Turn)
				}
			} else {
				answer.Outcome = outcomeOf(f.found.Outcome, route.AI)
			}
			// An answer of the contract always marshals; the record keeps no frame and no turn.
			written, _ := proto.Marshal(&v1.FindTasksResponse{Outcome: answer.GetOutcome()})
			return answer, store.KeyTarget{ProfileID: profileID, Response: written}, nil
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

// find calls the finding of f until finishReserve before the request's
// deadline, and settles its reserve; own is the value of the request's own key,
// for the stub. The photo and the learner's request go no further than the call.
func (s *Service) find(ctx context.Context, f finding, own string) finding {
	ctx, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	f.found = s.finder.FindTasks(ctx, f.request, own)
	f.request.Photo, f.request.Request = nil, ""
	noteCall(ctx, f.found.Outcome, time.Since(start))
	s.afterCall(ctx, f.reserve, f.found.Outcome)
	return f
}

// requestTaken is the field of the learner's request about the page that the
// contract refuses, or "" when it takes them: the request, at most maxRequest
// code points of UTF-8, no control character, not blank; the frames only with
// it, at most 20 found and maxDrawn drawn, each a rectangle in thousandths of
// the photo.
func requestTaken(req *v1.FindTasksRequest) string {
	text := req.GetRequest()
	switch {
	case !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxRequest || strings.ContainsFunc(text, unicode.IsControl),
		text != "" && strings.TrimSpace(text) == "":
		return "request"
	case len(req.GetFound()) > maxPaperTasks || (text == "" && len(req.GetFound()) > 0) || slices.ContainsFunc(req.GetFound(), offPhoto):
		return "found"
	case len(req.GetDrawn()) > maxDrawn || (text == "" && len(req.GetDrawn()) > 0) || slices.ContainsFunc(req.GetDrawn(), offPhoto):
		return "drawn"
	}
	return ""
}

// offPhoto says whether a frame is no rectangle in the photo, in thousandths.
func offPhoto(f *v1.TaskFrame) bool {
	b := f.GetBox()
	return b == nil || b.GetXmin() < 0 || b.GetXmin() >= b.GetXmax() || b.GetXmax() > 1000 ||
		b.GetYmin() < 0 || b.GetYmin() >= b.GetYmax() || b.GetYmax() > 1000
}

// boxesOf are the boxes of frames.
func boxesOf(frames []*v1.TaskFrame) []*observationv1.CropBox {
	boxes := make([]*observationv1.CropBox, len(frames))
	for i, f := range frames {
		boxes[i] = f.GetBox()
	}
	return boxes
}

// photoTaken says whether photo is a JPEG or a PNG of at most maxPhoto, by its
// header, whose size is read before anything is decoded: tutor decodes it whole
// before its call, and refuses what this lets through but cannot be read.
func photoTaken(photo []byte) bool {
	if len(photo) == 0 || len(photo) > maxPhoto {
		return false
	}
	var cfg image.Config
	var err error
	switch {
	case bytes.HasPrefix(photo, []byte("\xff\xd8\xff")):
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(photo))
	case bytes.HasPrefix(photo, []byte("\x89PNG\r\n\x1a\n")):
		cfg, err = png.DecodeConfig(bytes.NewReader(photo))
	default:
		return false
	}
	return err == nil && cfg.Width > 0 && cfg.Height > 0
}

// pageID is the ID app gives a page, which names its file of the export: one
// part of a path.
var pageID = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,199}$`)

// BeginPaperAttempts begins the attempts of the tasks picked on a page, under
// its idempotency key, one for each, their IDs in the order of the tasks. It
// closes the profile's open attempts of another page "left", those of the page
// named staying for a Start again after "Add", and leaves its open attempt of a
// module as it is. The attempts begin when the gate admits, on the route of
// ownKey, the calls without which no Check gives a verdict — the reading of the
// statement and that of the work; the solution serves only the tasks no formula
// keys —, and on platform AI only with room for a typical task of handwriting
// left of the family day, while the limit of its provider leaves platform AI
// open, as BeginAttempt begins a task of a module.
func (p *PaperService) BeginPaperAttempts(ctx context.Context, req *v1.BeginPaperAttemptsRequest) (*v1.BeginPaperAttemptsResponse, error) {
	s := p.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if n := req.GetTasks(); n < 1 || n > maxPaperTasks {
		return nil, rpc.Malformed("tasks")
	}
	if !pageID.MatchString(req.GetPageId()) {
		return nil, rpc.Malformed("page_id")
	}
	ownKey := req.GetOwnKeyProvider()
	if err := ownKeyFits(ctx, category, ownKey != ""); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.PaperServiceBeginPaperAttemptsProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.BeginPaperAttemptsResponse, store.KeyTarget, error) {
			now := s.clock()
			if !s.debugChosen(ctx) && !(s.admits.Reads(tutor.CallStatement, category, ownKey) && s.admits.Reads(tutor.CallPaper, category, ownKey)) {
				return nil, store.KeyTarget{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NOT_ADMITTED)
			}
			if ownKey == "" {
				closed, err := s.closedTo(ctx, tx, profileID, quota.TypicalHandwriting, now)
				if err != nil {
					return nil, store.KeyTarget{}, err
				}
				if closed != nil {
					return nil, store.KeyTarget{}, closedRefusal(ctx, closed)
				}
			}
			ids, err := store.BeginPaperAttempts(ctx, tx, profileID, req.GetPageId(), int(req.GetTasks()), now)
			if err != nil {
				return nil, store.KeyTarget{}, err
			}
			return &v1.BeginPaperAttemptsResponse{AttemptIds: ids}, store.KeyTarget{ProfileID: profileID}, nil
		})
	if err != nil {
		return nil, refused(ctx, err)
	}
	return answer, nil
}
