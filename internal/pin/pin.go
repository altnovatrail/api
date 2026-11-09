// Package pin is the PIN of the section for adults (R04, R35): its form, its
// hash and the count of wrong PINs that closes its input. The PIN is a barrier
// of the interface, not of the server; the count is what keeps it from being
// tried by the server, one for every device of the account.
package pin

import (
	"time"

	"github.com/altnovatrail/api/internal/argon"
)

// The count (R04, R35): no more than MaxFailures wrong PINs in any Window. The
// count is of the wrong PINs of the last Window, and the one that brings it to
// MaxFailures closes the input for LockFor from itself.
const (
	MaxFailures = 5
	Window      = 15 * time.Minute
	LockFor     = 15 * time.Minute
)

// Valid says whether pin has the contract's form: from 4 to 6 digits.
func Valid(pin string) bool {
	if len(pin) < 4 || len(pin) > 6 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Hash is the PIN's argon2id, to Put.
func Hash(pin string) string { return argon.Hash([]byte(pin)) }

// State is what the account keeps of its PIN.
type State struct {
	// Hash is the PIN's argon2id, or empty while none is set.
	Hash string
	// Failed is when each wrong PIN of the last Window was given, the oldest
	// first; it may hold older ones, which no longer count.
	Failed []time.Time
	// LockedUntil is when the input opens once it closed; zero while it is open.
	LockedUntil time.Time
}

// Set says whether a PIN is set.
func (s *State) Set() bool { return s.Hash != "" }

// Locked says whether the input is closed at now.
func (s *State) Locked(now time.Time) bool { return now.Before(s.LockedUntil) }

// Put sets the PIN by its Hash, and opens the input with its count cleared.
func (s *State) Put(hash string) { *s = State{Hash: hash} }

// Check is how a PIN compared with the one set.
type Check struct {
	// Matches says it is the PIN set.
	Matches bool
	// Left is how many wrong PINs are left before the input closes, beside a
	// wrong one; 0 when this one closed it.
	Left int
	// LockedUntil is when the input opens, beside the wrong PIN that closed it.
	LockedUntil time.Time
}

// Try compares pin with the one set at now, while the input is open. The right
// PIN clears the count. A wrong one is counted with those of the last Window,
// and the one that brings them to MaxFailures closes the input.
func (s *State) Try(pin string, now time.Time) Check {
	if argon.Matches([]byte(pin), s.Hash) {
		s.Failed, s.LockedUntil = nil, time.Time{}
		return Check{Matches: true}
	}
	recent := s.Failed[:0:0]
	for _, at := range s.Failed {
		if now.Before(at.Add(Window)) {
			recent = append(recent, at)
		}
	}
	s.Failed = append(recent, now)
	check := Check{Left: MaxFailures - len(s.Failed)}
	if len(s.Failed) >= MaxFailures {
		s.LockedUntil = now.Add(LockFor)
		check.Left, check.LockedUntil = 0, s.LockedUntil
	}
	return check
}
