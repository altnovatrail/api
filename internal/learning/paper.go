package learning

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/quota"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// The bounds of a page from paper, the contract's (R162 spec): the photo, a JPEG
// or a PNG, and how many attempts it begins.
const (
	maxPhoto      = 4 << 20
	maxPaperTasks = 20
)

// PaperService is the requests of a task from the learner's paper (R10; R162
// spec): the finding of the tasks on the photo of a page, and the beginning of
// the attempts of the tasks picked, which AttemptService then checks.
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
// order of the page. It makes no call without the admission of the finding on
// the request's route, nor on platform AI without the reserve of the quota:
// either is a refusal, and the family day the request wrote stays. A call that
// gives no frames is an outcome, and the learner frames the tasks by hand. The
// photo goes no further than the call, and the record of the key holds the
// outcome alone: a repeat gets an answer "repeated" without frames.
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
	profileID := req.GetProfileId()
	key := rpc.Key{
		Method:   apiv1connect.PaperServiceFindTasksProcedure,
		GoingFor: s.goingFor,
		Repeat: func(_ context.Context, _ pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.FindTasksResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			answer.Repeated = true
			return answer, nil
		},
	}
	route := routeOf(category, req.GetOwnKey())
	admitted := s.admits.Reads(tutor.CallFinding, category, req.GetOwnKey().GetProvider())
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
			f := finding{request: tutor.TaskFinding{Route: route, Photo: req.GetPhoto()}}
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
			} else {
				answer.Outcome = outcomeOf(f.found.Outcome, route.AI)
			}
			// An answer of the contract always marshals; the record keeps no frame.
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
// for the stub. The photo goes no further than the call.
func (s *Service) find(ctx context.Context, f finding, own string) finding {
	ctx, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	f.found = s.finder.FindTasks(ctx, f.request, own)
	f.request.Photo = nil
	noteCall(ctx, f.found.Outcome, time.Since(start))
	s.afterCall(ctx, f.reserve, f.found.Outcome)
	return f
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

// BeginPaperAttempts begins the attempts of the tasks picked on a page, under
// its idempotency key, one for each, their IDs in the order of the tasks. It
// closes the profile's open attempts of an earlier page "left", and leaves its
// open attempt of a module as it is (R10). A task from paper is read as
// handwriting, so the attempts begin when the gate admits the reading of a
// sheet on the route of ownKey, and on platform AI only with room for a typical
// task of handwriting left of the family day, while the limit of its provider
// leaves platform AI open, as BeginAttempt begins a task of a module (R11, R59).
func (p *PaperService) BeginPaperAttempts(ctx context.Context, req *v1.BeginPaperAttemptsRequest) (*v1.BeginPaperAttemptsResponse, error) {
	s := p.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if n := req.GetTasks(); n < 1 || n > maxPaperTasks {
		return nil, rpc.Malformed("tasks")
	}
	ownKey := req.GetOwnKeyProvider()
	if err := ownKeyFits(ctx, category, ownKey != ""); err != nil {
		return nil, err
	}
	profileID := req.GetProfileId()
	answer, err := rpc.Once(ctx, s.db, rpc.Key{Method: apiv1connect.PaperServiceBeginPaperAttemptsProcedure}, req,
		func(tx pgx.Tx, _ string) (*v1.BeginPaperAttemptsResponse, store.KeyTarget, error) {
			now := s.clock()
			if !s.admits.Reads(tutor.CallSheet, category, ownKey) {
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
			ids, err := store.BeginPaperAttempts(ctx, tx, profileID, int(req.GetTasks()), now)
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
