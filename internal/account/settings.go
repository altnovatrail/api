package account

import (
	"errors"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"golang.org/x/text/language"

	"github.com/altnovatrail/api/internal/store"
)

// The bounds of the settings (contract, ProfileSettings).
const (
	maxPseudonym = 30
	maxAvatar    = 64
	maxGrade     = 12
)

// newProfile is a profile of the category and country with the defaults of the
// contract: the mode "by steps"; the voicing on at under_13 and off at the
// others, at normal speed; on a child's profile "ask before showing" on.
func newProfile(category, country string) store.Profile {
	return store.Profile{
		AgeCategory:      category,
		Country:          country,
		VoicingEnabled:   category == "under_13",
		VoicingSpeed:     "normal",
		DefaultMode:      "by_steps",
		AskBeforeShowing: category != "adult",
	}
}

// apply sets on p the fields of s that are sent, and leaves the others as they
// are; on a profile being created, the pseudonym is needed. A field of another
// form is invalid_argument, its message the field's name and never its value.
func apply(p *store.Profile, s *v1.ProfileSettings, creating bool) error {
	if s == nil {
		s = &v1.ProfileSettings{}
	}
	switch {
	case s.Pseudonym != nil:
		pseudonym := strings.TrimSpace(s.GetPseudonym())
		if n := utf8.RuneCountInString(pseudonym); n < 1 || n > maxPseudonym {
			return malformed("pseudonym")
		}
		p.Pseudonym = pseudonym
	case creating:
		return malformed("pseudonym")
	}
	if s.Avatar != nil {
		if !ascii(s.GetAvatar()) || len(s.GetAvatar()) > maxAvatar {
			return malformed("avatar")
		}
		p.Avatar = s.GetAvatar()
	}
	if err := applyVoicing(p, s.GetVoicing()); err != nil {
		return err
	}
	switch s.GetDefaultMode() {
	case v1.Mode_MODE_UNSPECIFIED:
	case v1.Mode_MODE_BY_STEPS:
		p.DefaultMode = "by_steps"
	case v1.Mode_MODE_SELF:
		p.DefaultMode = "self"
	default:
		return malformed("default_mode")
	}
	if s.InterfaceLanguage != nil {
		switch tag := s.GetInterfaceLanguage(); {
		case tag == "":
			p.InterfaceLanguage = nil
		case !wellFormed(tag):
			return malformed("interface_language")
		default:
			p.InterfaceLanguage = &tag
		}
	}
	if s.Grade != nil {
		switch grade := s.GetGrade(); {
		case grade == 0:
			p.Grade = nil
		case grade < 1 || grade > maxGrade:
			return malformed("grade")
		default:
			g := int16(grade)
			p.Grade = &g
		}
	}
	if s.AskBeforeShowing != nil {
		// A profile of the category adult never asks before showing.
		if p.AgeCategory == "adult" {
			return malformed("ask_before_showing")
		}
		p.AskBeforeShowing = s.GetAskBeforeShowing()
	}
	return nil
}

// applyVoicing sets the voicing's fields that are sent: whether it is on, and
// its speed unless VOICING_SPEED_UNSPECIFIED.
func applyVoicing(p *store.Profile, v *v1.Voicing) error {
	if v == nil {
		return nil
	}
	if v.Enabled != nil {
		p.VoicingEnabled = v.GetEnabled()
	}
	switch v.GetSpeed() {
	case v1.VoicingSpeed_VOICING_SPEED_UNSPECIFIED:
	case v1.VoicingSpeed_VOICING_SPEED_NORMAL:
		p.VoicingSpeed = "normal"
	case v1.VoicingSpeed_VOICING_SPEED_SLOW:
		p.VoicingSpeed = "slow"
	default:
		return malformed("voicing")
	}
	return nil
}

// wellFormed says whether tag is a well-formed tag of BCP 47, which api keeps
// with no list of its own (R92 spec): a subtag the library does not know is
// still well-formed, and only a tag of another form is refused.
func wellFormed(tag string) bool {
	_, err := language.Parse(tag)
	var unknown language.ValueError
	return err == nil || errors.As(err, &unknown)
}

// ascii says whether s is printable ASCII alone.
func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// malformed is a field whose form the contract refuses. Its message names the
// field, never the value.
func malformed(field string) error { return connect.NewError(connect.CodeInvalidArgument, field) }
