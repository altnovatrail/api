package rpc

import "context"

// WithCaller is ctx with the caller the checks would have put in it.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}
