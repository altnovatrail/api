package argon_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/altnovatrail/api/internal/argon"
)

func TestAHashIsCheckedByItsOwnParameters(t *testing.T) {
	t.Parallel()

	hash := argon.Hash([]byte("2468"))
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") || !argon.Is(hash) {
		t.Fatalf("Hash = %q, want argon2id with OWASP's minimum, in the PHC format", hash)
	}
	if !argon.Matches([]byte("2468"), hash) {
		t.Error("the secret does not match its hash")
	}
	if argon.Matches([]byte("2469"), hash) {
		t.Error("another secret matches the hash")
	}
	if again := argon.Hash([]byte("2468")); again == hash {
		t.Error("two hashes of one secret are the same: no salt of their own")
	}
	// A hash with other parameters, as one made before they are raised, still
	// matches by its own.
	salt := []byte("saltsaltsaltsalt")
	lighter := "$argon2id$v=19$m=8,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(argon2.IDKey([]byte("2468"), salt, 1, 8, 1, 32))
	if !argon.Matches([]byte("2468"), lighter) || argon.Matches([]byte("2469"), lighter) {
		t.Error("a hash of other parameters is not checked by them")
	}
}

func TestWhatIsNoHashMatchesNothing(t *testing.T) {
	t.Parallel()

	good := argon.Hash([]byte("2468"))
	parts := strings.Split(good, "$")
	for name, phc := range map[string]string{
		"empty":            "",
		"a fast one":       "sha256:00",
		"argon2i":          strings.Replace(good, "argon2id", "argon2i", 1),
		"another version":  strings.Replace(good, "v=19", "v=16", 1),
		"no parameters":    strings.Join([]string{"", parts[1], parts[2], "", parts[4], parts[5]}, "$"),
		"a zero parameter": strings.Replace(good, "t=2", "t=0", 1),
		"a padded one":     strings.Replace(good, "t=2", "t=02", 1),
		"a bad salt":       strings.Join([]string{"", parts[1], parts[2], parts[3], "!", parts[5]}, "$"),
		"no salt":          strings.Join([]string{"", parts[1], parts[2], parts[3], "", parts[5]}, "$"),
		"a bad hash":       strings.Join([]string{"", parts[1], parts[2], parts[3], parts[4], "!"}, "$"),
		"no hash":          strings.Join([]string{"", parts[1], parts[2], parts[3], parts[4], ""}, "$"),
		"a part too many":  good + "$",
		"text before":      "x" + good,
	} {
		if argon.Matches([]byte("2468"), phc) {
			t.Errorf("Matches of %s = true", name)
		}
	}
}
