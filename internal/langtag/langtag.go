// Package langtag tells a well-formed language tag of BCP 47, as api takes one
// from app: for the profile's language and for the language of the tutor's
// words, which goes to a model by its name or as the tag itself.
package langtag

import (
	"errors"
	"regexp"

	"golang.org/x/text/language"
)

// Max is the longest tag taken: as long as the avatar's code.
const Max = 64

// WellFormed says whether tag is a well-formed tag of BCP 47, which api keeps
// with no list of its own: subtags of letters and digits joined by hyphens, as
// RFC 5646 writes them, of no more than Max, that the library parses. A subtag
// it does not know is still well-formed. The library alone would take an
// underscore for a hyphen, and a tag of any length.
func WellFormed(tag string) bool {
	if len(tag) > Max || !shape.MatchString(tag) {
		return false
	}
	_, err := language.Parse(tag)
	var unknown language.ValueError
	return err == nil || errors.As(err, &unknown)
}

// shape is the shape of a tag of BCP 47: subtags of 1 to 8 letters and digits,
// joined by hyphens.
var shape = regexp.MustCompile(`^[A-Za-z0-9]{1,8}(-[A-Za-z0-9]{1,8})*$`)
