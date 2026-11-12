package quota_test

import (
	"testing"
	"time"

	"github.com/altnovatrail/api/internal/quota"
)

// at is an instant of UTC, "2026-11-12T10:00".
func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02T15:04", s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func offset(minutes int32) *int32 { return &minutes }

// An offset is from −12 to +14 hours, by quarters of an hour (R11).
func TestAnOffsetIsWithinItsBoundsByQuarters(t *testing.T) {
	t.Parallel()

	for _, m := range []int32{0, -720, 840, 330, 345, -570, 15} {
		if !quota.ValidOffset(m) {
			t.Errorf("ValidOffset(%d) = false", m)
		}
	}
	for _, m := range []int32{-735, 855, 7, 20, -1, 1000} {
		if quota.ValidOffset(m) {
			t.Errorf("ValidOffset(%d) = true", m)
		}
	}
}

// Until a device sends an offset, the day is UTC's, midnight to midnight; the
// first offset sent makes the first day by it, as no day came before.
func TestTheFirstDayIsByTheOffsetSentOrUTC(t *testing.T) {
	t.Parallel()

	now := at(t, "2026-11-12T10:30")
	utc := quota.Clock{}.Today(now)
	if utc.Start != at(t, "2026-11-12T00:00") || utc.End != at(t, "2026-11-13T00:00") || !utc.Holds(now) {
		t.Errorf("the day of no offset = %+v, want the day of UTC", utc)
	}
	c, changed := quota.Clock{}.Sent(offset(-300), now)
	if !changed || *c.Offset != -300 || c.Day.Start != at(t, "2026-11-12T05:00") || c.Day.End != at(t, "2026-11-13T05:00") {
		t.Errorf("the first day at −5 h = %+v, %v; want midnight to midnight at −5 h", c.Day, changed)
	}
	// One sent at its end, at a quarter's offset: still a day of 24 hours.
	c, _ = quota.Clock{}.Sent(offset(345), at(t, "2026-11-12T18:14"))
	if c.Day.Start != at(t, "2026-11-11T18:15") || c.Day.End != at(t, "2026-11-12T18:15") {
		t.Errorf("the first day at +5:45 = %+v", c.Day)
	}
}

// A day goes on to its end whatever offset a request sends meanwhile; the next
// begins where it ended and goes by the offset sent last. A request with no
// offset, or one out of bounds, counts under the offset kept, and a request
// within the day changes nothing.
func TestANewOffsetHoldsFromTheNextDay(t *testing.T) {
	t.Parallel()

	c, _ := quota.Clock{}.Sent(nil, at(t, "2026-11-12T10:00"))
	same, changed := c.Sent(nil, at(t, "2026-11-12T23:00"))
	if changed || *same.Day != *c.Day {
		t.Errorf("a request within the day changed the clock: %+v, %v", same.Day, changed)
	}
	c, changed = c.Sent(offset(120), at(t, "2026-11-12T23:00"))
	if !changed || *c.Offset != 120 || c.Day.End != at(t, "2026-11-13T00:00") {
		t.Errorf("the day after a new offset = %+v, %v; want it to end at its own end", c.Day, changed)
	}
	// The next day at +2 h: its first midnight 20 hours on is 22:00 UTC.
	next, _ := c.Sent(nil, at(t, "2026-11-13T00:00"))
	if next.Day.Start != at(t, "2026-11-13T00:00") || next.Day.End != at(t, "2026-11-13T22:00") {
		t.Errorf("the first day at +2 h = %+v, want from the end of the last to its midnight", next.Day)
	}
	for _, bad := range []int32{7, 900, -800} {
		kept, _ := next.Sent(offset(bad), at(t, "2026-11-13T10:00"))
		if *kept.Offset != 120 {
			t.Errorf("an offset of %d was kept", bad)
		}
	}
}

// The first day by a new offset is no shorter than 20 hours, so a jump from +14
// to −12 gives no second quota, and after a trip east of more than 4 hours it
// is longer, up to 44 hours; a day past which a while went with no request is a
// day of 24 hours that holds now.
func TestTheFirstDayByANewOffsetIsFrom20To44Hours(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		from, to int32
		// at is when the request that rolls the day comes.
		at         string
		start, end string
	}{
		// At +14 h a day ends at 10:00 UTC; at −12 h midnight is 12:00 UTC, 2 hours on.
		{"from +14 h to −12 h", 840, -720, "2026-11-12T10:00", "2026-11-12T10:00", "2026-11-13T12:00"},
		// From UTC east by 8 hours: midnights at 16:00 UTC, the first 20 hours on is
		// the next day's.
		{"east by 8 hours", 0, 480, "2026-11-13T00:30", "2026-11-13T00:00", "2026-11-14T16:00"},
		// East by 4 hours exactly: 20 hours on is midnight itself.
		{"east by 4 hours", 0, 240, "2026-11-13T00:00", "2026-11-13T00:00", "2026-11-13T20:00"},
		// West by 3 hours: midnights at 03:00 UTC, the first 20 hours on 27 hours on.
		{"west by 3 hours", 0, -180, "2026-11-13T01:00", "2026-11-13T00:00", "2026-11-14T03:00"},
		// Three days with no request: the day that holds now.
		{"a gap", 0, 480, "2026-11-16T09:00", "2026-11-15T16:00", "2026-11-16T16:00"},
		{"a gap at its end", 0, 0, "2026-11-16T00:00", "2026-11-16T00:00", "2026-11-17T00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c, _ := quota.Clock{}.Sent(offset(tc.from), at(t, "2026-11-12T09:00"))
			c, _ = c.Sent(offset(tc.to), at(t, "2026-11-12T09:30"))
			before := c.Day.End
			c, changed := c.Sent(nil, at(t, tc.at))
			if !changed || c.Day.Start != at(t, tc.start) || c.Day.End != at(t, tc.end) {
				t.Fatalf("the day = %+v, %v; want %s to %s", c.Day, changed, tc.start, tc.end)
			}
			if long := c.Day.End.Sub(c.Day.Start); long < 20*time.Hour || long > 44*time.Hour || c.Day.End.Sub(before) < 20*time.Hour ||
				!c.Day.Holds(at(t, tc.at)) || c.Day.Start.Before(before) {
				t.Errorf("the day %+v lasts %v after %v", c.Day, long, before)
			}
		})
	}
}

// A day ends at its end exactly: the instant of its end is the next day's.
func TestADayEndsAtItsEnd(t *testing.T) {
	t.Parallel()

	c, _ := quota.Clock{}.Sent(nil, at(t, "2026-11-12T10:00"))
	last := at(t, "2026-11-13T00:00").Add(-time.Nanosecond)
	if today := c.Today(last); today != *c.Day {
		t.Errorf("the last instant of the day is in %+v", today)
	}
	if today := c.Today(at(t, "2026-11-13T00:00")); today.Start != at(t, "2026-11-13T00:00") {
		t.Errorf("the end of the day is in %+v, want the next", today)
	}
	// A clock of the server set back counts in the day kept.
	if today := c.Today(at(t, "2026-11-11T22:00")); today != *c.Day {
		t.Errorf("a clock set back gives %+v, want the day kept", today)
	}
}
