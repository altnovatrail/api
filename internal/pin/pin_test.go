package pin_test

import (
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/pin"
)

func TestAPINIsFourToSixDigits(t *testing.T) {
	t.Parallel()

	for _, given := range []string{"0000", "12345", "987654"} {
		if !pin.Valid(given) {
			t.Errorf("Valid(%q) = false", given)
		}
	}
	for _, given := range []string{"", "123", "1234567", "12a4", "12 34", "١٢٣٤", "-123"} {
		if pin.Valid(given) {
			t.Errorf("Valid(%q) = true", given)
		}
	}
}

var start = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func set(t *testing.T) *pin.State {
	t.Helper()
	s := &pin.State{}
	s.Put(pin.Hash("2468"))
	if !s.Set() {
		t.Fatal("Put set no PIN")
	}
	return s
}

func TestTheFifthWrongPINClosesTheInputForAQuarterOfAnHour(t *testing.T) {
	t.Parallel()

	s := set(t)
	for i := 1; i < pin.MaxFailures; i++ {
		check := s.Try("1111", start.Add(time.Duration(i)*time.Minute))
		if check.Matches || check.Left != pin.MaxFailures-i || !check.LockedUntil.IsZero() {
			t.Fatalf("wrong PIN %d = %+v, want %d left and open", i, check, pin.MaxFailures-i)
		}
	}
	fifth := start.Add(10 * time.Minute)
	check := s.Try("1111", fifth)
	if check.Matches || check.Left != 0 || !check.LockedUntil.Equal(fifth.Add(pin.LockFor)) {
		t.Fatalf("fifth wrong PIN = %+v, want 0 left and closed until %v", check, fifth.Add(pin.LockFor))
	}
	if !s.Locked(fifth.Add(pin.LockFor-time.Second)) || s.Locked(fifth.Add(pin.LockFor)) {
		t.Error("the input is not closed for exactly LockFor from the fifth wrong PIN")
	}
	// Once it opens, every wrong PIN before is older than the window.
	if check := s.Try("1111", fifth.Add(pin.LockFor)); check.Left != pin.MaxFailures-1 {
		t.Errorf("a wrong PIN after the lock = %+v, want a full count", check)
	}
}

// No more than five wrong PINs fit in any quarter of an hour, across the edge of
// a window too.
func TestTheCountIsOfTheLastQuarterOfAnHour(t *testing.T) {
	t.Parallel()

	s := set(t)
	s.Try("1111", start)
	for i := range pin.MaxFailures - 2 {
		s.Try("1111", start.Add(14*time.Minute+time.Duration(i)*time.Second))
	}
	// The first has passed out of the window; the three of minute 14 have not.
	edge := start.Add(pin.Window)
	if check := s.Try("1111", edge); check.Left != 1 || !check.LockedUntil.IsZero() {
		t.Fatalf("a wrong PIN at the edge = %+v, want 1 left", check)
	}
	if check := s.Try("1111", edge.Add(time.Second)); check.Left != 0 || !check.LockedUntil.Equal(edge.Add(time.Second+pin.LockFor)) {
		t.Errorf("the fifth of the last quarter of an hour = %+v, want the input closed", check)
	}
}

func TestTheRightPINClearsTheCount(t *testing.T) {
	t.Parallel()

	s := set(t)
	s.Try("1111", start)
	s.Try("1111", start.Add(time.Second))
	if check := s.Try("2468", start.Add(2*time.Second)); !check.Matches || check.Left != 0 {
		t.Fatalf("the right PIN = %+v, want a match", check)
	}
	if len(s.Failed) != 0 || !s.LockedUntil.IsZero() {
		t.Errorf("state after the right PIN = %+v, want no count", s)
	}
	if check := s.Try("1111", start.Add(3*time.Second)); check.Left != pin.MaxFailures-1 {
		t.Errorf("a wrong PIN after the right one = %+v, want a full count", check)
	}
}

func TestPuttingAPINOpensTheInput(t *testing.T) {
	t.Parallel()

	s := set(t)
	for range pin.MaxFailures {
		s.Try("1111", start)
	}
	s.Put(pin.Hash("1357"))
	if s.Locked(start) || len(s.Failed) != 0 || !s.Try("1357", start).Matches {
		t.Errorf("state after Put = %+v, want the new PIN and the input open", s)
	}
}
