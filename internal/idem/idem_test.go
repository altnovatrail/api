package idem_test

import (
	"strings"
	"testing"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"

	"github.com/altnovatrail/api/internal/idem"
)

func TestAKeyIsPresentAndShort(t *testing.T) {
	t.Parallel()

	if !idem.ValidKey("k") || !idem.ValidKey(strings.Repeat("k", idem.MaxKey)) {
		t.Error("a key of 1 or of MaxKey bytes is refused")
	}
	if idem.ValidKey("") || idem.ValidKey(strings.Repeat("k", idem.MaxKey+1)) || idem.ValidKey("a\x00b") {
		t.Error("an empty key, a longer one or one with a NUL is taken")
	}
}

func TestARepeatHasTheFingerprintOfItsFirstWhateverItsKey(t *testing.T) {
	t.Parallel()

	for _, slow := range []bool{false, true} {
		first := &v1.VerifyPinRequest{Pin: "2468", IdempotencyKey: "first"}
		fingerprint := idem.Fingerprint(first, slow)
		if !idem.Same(&v1.VerifyPinRequest{Pin: "2468", IdempotencyKey: "other"}, fingerprint) {
			t.Errorf("slow %v: the same request under another key is another request", slow)
		}
		if idem.Same(&v1.VerifyPinRequest{Pin: "1357", IdempotencyKey: "first"}, fingerprint) {
			t.Errorf("slow %v: another PIN is the same request", slow)
		}
		if idem.Same(&v1.SetPinRequest{Pin: "1357", IdempotencyKey: "first"}, fingerprint) {
			t.Errorf("slow %v: another message is the same request", slow)
		}
		if first.GetIdempotencyKey() != "first" {
			t.Errorf("slow %v: the fingerprint cleared the request's own key", slow)
		}
	}
}

// A slow fingerprint holds nothing a fast hash would give the PIN back from.
func TestAFingerprintOfAPINIsSlow(t *testing.T) {
	t.Parallel()

	slow := idem.Fingerprint(&v1.VerifyPinRequest{Pin: "2468", IdempotencyKey: "k"}, true)
	fast := idem.Fingerprint(&v1.VerifyPinRequest{Pin: "2468", IdempotencyKey: "k"}, false)
	if !strings.HasPrefix(slow, "$argon2id$") || !strings.HasPrefix(fast, "sha256:") || strings.Contains(slow, "2468") {
		t.Errorf("fingerprints = %q, %q; want argon2id and SHA-256, neither with the PIN", slow, fast)
	}
	// A message with no key of its own is taken whole.
	if !idem.Same(&v1.GetAccountRequest{}, idem.Fingerprint(&v1.GetAccountRequest{}, false)) {
		t.Error("a message with no key is not the same as itself")
	}
}

// A fingerprint holds nothing of the learner's own key: a repeat with another
// key at the provider is the same request, and the request keeps its own.
func TestAFingerprintLeavesTheOwnKeyOut(t *testing.T) {
	t.Parallel()

	first := &v1.SubmitWorkRequest{AttemptId: "a", IdempotencyKey: "k", OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "secret"}}
	fingerprint := idem.Fingerprint(first, false)
	if !idem.Same(&v1.SubmitWorkRequest{AttemptId: "a", IdempotencyKey: "other"}, fingerprint) {
		t.Error("the same request without its own key is another request")
	}
	if idem.Same(&v1.SubmitWorkRequest{AttemptId: "b", IdempotencyKey: "k"}, fingerprint) {
		t.Error("another attempt is the same request")
	}
	if first.GetOwnKey().GetKey() != "secret" {
		t.Error("the fingerprint cleared the request's own key")
	}
}
