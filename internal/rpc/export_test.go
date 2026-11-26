package rpc

import (
	"context"
	"time"

	"github.com/altnovatrail/api/internal/store"
)

// WithCaller is ctx with the caller the checks would have put in it.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// WithGoing is ctx as Across gives its call: with the record going of the
// account's key made at made, on db.
func WithGoing(ctx context.Context, db store.Beginner, accountID, key string, made time.Time) context.Context {
	return context.WithValue(ctx, goingKey{}, going{db: db, accountID: accountID, key: key, made: made})
}
