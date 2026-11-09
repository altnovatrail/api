package rpc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
)

// stream is a ServerStream whose message is received at once, or fails.
type stream struct{ err error }

func (s *stream) Receive(any) error { return s.err }

func (*stream) SendHeaders() error { return nil }
func (*stream) Send(any) error     { return nil }

// A method the table lacks is served by no one, whatever the server registered.
func TestAMethodWithNoRuleIsUnimplemented(t *testing.T) {
	t.Parallel()

	c := &checks{}
	err := c.intercept(nil)(t.Context(), connect.Spec{Procedure: "/novatrail.api.v1.AccountService/Unknown"}, &stream{})
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("a method with no rule = %v, want unimplemented", err)
	}
}

// A message that cannot be received is that error, and one that names no
// profile, on a method with a right, has none to be let to.
func TestAMessageWithNoProfileIsNone(t *testing.T) {
	t.Parallel()

	failed := &checked{ServerStream: &stream{err: io.ErrUnexpectedEOF}, ctx: t.Context()}
	if err := failed.Receive(&v1.UpdateProfileRequest{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Receive of a broken stream = %v, want its error", err)
	}
	unnamed := &checked{ServerStream: &stream{}, ctx: t.Context(), rule: toManage}
	err := unnamed.Receive(&v1.GetAccountRequest{})
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeNotFound {
		t.Errorf("Receive of a message with no profile = %v, want no profile", err)
	}
}

// A refusal outside a request's line is noted nowhere, and is still a refusal.
func TestARefusalOutsideARequestIsStillOne(t *testing.T) {
	t.Parallel()

	if err := Refuse(context.Background(), connect.CodeNotFound, v1.Cause_CAUSE_NO_PROFILE); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("Refuse = %v, want not_found", err)
	}
}

// A call with no server's call info — run past Connect's transport — has no
// token to be checked, and is refused as one with none.
func TestACallWithNoCallInfoIsUnauthenticated(t *testing.T) {
	t.Parallel()

	c := &checks{}
	err := c.intercept(nil)(t.Context(), connect.Spec{Procedure: "/novatrail.api.v1.AccountService/GetAccount"}, &stream{})
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("a call with no call info = %v, want unauthenticated", err)
	}
}

// No time limit given is the default one, not none and not one already out.
func TestNoTimeLimitIsTheDefault(t *testing.T) {
	t.Parallel()

	var left time.Duration
	_ = limited(0)(func(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
		deadline, ok := ctx.Deadline()
		if ok {
			left = time.Until(deadline)
		}
		return nil
	})(t.Context(), connect.Spec{}, &stream{})
	if left < 29*time.Second || left > 30*time.Second {
		t.Errorf("the time left under no limit given = %v, want the default 30s", left)
	}
}
