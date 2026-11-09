package account_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/idem"
	"github.com/altnovatrail/api/internal/pin"
	"github.com/altnovatrail/api/internal/rpc"
)

// The PINs of the tests: synthetic, and long enough that no hash holds them by
// chance.
const (
	thePIN  = "135792"
	wrong   = "864200"
	another = "246813"
)

// owner is an account of the world, made by its invite code: the context's calls
// carry its token.
func (w *world) owner(t *testing.T, uid string) context.Context {
	t.Helper()

	ctx := w.signIn(t, uid)
	if _, err := create(ctx, w, w.code(t)); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// keys gives each action a key of its own, as app does.
type keys struct {
	mu sync.Mutex
	n  int
}

func (k *keys) next() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.n++
	return fmt.Sprintf("key-%d", k.n)
}

func (w *world) setPin(t *testing.T, ctx context.Context, k *keys, given string) {
	t.Helper()
	if _, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: given, IdempotencyKey: k.next()}); err != nil {
		t.Fatal(err)
	}
}

func (w *world) verify(ctx context.Context, key, given string) (*v1.PinCheck, error) {
	res, err := w.client.VerifyPin(ctx, &v1.VerifyPinRequest{Pin: given, IdempotencyKey: key})
	return res.GetCheck(), err
}

func TestAPINIsSetOnceAndShows(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	key := k.next()
	if _, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: key}); err != nil {
		t.Fatal(err)
	}
	// Its repeat is the same answer; another setting is refused.
	if _, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: key}); err != nil {
		t.Errorf("the repeat of SetPin = %v, want its answer", err)
	}
	_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: another, IdempotencyKey: k.next()})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_PIN_ALREADY_SET)

	got, err := w.client.GetAccount(ctx, &v1.GetAccountRequest{})
	if err != nil || !got.GetAccount().GetPinSet() {
		t.Errorf("GetAccount = %v, %v; want the PIN set", got, err)
	}
	if check, err := w.verify(ctx, k.next(), thePIN); err != nil || !check.GetMatches() {
		t.Errorf("VerifyPin of the PIN set = %v, %v; want a match", check, err)
	}
}

func TestNoPINIsNotChecked(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	_, err := w.verify(ctx, k.next(), thePIN)
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_PIN)
	_, err = w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: thePIN, NewPin: another, IdempotencyKey: k.next()})
	refusedAs(t, err, connect.CodeFailedPrecondition, v1.Cause_CAUSE_NO_PIN)
}

func TestTheFifthWrongPINClosesTheInputUntilAQuarterOfAnHourAfterIt(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	for left := pin.MaxFailures - 1; left > 0; left-- {
		w.clock.advance(time.Minute)
		check, err := w.verify(ctx, k.next(), wrong)
		if err != nil || check.GetMatches() || check.GetAttemptsLeft() != int32(left) || check.GetLockedUntil() != nil {
			t.Fatalf("a wrong PIN = %v, %v; want %d left", check, err, left)
		}
	}
	w.clock.advance(time.Minute)
	fifth := w.clock.Now()
	check, err := w.verify(ctx, k.next(), wrong)
	if err != nil || check.GetAttemptsLeft() != 0 || !check.GetLockedUntil().AsTime().Equal(fifth.Add(pin.LockFor).Truncate(time.Microsecond)) {
		t.Fatalf("the fifth wrong PIN = %v, %v; want the input closed until %v", check, err, fifth.Add(pin.LockFor))
	}
	// Closed, the right PIN is refused too, and so is a change; the refusal says
	// when it opens.
	w.clock.advance(pin.LockFor - time.Second)
	_, err = w.verify(ctx, k.next(), thePIN)
	r := refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PIN_LOCKED)
	if !r.GetLockedUntil().AsTime().Equal(check.GetLockedUntil().AsTime()) {
		t.Errorf("the refusal's locked_until = %v, want %v", r.GetLockedUntil().AsTime(), check.GetLockedUntil().AsTime())
	}
	_, err = w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: thePIN, NewPin: another, IdempotencyKey: k.next()})
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PIN_LOCKED)
	w.clock.advance(time.Second)
	if check, err := w.verify(ctx, k.next(), thePIN); err != nil || !check.GetMatches() {
		t.Errorf("the right PIN once the input opens = %v, %v; want a match", check, err)
	}
}

func TestARepeatOfAWrongPINCountsItOnce(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	key := k.next()
	first, err := w.verify(ctx, key, wrong)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := w.verify(ctx, key, wrong)
		if err != nil || !proto.Equal(again, first) {
			t.Fatalf("the repeat = %v, %v; want the first answer, %v", again, err, first)
		}
	}
	if check, err := w.verify(ctx, k.next(), wrong); err != nil || check.GetAttemptsLeft() != pin.MaxFailures-2 {
		t.Errorf("the next wrong PIN = %v, %v; want %d left: the repeats counted nothing", check, err, pin.MaxFailures-2)
	}
}

func TestAChangeIsByThePINBeforeAndItsRepeatIsNoWrongPIN(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)

	res, err := w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: wrong, NewPin: another, IdempotencyKey: k.next()})
	if err != nil || res.GetCheck().GetMatches() || res.GetCheck().GetAttemptsLeft() != pin.MaxFailures-1 {
		t.Fatalf("a change by a wrong PIN = %v, %v; want it counted and nothing changed", res, err)
	}
	key := k.next()
	change := &v1.ChangePinRequest{Pin: thePIN, NewPin: another, IdempotencyKey: key}
	if res, err := w.client.ChangePin(ctx, change); err != nil || !res.GetCheck().GetMatches() {
		t.Fatalf("a change by the PIN = %v, %v; want a match", res, err)
	}
	// The PIN before no longer matches, so a repeat read anew would be wrong; the
	// record gives the first answer.
	if res, err := w.client.ChangePin(ctx, change); err != nil || !res.GetCheck().GetMatches() {
		t.Errorf("the repeat of the change = %v, %v; want its match", res, err)
	}
	if check, err := w.verify(ctx, k.next(), another); err != nil || !check.GetMatches() {
		t.Errorf("the new PIN = %v, %v; want a match", check, err)
	}
}

func TestAForgottenPINIsResetByAFreshSignInAndOpensTheInput(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	// With no PIN, a reset sets one.
	if _, err := w.client.ResetPin(ctx, &v1.ResetPinRequest{NewPin: thePIN, IdempotencyKey: k.next()}); err != nil {
		t.Fatal(err)
	}
	for range pin.MaxFailures {
		if _, err := w.verify(ctx, k.next(), wrong); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.client.ResetPin(ctx, &v1.ResetPinRequest{NewPin: another, IdempotencyKey: k.next()}); err != nil {
		t.Fatal(err)
	}
	if check, err := w.verify(ctx, k.next(), another); err != nil || !check.GetMatches() {
		t.Errorf("the PIN reset = %v, %v; want the input open and a match", check, err)
	}

	// A sign-in older than five minutes is refused, last.
	stale := w.signIn(t, "uid-owner")
	w.signin.Accept("token-uid-owner", authn.Token{UID: "uid-owner", AuthTime: time.Now().Add(-10 * time.Minute)})
	_, err := w.client.ResetPin(stale, &v1.ResetPinRequest{NewPin: thePIN, IdempotencyKey: k.next()})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_FRESH_SIGN_IN_NEEDED)
}

func TestAKeyOfAnotherRequestIsUsedAlready(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	key := k.next()
	if _, err := w.verify(ctx, key, thePIN); err != nil {
		t.Fatal(err)
	}
	_, err := w.verify(ctx, key, wrong)
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	_, err = w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: thePIN, NewPin: another, IdempotencyKey: key})
	refusedAs(t, err, connect.CodeInvalidArgument, v1.Cause_CAUSE_KEY_ALREADY_USED)
	// The key of another account is another key.
	other := w.owner(t, "uid-other")
	if _, err := w.client.SetPin(other, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: key}); err != nil {
		t.Errorf("SetPin of another account under the same key = %v, want nil", err)
	}
}

func TestARequestStillGoingIsToldSo(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	// A record that waits for its answer, as one of epic E3 does while AI works.
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO idempotency_keys (account_id, key, method, fingerprint, state)
		SELECT id, 'going', '/novatrail.api.v1.AccountService/VerifyPin', $1, 'going' FROM accounts`,
		fingerprintOf(&v1.VerifyPinRequest{Pin: thePIN})); err != nil {
		t.Fatal(err)
	}
	_, err := w.verify(ctx, "going", thePIN)
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_STILL_GOING)
}

func TestRepeatsAtOnceAreOneAction(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	checks := make([]*v1.PinCheck, 4)
	var wg sync.WaitGroup
	for i := range checks {
		wg.Go(func() {
			check, err := w.verify(as(t, "uid-owner"), "at-once", wrong)
			if err != nil {
				t.Error(err)
			}
			checks[i] = check
		})
	}
	wg.Wait()
	for _, check := range checks {
		if !proto.Equal(check, checks[0]) || check.GetAttemptsLeft() != pin.MaxFailures-1 {
			t.Errorf("checks = %v; want one wrong PIN, counted once", checks)
		}
	}
}

func TestTwoDevicesCountEveryWrongPIN(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	left := make(chan int32, pin.MaxFailures)
	var wg sync.WaitGroup
	for range pin.MaxFailures {
		wg.Go(func() {
			check, err := w.verify(as(t, "uid-owner"), k.next(), wrong)
			if err != nil {
				t.Error(err)
				return
			}
			left <- check.GetAttemptsLeft()
		})
	}
	wg.Wait()
	close(left)
	seen := map[int32]bool{}
	for l := range left {
		seen[l] = true
	}
	if len(seen) != pin.MaxFailures {
		t.Errorf("attempts left = %v; want each of 0 to %d once", seen, pin.MaxFailures-1)
	}
	_, err := w.verify(ctx, k.next(), thePIN)
	refusedAs(t, err, connect.CodeResourceExhausted, v1.Cause_CAUSE_PIN_LOCKED)
}

func TestARequestOfThePINOfNoFormIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	for name, call := range map[string]func() error{
		"a short PIN to set": func() error {
			_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: "12", IdempotencyKey: "k"})
			return err
		},
		"a PIN of letters": func() error {
			_, err := w.client.VerifyPin(ctx, &v1.VerifyPinRequest{Pin: "abcd", IdempotencyKey: "k"})
			return err
		},
		"a change's PIN before": func() error {
			_, err := w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: "1", NewPin: thePIN, IdempotencyKey: "k"})
			return err
		},
		"a change's new PIN": func() error {
			_, err := w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: thePIN, NewPin: "1234567", IdempotencyKey: "k"})
			return err
		},
		"a reset's new PIN": func() error {
			_, err := w.client.ResetPin(ctx, &v1.ResetPinRequest{NewPin: "", IdempotencyKey: "k"})
			return err
		},
		"no key": func() error {
			_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN})
			return err
		},
		"a key too long": func() error {
			_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: strings.Repeat("k", 129)})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: nil, want invalid_argument", name)
		} else if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
}

func TestThePINOfAnAccountGoneIsGoneToo(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	if _, err := w.pool.Exec(t.Context(), `UPDATE accounts SET deleting_at = now()`); err != nil {
		t.Fatal(err)
	}
	_, err := w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: "k"})
	refusedAs(t, err, connect.CodeUnauthenticated, v1.Cause_CAUSE_ACCOUNT_GONE)
}

func TestNeitherThePINNorItsHashIsInTheLogOrTheDatabaseInTheClear(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.owner(t, "uid-owner")
	k := &keys{}
	w.setPin(t, ctx, k, thePIN)
	if _, err := w.verify(ctx, k.next(), wrong); err != nil {
		t.Fatal(err)
	}
	if _, err := w.client.ChangePin(ctx, &v1.ChangePinRequest{Pin: thePIN, NewPin: another, IdempotencyKey: k.next()}); err != nil {
		t.Fatal(err)
	}
	var rows string
	if err := w.pool.QueryRow(t.Context(), `SELECT (SELECT json_agg(a)::text FROM accounts a) ||
		(SELECT json_agg(k)::text FROM idempotency_keys k)`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := w.pool.QueryRow(t.Context(), `SELECT pin_hash FROM accounts`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	log := w.log.String()
	for _, secret := range []string{thePIN, wrong, another} {
		if strings.Contains(rows, secret) {
			t.Errorf("the database holds the PIN %s in the clear", secret)
		}
		if strings.Contains(log, secret) {
			t.Errorf("the log holds the PIN %s", secret)
		}
	}
	if strings.Contains(log, hash) || strings.Contains(log, "argon2id") || !strings.Contains(log, "VerifyPin") {
		t.Errorf("the log %s holds a hash, or no line of the requests", log)
	}
}

func TestAFailedDatabaseLeavesThePINAsItWas(t *testing.T) {
	t.Parallel()

	broken, err := pgxpool.New(t.Context(), "postgres://u@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	w := newWorld(t, func(*pgxpool.Pool) rpc.DB { return broken })
	ctx := w.signIn(t, "uid-owner")
	if _, err := w.pool.Exec(t.Context(), `INSERT INTO accounts (auth_uid) VALUES ('uid-owner')`); err != nil {
		t.Fatal(err)
	}
	_, err = w.client.SetPin(ctx, &v1.SetPinRequest{Pin: thePIN, IdempotencyKey: "k"})
	refusedAs(t, err, connect.CodeUnavailable, v1.Cause_CAUSE_UNSPECIFIED)
}

// fingerprintOf is the fingerprint the handlers record of req.
func fingerprintOf(req proto.Message) string { return idem.Fingerprint(req, true) }
