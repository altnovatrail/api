package invite_test

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/altnovatrail/api/internal/invite"
)

func TestANewCodeIsWrittenInGroupsAndReadBackToItsHash(t *testing.T) {
	t.Parallel()

	shape := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`)
	seen := map[string]bool{}
	for range 1000 {
		code, hash := invite.New()
		if !shape.MatchString(code) {
			t.Fatalf("New = %q, want four groups of four symbols of Crockford's base32", code)
		}
		if seen[code] {
			t.Fatalf("New gave %q twice", code)
		}
		seen[code] = true
		read, ok := invite.Read(code)
		if !ok || read != hash {
			t.Fatalf("Read(%q) = %x, %v; want the hash New gave, %x", code, read, ok, hash)
		}
	}
}

func TestACodeIsReadAsAPersonTypesItBack(t *testing.T) {
	t.Parallel()

	want := invite.Hash(sha256.Sum256([]byte("0123456789ABCDEF")))
	for _, given := range []string{
		"0123-4567-89AB-CDEF",
		"0123456789ABCDEF",
		"0123-4567-89ab-cdef",
		" 0123 4567\t89AB CDEF ",
		"O123-4567-89AB-CDEF", // O is 0
		"0I23-4567-89AB-CDEF", // I is 1
		"0l23-4567-89AB-CDEF", // l is 1
		"0123--4567-89AB-CDEF",
		"0123-4567-89AB-CDEF\n",              // as the terminal printed it
		"0123-4567-89AB-CDEF\r\n",            // or a mail
		"0123\u00a04567\u00a089AB\u00a0CDEF", // with spaces of no break
	} {
		if got, ok := invite.Read(given); !ok || got != want {
			t.Errorf("Read(%q) = %x, %v; want %x", given, got, ok, want)
		}
	}
}

func TestWhatIsNoCodeIsNotRead(t *testing.T) {
	t.Parallel()

	for _, given := range []string{
		"",
		"0123-4567-89AB-CDE",    // one short
		"0123-4567-89AB-CDEF0",  // one more
		"0123-4567-89AB-CDEU",   // U is no symbol
		"0123-4567-89AB-CDE!",   // nor is a sign
		"0123-4567-89AB-CDEÉ",   // nor a letter of another alphabet
		"0123_4567_89AB_CDEF",   // nor an underscore
		"0123-4567-89AB-CDEF-0", // a group too many
	} {
		if _, ok := invite.Read(given); ok {
			t.Errorf("Read(%q) = ok, want no code", given)
		}
	}
}

// Whatever is given, Read does not panic; what it reads, it reads again
// written otherwise — in the other case, with O for 0 and I or L for 1, with
// dashes and white space between the symbols — to the same hash, and so it
// reads a code of sixteen symbols. Dashes and white space never change what
// it says (R34).
func FuzzRead(f *testing.F) {
	for _, seed := range []string{
		"0123-4567-89AB-CDEF",
		"o123-4567-89ab-cdef\r\n",
		"0I23 4567\t89AB CDEF",
		"0123-4567-89AB-CDE",
		"0123-4567-89AB-CDEF0",
		"0123-4567-89AB-CDEU",
		"0123-4567-89AB-CDEÉ",
		"ı123-4567-89AB-CDEF", // a dotless i, whose upper case is I
		"",
		"\xff\xfe",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, given string) {
		hash, ok := invite.Read(given)
		spaced := spread(given)
		if again, okAgain := invite.Read(spaced); okAgain != ok || again != hash {
			t.Fatalf("Read(%q) = %x, %v; and with dashes and spaces, %q, = %x, %v", given, hash, ok, spaced, again, okAgain)
		}
		if !ok {
			return
		}
		if n := symbols(given); n != invite.Symbols {
			t.Fatalf("Read(%q) = ok of %d symbols, want %d", given, n, invite.Symbols)
		}
		for _, other := range []string{
			strings.ToLower(given),
			strings.ToUpper(given),
			strings.NewReplacer("0", "o", "1", "l").Replace(given),
			strings.NewReplacer("0", "O", "1", "I").Replace(given),
		} {
			if again, okAgain := invite.Read(other); !okAgain || again != hash {
				t.Fatalf("Read(%q) = %x; and written as %q, = %x, %v", given, hash, other, again, okAgain)
			}
		}
	})
}

// spread is s with a dash after each of its runes, and white space around.
func spread(s string) string {
	var b strings.Builder
	b.WriteString(" \n")
	for _, r := range s {
		b.WriteRune(r)
		b.WriteByte('-')
	}
	b.WriteString(" \t")
	return b.String()
}

// symbols is how many runes of s are neither a dash nor white space.
func symbols(s string) int {
	n := 0
	for _, r := range s {
		if r != '-' && !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
