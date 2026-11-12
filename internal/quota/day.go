package quota

import "time"

// The family day (R11; architecture 3.6): the quota counts by the day of the
// family, from midnight to midnight by the clock of its device, as R293
// standalone had it, rather than by UTC, which would renew it in the evening in
// America. app sends the offset of the device's clock from UTC with the
// requests on platform AI; the account keeps the offset and its current day,
// and a new offset holds from the next day on. The first day by a new offset
// ends at its first midnight no sooner than 20 hours after the end of the day
// before, so that a jump from +14 hours to −12 gives no day of 2 hours and a
// second quota; after a trip east of more than 4 hours that day is longer, up
// to 44 hours. Until a device has sent an offset, the day is UTC's.

// The bounds of an offset (R11): from −12 to +14 hours, by quarters of an hour.
const (
	minOffset  = -12 * 60
	maxOffset  = 14 * 60
	offsetStep = 15
)

// leastDay is the shortest a first day by a new offset may be after the day
// before.
const leastDay = 20 * time.Hour

// day is the length of a family day by one offset.
const day = 24 * time.Hour

// ValidOffset says whether minutes is an offset of a clock from UTC that the
// quota takes: from −12 to +14 hours, a multiple of a quarter of an hour. Any
// other is ignored, and the request counts under the offset kept.
func ValidOffset(minutes int32) bool {
	return minutes >= minOffset && minutes <= maxOffset && minutes%offsetStep == 0
}

// Day is a family day: the instant it begins, the key of its counters, and the
// instant it ends, the first of the next.
type Day struct {
	Start, End time.Time
}

// Holds says whether the day holds the instant t: from its start, until its end.
func (d Day) Holds(t time.Time) bool { return !t.Before(d.Start) && t.Before(d.End) }

// Clock is what an account keeps of its family day: the last offset its devices
// sent, in minutes, if any, and its current day, if it has had one.
type Clock struct {
	Offset *int32
	Day    *Day
}

// Today is the family day of the clock c at now, as c keeps it: its day, until
// it ends, even when the server's clock went back before its start; else the
// day that holds now, by the offset kept — UTC when there is none — and after
// the day kept.
func (c Clock) Today(now time.Time) Day {
	if c.Day != nil && now.Before(c.Day.End) {
		return *c.Day
	}
	var offset int32
	if c.Offset != nil {
		offset = *c.Offset
	}
	var before *time.Time
	if c.Day != nil {
		before = &c.Day.End
	}
	return dayAfter(before, offset, now)
}

// Sent is the clock c after a request at now that sent the offset reported, if
// it sent one, and whether it changed: a valid offset is kept, for the days that
// begin from then on — the current day ends at its own end — and once the day
// kept is over, the clock keeps the day that holds now. An offset that is not
// valid is ignored.
func (c Clock) Sent(reported *int32, now time.Time) (Clock, bool) {
	changed := false
	if reported != nil && ValidOffset(*reported) && (c.Offset == nil || *c.Offset != *reported) {
		offset := *reported
		c.Offset, changed = &offset, true
	}
	if c.Day == nil || !now.Before(c.Day.End) {
		today := c.Today(now)
		c.Day, changed = &today, true
	}
	return c, changed
}

// dayAfter is the family day that holds now, by the offset of minutes, after
// the day that ended at before, when there was one: it ends at the first
// midnight by the offset after now that is no sooner than 20 hours after
// before, and begins at the midnight 24 hours earlier — or at before, when that
// midnight is still to come, or came before it.
func dayAfter(before *time.Time, minutes int32, now time.Time) Day {
	end := midnightAfter(now, time.Duration(minutes)*time.Minute)
	if before != nil {
		for end.Before(before.Add(leastDay)) {
			end = end.Add(day)
		}
	}
	start := end.Add(-day)
	if before != nil && (start.After(now) || start.Before(*before)) {
		start = *before
	}
	return Day{Start: start, End: end}
}

// midnightAfter is the first midnight by the clock of offset that comes after
// t, as an instant in UTC.
func midnightAfter(t time.Time, offset time.Duration) time.Time {
	// Truncate counts from the zero time, a midnight of UTC: whole days of the
	// local time are midnights by the offset.
	local := t.UTC().Add(offset).Truncate(day)
	return local.Add(day).Add(-offset)
}
