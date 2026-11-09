// Package argon is api's slow hash, argon2id, for what has too few values for a
// fast one: a PIN, and the fingerprint of a request that carries a PIN or an age
// (R35). A hash is written as the PHC string format writes it,
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>, so that it carries its own
// parameters, and raising them later needs no migration: a hash made before is
// still checked by the parameters it names.
package argon

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The parameters: the minimum OWASP's Password Storage Cheat Sheet recommends,
// "m=19456 (19 MiB), t=2, p=1" (read 2026-11-09; R35).
const (
	memoryKiB = 19456
	passes    = 2
	lanes     = 1
	saltBytes = 16
	hashBytes = 32
)

var b64 = base64.RawStdEncoding

// Hash is the PHC string of argon2id of secret, with a salt of its own.
// crypto/rand never fails on the platforms api runs on.
func Hash(secret []byte) string {
	salt := make([]byte, saltBytes)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey(secret, salt, passes, memoryKiB, lanes, hashBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, passes, lanes, b64.EncodeToString(salt), b64.EncodeToString(sum))
}

// Matches says whether secret is what phc is the hash of, by the parameters phc
// names. A string that is no argon2id hash of this version matches nothing.
func Matches(secret []byte, phc string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false
	}
	var memory, time uint32
	var threads uint8
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil || n != 3 ||
		fmt.Sprintf("m=%d,t=%d,p=%d", memory, time, threads) != parts[3] || memory == 0 || time == 0 || threads == 0 {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey(secret, salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Is says whether s is written as a hash of this package: what tells a slow
// fingerprint from a fast one.
func Is(s string) bool { return strings.HasPrefix(s, "$argon2id$") }
