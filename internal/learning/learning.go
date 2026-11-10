// Package learning runs attempts of tasks (R44, R46, R47): their beginning, the
// open attempt, the submission of the work and its verdict by module-kit, on
// the options tapped and on what tutor read, the techniques a solving step
// gives, "Done", the hint ladder with the tutor's words and their fallback
// texts, the showing of the solution, and lesson runs with their segments
// (R48). It answers the methods of LearningService and AttemptService that it
// has; the others answer unimplemented, until the tasks of epic E3 that build
// them — the engine's choice.
//
// Every request is a learning request on a profile, whose right the checks hold
// before it (R33), and a profile whose consent is not confirmed is refused. An
// attempt is read by the pair of its profile and its ID, its row held for the
// request: one of another profile, or none, is "not found" (R04).
package learning

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/module-kit/modulepackage"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
	"github.com/altnovatrail/api/internal/version"
)

// The services the package answers, as the contract names them.
var (
	_ apiv1connect.LearningServiceHandler = (*LearningService)(nil)
	_ apiv1connect.AttemptServiceHandler  = (*AttemptService)(nil)
)

// Service runs the attempts of the profiles on the packages the image holds.
type Service struct {
	set  *packages.Set
	gate *admission.Gate
	// reader reads the work of a handwriting sheet, and teller words the
	// tutor's words: the gate's tutor.
	reader sheetReader
	teller wordsTeller
	// goingFor is the longest a request may run, past which the record of its
	// key going is of a request that fell (R17, R33).
	goingFor  time.Duration
	db        rpc.DB
	now       func() time.Time
	moduleKit string
}

// Option sets what a Service runs on, for the tests.
type Option func(*Service)

// WithClock is the clock a Service tells the time of its actions by.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithRequestTimeout is the longest a request may run, as the server bounds it
// (R33).
func WithRequestTimeout(d time.Duration) Option { return func(s *Service) { s.goingFor = d } }

// sheetReader reads the work of a handwriting sheet, as tutor does.
type sheetReader interface {
	ReadSheet(ctx context.Context, r tutor.SheetReading) tutor.SheetRead
}

// New is the Service of the packages of set, which asks gate before a task that
// AI checks; its handlers run on the database they are given. A binary that
// recorded no version of module-kit is refused, before anything is opened:
// every attempt keeps it. So is no gate.
func New(set *packages.Set, gate *admission.Gate, opts ...Option) (*Service, error) {
	s := &Service{set: set, gate: gate, reader: gate, teller: gate, now: time.Now, moduleKit: version.ModuleKit(),
		goingFor: config.DefaultRequestTimeout}
	for _, opt := range opts {
		opt(s)
	}
	if gate == nil {
		return nil, errors.New("learning: no gate of admission")
	}
	if s.moduleKit == "" {
		return nil, errors.New("learning: the build recorded no version of module-kit")
	}
	return s, nil
}

// LearningService is the way from a module to a task: here, the beginning of an
// attempt and the open attempt.
type LearningService struct {
	apiv1connect.UnimplementedLearningServiceHandler
	s *Service
}

// Learning is the LearningService of s, on db.
func (s *Service) Learning(db rpc.DB) *LearningService { return &LearningService{s: s.on(db)} }

// AttemptService is the requests on an attempt: the submission of the work,
// the beginning of a step and "Done", the hint ladder and the showing of the
// solution.
type AttemptService struct {
	apiv1connect.UnimplementedAttemptServiceHandler
	s      *Service
	rights canner
}

// Attempts is the AttemptService of s, on db, which asks rights of an adult's
// action.
func (s *Service) Attempts(db rpc.DB, rights canner) *AttemptService {
	return &AttemptService{s: s.on(db), rights: rights}
}

// on is s on db.
func (s *Service) on(db rpc.DB) *Service {
	c := *s
	c.db = db
	return &c
}

// clock is the time of an action, to the microsecond, as the database keeps
// it: an answer recorded and a refusal read later name one instant.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// learner is the age category of the profile, whose consent is confirmed
// (rpc.Learner).
func (s *Service) learner(ctx context.Context, profileID string) (formatv0.AgeCategory, error) {
	category, err := rpc.Learner(ctx, s.db, profileID)
	return formatv0.AgeCategory(category), err
}

// held is an open attempt as a request acts on it: its row, held, its package,
// and the task it got — an instance, when the task is a template.
type held struct {
	row  store.Attempt
	pkg  *modulepackage.Package
	task formatv0.Task
}

// closedError is an attempt closed, by its reason.
type closedError struct{ reason string }

func (e *closedError) Error() string { return "learning: the attempt is closed " + e.reason }

// errPackageUnknown is an attempt on a package whose digest the index of the
// image does not know: a deployment is going on (R07).
var errPackageUnknown = errors.New("learning: the index of the image does not know the package")

// lapsed is why the open attempt row can go on no longer at now, as the check
// of a request and the beginning of another attempt close it: "unchecked" on a
// package the image no longer gives (R07), "abandoned" past 7 days with no
// action, as the daily job closes it (R10); empty when it can go on.
func (s *Service) lapsed(row store.Attempt, now time.Time) string {
	switch {
	case s.set.Status(formatv0.Digest(row.PackageDigest)) == packages.NotGiven:
		return closeUnchecked
	case now.After(row.LastActionAt.Add(store.AbandonedAfter)):
		return closeAbandoned
	}
	return ""
}

// hold is the profile's open attempt, held in tx. Before anything acts on it, it
// is checked as every request checks it: one the profile does not have is
// store.ErrNotFound; one closed, a closedError; one on a package the index does
// not know, errPackageUnknown, with nothing written; and one that lapsed is
// closed by this check, and the refusal "closed" keeps the close (rpc.Keep).
func (s *Service) hold(ctx context.Context, tx pgx.Tx, profileID, attemptID string, now time.Time) (held, error) {
	if !store.CanonicalID(attemptID) {
		return held{}, store.ErrNotFound
	}
	// The run first, as the requests on the run hold it: a close here may end it.
	if err := store.HoldAttemptRun(ctx, tx, profileID, attemptID); err != nil {
		return held{}, err
	}
	row, err := store.HoldAttempt(ctx, tx, profileID, attemptID)
	if err != nil {
		return held{}, err
	}
	if row.CloseReason != nil {
		return held{}, &closedError{reason: *row.CloseReason}
	}
	digest := formatv0.Digest(row.PackageDigest)
	if s.set.Status(digest) == packages.Unknown {
		return held{}, errPackageUnknown
	}
	if reason := s.lapsed(row, now); reason != "" {
		if err := store.CloseAttempt(ctx, tx, profileID, attemptID, reason, now); err != nil {
			return held{}, err
		}
		return held{}, rpc.Keep(refused(ctx, &closedError{reason: reason}))
	}
	pkg := s.set.Packages[digest]
	task, err := instanceAgain(pkg, formatv0.TaskID(row.TaskID), row.Instance)
	if err != nil {
		return held{}, err
	}
	return held{row: row, pkg: pkg, task: task}, nil
}

// act runs fn on the profile's open attempt, held, under the caller's account,
// as write does.
func (s *Service) act(ctx context.Context, profileID, attemptID string, fn func(tx pgx.Tx, h held, now time.Time) error) error {
	return s.write(ctx, func(tx pgx.Tx, now time.Time) error {
		h, err := s.hold(ctx, tx, profileID, attemptID, now)
		if err != nil {
			return err
		}
		return fn(tx, h, now)
	})
}

// write runs fn, a request with no key, under the caller's account, which a
// deletion marks (R21), at one time. What a refusal of rpc.Keep keeps — a close
// or an end by the check of the request — commits, and the request is refused
// after it, as rpc.Once does for a request with a key.
func (s *Service) write(ctx context.Context, fn func(tx pgx.Tx, now time.Time) error) error {
	caller, _ := rpc.CallerFrom(ctx)
	now := s.clock()
	var keptRefusal error
	err := store.WriteUnderAccount(ctx, s.db, caller.AccountID, func(tx pgx.Tx) error {
		err := fn(tx, now)
		if refusal, ok := rpc.Kept(err); ok {
			keptRefusal = refusal
			return nil
		}
		return err
	})
	if err == nil {
		err = keptRefusal
	}
	if err != nil {
		return refused(ctx, err)
	}
	return nil
}

// The close reasons of an attempt, as the schema writes them.
const (
	closeCorrect   = "correct"
	closeLeft      = "left"
	closeUnchecked = "unchecked"
	closeAbandoned = "abandoned"
)

// closeReasons is each close reason of the schema in the contract's words.
var closeReasons = map[string]v1.CloseReason{
	closeCorrect:   v1.CloseReason_CLOSE_REASON_CORRECT,
	closeShown:     v1.CloseReason_CLOSE_REASON_SOLUTION_SHOWN,
	closeLeft:      v1.CloseReason_CLOSE_REASON_LEFT,
	closeAbandoned: v1.CloseReason_CLOSE_REASON_ABANDONED,
	closeUnchecked: v1.CloseReason_CLOSE_REASON_UNCHECKED,
	"not_suitable": v1.CloseReason_CLOSE_REASON_NOT_SUITABLE,
	"done":         v1.CloseReason_CLOSE_REASON_DONE,
}

// refused answers what a request met: a refusal of the contract as it is, the
// attempt, the run or the choice not found, the run ended with its end, the
// attempt closed with its reason, the
// package unknown, the profile or the account gone meanwhile, or a failure.
func refused(ctx context.Context, err error) error {
	var refusal *connect.Error
	var closed *closedError
	var ended *runEndedError
	switch {
	case errors.As(err, &refusal):
		return err
	case errors.As(err, &ended):
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_RUN_ENDED, func(r *v1.Refusal) {
			r.Data = &v1.Refusal_RunEnd{RunEnd: runEnd(ended.reason)}
		})
	case errors.As(err, &closed):
		return rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_ATTEMPT_CLOSED, func(r *v1.Refusal) {
			r.Data = &v1.Refusal_CloseReason{CloseReason: closeReasons[closed.reason]}
		})
	case errors.Is(err, store.ErrNotFound):
		return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NOT_FOUND)
	case errors.Is(err, errPackageUnknown):
		return rpc.Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	case errors.Is(err, store.ErrChoiceBegun):
		return rpc.Refuse(ctx, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
	case errors.Is(err, store.ErrNoProfile):
		return rpc.Refuse(ctx, connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
	case errors.Is(err, store.ErrAccountDeleting), errors.Is(err, store.ErrNoAccount):
		return rpc.AccountGone(ctx)
	}
	return rpc.Failed(ctx, err)
}
