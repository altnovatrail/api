package account_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
)

// The stored works of WorkService are not built yet: each method answers
// unimplemented, behind the checks of its rule, which tell a profile that is
// none so first (R69).
func TestTheStoredWorksAreNotBuiltYet(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.allow(t, "US")
	ctx := w.owner(t, "uid-owner")
	p := w.ownerProfile(t, ctx).GetProfileId()
	const none = "00000000-0000-0000-0000-000000000000"
	learning := v1.Action_ACTION_LEARNING
	for name, call := range map[string]func(context.Context, string) error{
		"PutWork": func(ctx context.Context, profile string) error {
			_, err := w.work.PutWork(ctx, &v1.PutWorkRequest{ProfileId: profile})
			return err
		},
		"ListWorks": func(ctx context.Context, profile string) error {
			_, err := w.work.ListWorks(ctx, &v1.ListWorksRequest{ProfileId: profile, Action: learning})
			return err
		},
		"GetWork": func(ctx context.Context, profile string) error {
			_, err := w.work.GetWork(ctx, &v1.GetWorkRequest{ProfileId: profile, Action: learning})
			return err
		},
		"DeleteWork": func(ctx context.Context, profile string) error {
			_, err := w.work.DeleteWork(ctx, &v1.DeleteWorkRequest{ProfileId: profile, Action: learning})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			refusedAs(t, call(as(t, "uid-owner"), p), connect.CodeUnimplemented, v1.Cause_CAUSE_UNSPECIFIED)
			refusedAs(t, call(as(t, "uid-owner"), none), connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE)
		})
	}
}
