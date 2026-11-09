package invite_test

import (
	"crypto/sha256"
	"regexp"
	"testing"

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

func TestTheTariffOfTheClosedSliceIsKnown(t *testing.T) {
	t.Parallel()

	if !invite.Known(invite.ClosedSlice) {
		t.Errorf("Known(%q) = false", invite.ClosedSlice)
	}
	for _, tariff := range []string{"", "plus", "Closed_Slice"} {
		if invite.Known(tariff) {
			t.Errorf("Known(%q) = true", tariff)
		}
	}
}
