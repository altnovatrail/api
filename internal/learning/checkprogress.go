package learning

import (
	"context"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"

	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// GetCheckProgress says how far the Check of a task from paper still going on
// the profile's attempt has got, by the record going of its SubmitWork under
// the caller's account: the place of the model that now reads, from 0, and the
// length of the call's list, once the Check has gone on to a later model, and
// both 0 before. A Check not going, or done, or one older than the longest a
// submission may run, is going false. It reads one row and writes nothing, so
// that app may ask it every few seconds.
func (a *AttemptService) GetCheckProgress(ctx context.Context, req *v1.GetCheckProgressRequest) (*v1.GetCheckProgressResponse, error) {
	s := a.s
	if !store.CanonicalID(req.GetAttemptId()) {
		return nil, rpc.Malformed("attempt_id")
	}
	caller, _ := rpc.CallerFrom(ctx)
	p, err := store.CheckProgress(ctx, s.db, caller.AccountID, req.GetProfileId(), req.GetAttemptId(),
		apiv1connect.AttemptServiceSubmitWorkProcedure, s.paperGoingFor)
	if err != nil {
		return nil, refused(ctx, err)
	}
	return &v1.GetCheckProgressResponse{Going: p.Going, Model: int32(p.Model), Models: int32(p.Models)}, nil
}
