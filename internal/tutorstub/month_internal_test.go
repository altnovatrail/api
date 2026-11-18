package tutorstub

import (
	"testing"
	"time"
)

// The ceiling of the tier lets a key go at 00:00 UTC on the first day of the
// next month by UTC, the year's last month included, wherever the clock's zone
// puts the time.
func TestTheTierReturnsOnTheFirstOfTheNextMonth(t *testing.T) {
	t.Parallel()

	east := time.FixedZone("UTC+3", 3*60*60)
	for at, want := range map[time.Time]time.Time{
		time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC):  time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC): time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC):    time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.December, 1, 1, 0, 0, 0, east):        time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, time.January, 31, 22, 0, 0, 0, time.UTC):   time.Date(2027, time.February, 1, 0, 0, 0, 0, time.UTC),
	} {
		if got := nextMonth(at); !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("nextMonth(%v) = %v, want %v", at, got, want)
		}
	}
}
