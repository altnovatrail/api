package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/verdict"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// MaxWork is the largest stored work, as StoredWork counts it: the bytes of its
// history, of the contexts of its submissions and of its picture (R113 spec).
const MaxWork = 4 << 20

// pngSignature is what the picture of a work begins with (RFC 2083, section 3.1).
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// workKinds is each kind of a stored work, as the contract names it.
var workKinds = map[string]v1.WorkKind{
	store.WorkHandwriting: v1.WorkKind_WORK_KIND_HANDWRITING,
	store.WorkEssay:       v1.WorkKind_WORK_KIND_ESSAY,
	store.WorkDrawing:     v1.WorkKind_WORK_KIND_DRAWING,
}

// errNoKind is a work of an attempt whose package the image holds no longer —
// out of the window of format versions —, with no work kept of it before to
// tell its kind.
var errNoKind = errors.New("learning: the image no longer holds the package of the attempt")

// WorkService is the stored works of the profiles (R70): each work of a profile
// as the device holds it, which api keeps while the storage of the profile's
// works is on, reads no more of than its form, and gives to no AI provider
// (R113 spec; contract, section 15). Each request repeats by the state of the
// profile's works, with no idempotency key.
type WorkService struct {
	apiv1connect.UnimplementedWorkServiceHandler
	s      *Service
	rights canner
}

// Works is the WorkService of s, on db, which asks rights of an adult's action.
func (s *Service) Works(db rpc.DB, rights canner) *WorkService {
	return &WorkService{s: s.on(db), rights: rights}
}

// PutWork keeps the work of an attempt of the profile, in the place of the one
// kept of it before, while the storage of the profile's works is on: a learning
// request. The work is checked before anything is read of the attempt — its
// size, then its form, which every stored work must have to go into the export
// —, and kept as a StoredWork of the fields the contract names alone: a field
// it does not name, which binary protobuf carries, is neither kept nor counted.
// Its kind comes of the attempt's task, and when it was kept of the clock.
func (w *WorkService) PutWork(ctx context.Context, req *v1.PutWorkRequest) (*v1.PutWorkResponse, error) {
	s := w.s
	profileID := req.GetProfileId()
	if _, err := s.learner(ctx, profileID); err != nil {
		return nil, err
	}
	work := req.GetWork()
	if !store.CanonicalID(work.GetAttemptId()) {
		return nil, refused(ctx, store.ErrNotFound)
	}
	size := workSize(work)
	if size > MaxWork {
		return nil, rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_WORK_TOO_LARGE)
	}
	if field := WorkMalformed(work); field != "" {
		return nil, rpc.Malformed(field)
	}
	kind, err := s.workKind(ctx, profileID, work.GetAttemptId())
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	data, err := proto.Marshal(&v1.StoredWork{AttemptId: work.GetAttemptId(), History: work.GetHistory(),
		Submissions: keptSubmissions(work.GetSubmissions()), Picture: work.GetPicture()})
	if err != nil {
		return nil, rpc.Failed(ctx, err)
	}
	stored := store.Work{AttemptID: work.GetAttemptId(), Kind: kind, SizeBytes: size, StoredAt: s.clock(), Bytes: data}
	caller, _ := rpc.CallerFrom(ctx)
	err = store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		return store.PutWork(ctx, tx, caller.AccountID, profileID, stored)
	})
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	return &v1.PutWorkResponse{Work: workInfo(stored)}, nil
}

// ListWorks is the profile's stored works, newest first, without their bytes:
// a learning request, or an adult's action by can_manage. While the storage is
// off there are none: switching it off erased them.
func (w *WorkService) ListWorks(ctx context.Context, req *v1.ListWorksRequest) (*v1.ListWorksResponse, error) {
	if err := w.reader(ctx, req.GetProfileId(), req.GetAction()); err != nil {
		return nil, err
	}
	var works []store.Work
	err := pgx.BeginFunc(ctx, w.s.db, func(tx pgx.Tx) (err error) {
		works, err = store.ListWorks(ctx, tx, req.GetProfileId())
		return err
	})
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	out := &v1.ListWorksResponse{Works: []*v1.StoredWorkInfo{}}
	for _, work := range works {
		out.Works = append(out.Works, workInfo(work))
	}
	return out, nil
}

// GetWork is one stored work of the profile, whole, as it was put, with its
// kind and when it was kept: as ListWorks. An attempt with no work kept, or not
// the profile's, is "not found".
func (w *WorkService) GetWork(ctx context.Context, req *v1.GetWorkRequest) (*v1.GetWorkResponse, error) {
	if err := w.reader(ctx, req.GetProfileId(), req.GetAction()); err != nil {
		return nil, err
	}
	if !store.CanonicalID(req.GetAttemptId()) {
		return nil, refused(ctx, store.ErrNotFound)
	}
	kept, err := store.GetWork(ctx, w.s.db, req.GetProfileId(), req.GetAttemptId())
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	var work v1.StoredWork
	if err := proto.Unmarshal(kept.Bytes, &work); err != nil {
		return nil, rpc.Failed(ctx, err)
	}
	// The row names the attempt, whatever the bytes say.
	work.AttemptId, work.Kind, work.StoredAt = kept.AttemptID, workKinds[kept.Kind], timestamppb.New(kept.StoredAt)
	return &v1.GetWorkResponse{Work: &work}, nil
}

// DeleteWork erases one stored work of the profile, by whoever decides the
// storage of its works, as SetWorkStorage: an adult's action by can_manage, and
// at a profile whose learner gave their own consent, the learner's learning
// request, which is checked as every learning request is. Who decides is read
// without a lock. The answer is "erased", of an attempt of the profile with no
// work kept too; the work on the device stays.
func (w *WorkService) DeleteWork(ctx context.Context, req *v1.DeleteWorkRequest) (*v1.DeleteWorkResponse, error) {
	s := w.s
	profileID := req.GetProfileId()
	action := req.GetAction()
	if err := w.reader(ctx, profileID, action); err != nil {
		return nil, err
	}
	caller, _ := rpc.CallerFrom(ctx)
	giver, err := store.WorkDecider(ctx, s.db, caller.AccountID, profileID)
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	switch learnerDecides := giver == store.GiverLearner; {
	case learnerDecides && action == v1.Action_ACTION_ADULT:
		return nil, rpc.Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_LEARNER_DECIDES)
	case !learnerDecides && action == v1.Action_ACTION_LEARNING:
		return nil, rpc.Refuse(ctx, connect.CodePermissionDenied, v1.Cause_CAUSE_ADULT_NEEDED)
	}
	if !store.CanonicalID(req.GetAttemptId()) {
		return nil, refused(ctx, store.ErrNotFound)
	}
	err = store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		there, err := store.AttemptOfProfile(ctx, tx, profileID, req.GetAttemptId())
		switch {
		case err != nil:
			return err
		case !there:
			return store.ErrNotFound
		}
		return store.DeleteWork(ctx, tx, profileID, req.GetAttemptId())
	})
	if err != nil {
		return nil, refusedWork(ctx, err)
	}
	return &v1.DeleteWorkResponse{}, nil
}

// reader checks the action of a request on the profile's works: a learning
// request as every one is, an adult's by can_manage, which the checks before it
// do not hold.
func (w *WorkService) reader(ctx context.Context, profileID string, action v1.Action) error {
	switch action {
	case v1.Action_ACTION_LEARNING:
		_, err := w.s.learner(ctx, profileID)
		return err
	case v1.Action_ACTION_ADULT:
		caller, _ := rpc.CallerFrom(ctx)
		can, err := w.rights.Can(ctx, caller.AccountID, authz.CanManage, profileID)
		switch {
		case err != nil:
			return rpc.Failed(ctx, err)
		case !can:
			return connect.NewError(connect.CodePermissionDenied, "")
		}
		return nil
	}
	return rpc.Malformed("action")
}

// workKind is the kind of the work of the profile's attempt, by its task, read
// with no lock: what an attempt is of never changes. The package is the
// image's, withdrawn too; one whose digest the index does not know is a
// deployment going on. A task the image holds no longer — its package out of
// the window — leaves the kind of the work kept of the attempt before, or
// errNoKind.
func (s *Service) workKind(ctx context.Context, profileID, attemptID string) (string, error) {
	digest, taskID, err := store.AttemptTask(ctx, s.db, profileID, attemptID)
	if err != nil {
		return "", err
	}
	if pkg, ok := s.set.Packages[formatv0.Digest(digest)]; ok {
		if task, ok := pkg.View.Tasks[formatv0.TaskID(taskID)]; ok {
			return workKind(task), nil
		}
	} else if s.set.Status(formatv0.Digest(digest)) == packages.Unknown {
		return "", errPackageUnknown
	}
	kind, err := store.KeptWorkKind(ctx, s.db, profileID, attemptID)
	if errors.Is(err, store.ErrNotFound) {
		return "", errNoKind
	}
	return kind, err
}

// workKind is what a work of the task is (contract, WorkKind): an essay, when an
// item has a rubric; handwriting, when an answer slot is checked by a key or a
// predicate; a drawing, when none is checked.
func workKind(task formatv0.Task) string {
	kind := store.WorkDrawing
	for _, item := range task.Items {
		for _, slot := range verdict.AnswerSlots(item) {
			switch slot.Check.Kind {
			case formatv0.CheckKindRubric:
				return store.WorkEssay
			case "":
			default:
				kind = store.WorkHandwriting
			}
		}
	}
	return kind
}

// workSize is the size of the work, as StoredWork counts it.
func workSize(work *v1.StoredWork) int64 {
	size := int64(len(work.GetHistory())) + int64(len(work.GetPicture()))
	for _, s := range work.GetSubmissions() {
		size += int64(len(s.GetContext()))
	}
	return size
}

// WorkMalformed is the field of the work whose form the contract refuses, or
// none (contract, section 15): its history a JSON array and each context a JSON
// object, in UTF-8, so that the export writes them as the JSON they are; each
// count of the history's events not below zero, as the export's schema asks;
// and its picture a PNG by its signature. Nothing more is read of them.
func WorkMalformed(work *v1.StoredWork) string {
	if !jsonOf(work.GetHistory(), '[') {
		return "history"
	}
	for _, s := range work.GetSubmissions() {
		if !jsonOf(s.GetContext(), '{') {
			return "context"
		}
		if s.GetHistoryLength() < 0 {
			return "history_length"
		}
	}
	if !bytes.HasPrefix(work.GetPicture(), pngSignature) {
		return "picture"
	}
	return ""
}

// jsonOf says whether data is JSON in UTF-8 whose value opens with open: an
// array or an object.
func jsonOf(data []byte, open byte) bool {
	value := bytes.TrimLeft(data, " \t\r\n")
	return len(value) > 0 && value[0] == open && utf8.Valid(data) && json.Valid(data)
}

// keptSubmissions is the submissions of a work by the fields the contract names.
func keptSubmissions(submissions []*v1.WorkSubmission) []*v1.WorkSubmission {
	out := make([]*v1.WorkSubmission, 0, len(submissions))
	for _, s := range submissions {
		out = append(out, &v1.WorkSubmission{Context: s.GetContext(), HistoryLength: s.GetHistoryLength()})
	}
	return out
}

// workInfo is the stored work named, without its bytes.
func workInfo(work store.Work) *v1.StoredWorkInfo {
	return &v1.StoredWorkInfo{AttemptId: work.AttemptID, Kind: workKinds[work.Kind], StoredAt: timestamppb.New(work.StoredAt),
		SizeBytes: work.SizeBytes}
}

// refusedWork answers what a request on the works met: the storage off, a work
// whose kind cannot be told, or what every request on an attempt meets.
func refusedWork(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrStorageOff):
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_STORAGE_OFF)
	case errors.Is(err, errNoKind):
		return connect.NewError(connect.CodeFailedPrecondition, "")
	}
	return refused(ctx, err)
}
