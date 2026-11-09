// Package invite is the invite codes of the closed slice (R04, R34): how a code
// is made, how one given back is read, and the SHA-256 that is all the database
// keeps of it.
//
// A code is 16 symbols of Crockford's base32, 80 bits from crypto/rand, written
// in four groups of four: 0123-4567-89AB-CDEF. It is read as Crockford reads it,
// so that what a person types or pastes back is taken: case, dashes and white
// space — a line's end, a space of no break — do not count, O is read as 0, and
// I and L as 1. The hash is of the code so read, so that every way of writing
// it has one.
package invite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"slices"
	"strings"
	"unicode"
)

// alphabet is Crockford's base32: the digits and the letters but I, L, O and U.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// The shape of a code.
const (
	// Symbols is how many symbols of the alphabet a code has.
	Symbols = 16
	// group is how many of them are written together.
	group = 4
	// entropy is how many random bytes a code holds: 5 bits a symbol.
	entropy = Symbols * 5 / 8
)

var encoding = base32.NewEncoding(alphabet).WithPadding(base32.NoPadding)

// The tariffs a code is made with (R11; architecture 3.6). A code's tariff
// passes to the account that redeems it. Their caps come with the quota (Д11).
const (
	// ClosedSlice is the tariff of the closed slice, a code's by default.
	ClosedSlice = "closed_slice"
)

// Tariffs is every tariff a code may be made with.
var Tariffs = []string{ClosedSlice}

// Known says whether tariff is one of Tariffs.
func Known(tariff string) bool { return slices.Contains(Tariffs, tariff) }

// Hash is what the database keeps of a code: its SHA-256.
type Hash [sha256.Size]byte

// New is a new code, as it is written for the person who gets it, and its hash.
// crypto/rand never fails on the platforms api runs on.
func New() (string, Hash) {
	random := make([]byte, entropy)
	_, _ = rand.Read(random)
	symbols := encoding.EncodeToString(random)
	var written strings.Builder
	for i := 0; i < Symbols; i += group {
		if i > 0 {
			written.WriteByte('-')
		}
		written.WriteString(symbols[i : i+group])
	}
	return written.String(), sha256.Sum256([]byte(symbols))
}

// Read is the hash of a code as it was given back, and false for what is no
// code: a symbol out of the alphabet, or too few or too many of them.
func Read(given string) (Hash, bool) {
	var symbols strings.Builder
	for _, r := range strings.ToUpper(given) {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(alphabet, r) {
			return Hash{}, false
		}
		symbols.WriteRune(r)
		if symbols.Len() > Symbols {
			return Hash{}, false
		}
	}
	if symbols.Len() != Symbols {
		return Hash{}, false
	}
	return sha256.Sum256([]byte(symbols.String())), true
}
