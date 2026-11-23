package learning

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/tutor"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// recognising is a request "Recognise" between the parts of its request: the
// attempt, the call and its reserve, what came of it, and the answer of an
// attempt that keeps a reading or a key, which calls nothing.
type recognising struct {
	attemptID string
	task      tutor.PaperTask
	reserve   reserve
	read      tutor.StatementRead
	ready     *v1.ReadStatementResponse
}

// ReadStatement is "Recognise" on a task from paper: the reading of the
// statement of the attempt's crop, the one its first Check would make, by one
// call of AI between two transactions (rpc.Across), whose reading the attempt
// keeps until it closes, for the Check to derive the key by with no call of its
// own. An attempt that keeps a reading or a key answers its statements,
// repeated, with no AI. The answer is the statements alone, never a formula. On
// platform AI the call goes only with the reserve of the quota, the statement's
// part of a first Check's, and a reserve refused is a refusal that keeps the
// family day the request wrote. A call that gives no reading is an outcome, and
// nothing is kept: the next press asks again. The crop goes no further than the
// call, and the record of the key holds the outcome alone.
func (a *AttemptService) ReadStatement(ctx context.Context, req *v1.ReadStatementRequest) (*v1.ReadStatementResponse, error) {
	s := a.s
	category, err := s.learner(ctx, req.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := ownKeySent(ctx, category, req.GetOwnKey()); err != nil {
		return nil, err
	}
	// The reading is the Check's first call, and goes by the Check's choice of the
	// debug mode, so that the key derived from it is of the models chosen.
	act := s.debugged(ctx, v1.DebugActionKind_DEBUG_ACTION_KIND_CHECK, req.GetAttemptId())
	defer act.End()
	profileID, attemptID := req.GetProfileId(), req.GetAttemptId()
	key := rpc.Key{
		Method:   apiv1connect.AttemptServiceReadStatementProcedure,
		GoingFor: s.paperGoingFor,
		Repeat: func(ctx context.Context, tx pgx.Tx, record store.KeyRecord) (proto.Message, error) {
			answer := &v1.ReadStatementResponse{}
			if err := proto.Unmarshal(record.Response, answer); err != nil {
				return nil, err
			}
			h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
			if refusal, ok := rpc.Kept(err); ok {
				// The close is the next request's to write: a repeat answers from the record.
				return nil, refusal
			}
			if err != nil {
				return nil, err
			}
			statements, found, err := statedOn(h.row)
			if found {
				answer = &v1.ReadStatementResponse{Statements: statements, Repeated: true}
			}
			return answer, err
		},
	}
	route := act.Route(routeOf(category, req.GetOwnKey()))
	answer, err := rpc.Across(ctx, s.db, key, req, rpc.Phases[recognising, *v1.ReadStatementResponse]{
		Prepare: func(tx pgx.Tx, _ string) (recognising, store.KeyTarget, error) {
			h, err := s.holdAny(ctx, tx, profileID, attemptID, s.clock())
			target := store.KeyTarget{ProfileID: profileID, AttemptID: h.row.ID}
			switch {
			case err != nil:
				return recognising{}, target, err
			case !h.row.Paper:
				return recognising{}, target, rpc.Malformed("attempt_id")
			case !photoTaken(req.GetStatement()):
				return recognising{}, target, rpc.Malformed("statement")
			}
			r := recognising{attemptID: h.row.ID, task: tutor.PaperTask{Route: route, Statement: req.GetStatement()}}
			statements, found, err := statedOn(h.row)
			if err != nil || found {
				r.ready = &v1.ReadStatementResponse{Statements: statements, Repeated: true}
				return r, target, err
			}
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*finishReserve {
				return recognising{}, target, connect.NewError(connect.CodeDeadlineExceeded, "too little time to read the statement")
			}
			day, err := s.familyDay(ctx, tx, route, req.ClockOffsetMinutes)
			if err != nil {
				return recognising{}, target, err
			}
			if r.reserve, err = s.reserveFor(ctx, tx, profileID, route, tutor.CallStatement, 1, day); err == nil && r.reserve.refused != nil {
				err = refusal(ctx, r.reserve.refused)
			}
			return r, target, err
		},
		Call: func(ctx context.Context, r recognising) recognising {
			return s.recognise(ctx, r, req.GetOwnKey().GetKey())
		},
		Finish: func(tx pgx.Tx, r recognising) (*v1.ReadStatementResponse, store.KeyTarget, error) {
			return s.recognised(ctx, tx, profileID, r)
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

// recognise calls the reading of the statement of r until finishReserve before
// the request's deadline, and settles its reserve; own is as sheetReader has
// it. An attempt that answered already calls nothing. The crop goes no further
// than the call.
func (s *Service) recognise(ctx context.Context, r recognising, own string) recognising {
	if r.ready != nil {
		return r
	}
	ctx, cancel := finishing(ctx)
	defer cancel()
	start := time.Now()
	r.read = s.paper.ReadStatement(ctx, r.task, own)
	r.task.Statement = nil
	noteCall(ctx, r.read.Outcome, time.Since(start))
	s.afterCall(ctx, r.reserve, r.read.Outcome)
	return r
}

// recognised writes what came of the call of r and answers it, on the attempt
// held again: a reading the attempt keeps, unless a Check or another press kept
// a key or a reading meanwhile, whose statements are the answer then, since
// the key is derived from what is kept; or the outcome of a call that gave
// none, with nothing kept.
func (s *Service) recognised(ctx context.Context, tx pgx.Tx, profileID string, r recognising) (*v1.ReadStatementResponse, store.KeyTarget, error) {
	answer := r.ready
	if answer == nil {
		row, err := s.holdPaper(ctx, tx, profileID, r.attemptID, s.clock())
		if err != nil {
			return nil, store.KeyTarget{}, err
		}
		switch statements, found, err := statedOn(row); {
		case err != nil:
			return nil, store.KeyTarget{}, err
		case r.read.Outcome.Kind != tutor.Answered:
			answer = &v1.ReadStatementResponse{Outcome: outcomeOf(r.read.Outcome, r.task.Route.AI)}
		case found:
			answer = &v1.ReadStatementResponse{Statements: statements}
		default:
			reading := readingOf(r.read.Tasks)
			// A reading of the contract always marshals.
			kept, _ := proto.Marshal(reading)
			if err := store.SetPaperReading(ctx, tx, profileID, row.ID, kept); err != nil {
				return nil, store.KeyTarget{}, err
			}
			answer = &v1.ReadStatementResponse{Statements: readStatements(reading)}
		}
	}
	// An answer of the contract always marshals; the record keeps no statement.
	written, _ := proto.Marshal(&v1.ReadStatementResponse{Outcome: answer.GetOutcome()})
	return answer, store.KeyTarget{ProfileID: profileID, AttemptID: r.attemptID, Response: written}, nil
}

// statedOn is the statements of the tasks of what the attempt keeps of its
// statement, the key's or else the reading's, and whether it keeps either.
func statedOn(row store.Attempt) ([]string, bool, error) {
	k, err := keptKey(row)
	if err != nil || k != nil {
		return statementsOf(k), k != nil, err
	}
	reading, err := keptReading(row)
	if err != nil || reading == nil {
		return nil, false, err
	}
	return readStatements(reading), true, nil
}

// keptReading is the reading of the statement the attempt keeps, or none, read
// as keptKey reads the key: one of a shape this build knows not is none, read
// again, not a reading of no task, whose key every Check would take for
// CAUSE_NO_KEY.
func keptReading(row store.Attempt) (*paperv1.Reading, error) {
	if row.PaperReading == nil {
		return nil, nil
	}
	r := &paperv1.Reading{}
	if err := proto.Unmarshal(row.PaperReading, r); err != nil {
		return nil, err
	}
	if len(r.GetTasks()) == 0 && len(r.ProtoReflect().GetUnknown()) > 0 {
		return nil, nil
	}
	return r, nil
}

// readingOf is the reading the attempt keeps of the tasks a call read.
func readingOf(tasks []tutor.StatementTask) *paperv1.Reading {
	r := &paperv1.Reading{}
	for _, t := range tasks {
		r.Tasks = append(r.Tasks, &paperv1.ReadTask{Statement: t.Statement, Formula: t.Formula})
	}
	return r
}

// readTasks are the tasks of a kept reading, as the call read them.
func readTasks(r *paperv1.Reading) []tutor.StatementTask {
	out := make([]tutor.StatementTask, len(r.GetTasks()))
	for i, t := range r.GetTasks() {
		out[i] = tutor.StatementTask{Statement: t.GetStatement(), Formula: t.GetFormula()}
	}
	return out
}

// readStatements are the statements of a reading's tasks, in order.
func readStatements(r *paperv1.Reading) []string {
	out := make([]string, len(r.GetTasks()))
	for i, t := range r.GetTasks() {
		out[i] = t.GetStatement()
	}
	return out
}
