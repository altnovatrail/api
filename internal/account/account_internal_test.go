package account

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect/v2"

	"github.com/altnovatrail/api/internal/store"
)

// A refusal of the store that only a race reaches — the account marked or
// deleted after the checks let the request through, or made by a request beside
// this one — is still the contract's.
func TestARefusalOfTheStoreAfterTheChecksIsTheContracts(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]connect.Code{
		store.ErrAccountDeleting:                           connect.CodeUnauthenticated,
		fmt.Errorf("wrapped: %w", store.ErrNoAccount):      connect.CodeUnauthenticated,
		store.ErrAccountExists:                             connect.CodeAlreadyExists,
		store.ErrCodeNotAccepted:                           connect.CodeInvalidArgument,
		fmt.Errorf("store: read the account: %w", errBoom): connect.CodeUnavailable,
	} {
		if got := connect.CodeOf(refused(t.Context(), err)); got != want {
			t.Errorf("refused(%v) = %v, want %v", err, got, want)
		}
	}
}

var errBoom = errors.New("boom")
