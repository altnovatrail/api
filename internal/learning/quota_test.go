package learning_test

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/google/uuid"
)

// familyDayOf is the offset and the end of the day the account keeps.
func (w *world) familyDayOf(t *testing.T, accountID string) (*int32, *time.Time) {
	t.Helper()
	var offset *int32
	var end *time.Time
	if err := w.pool.QueryRow(t.Context(), `SELECT utc_offset_minutes, day_ends_at FROM accounts WHERE id = $1`, accountID).
		Scan(&offset, &end); err != nil {
		t.Fatal(err)
	}
	return offset, end
}

// A request on platform AI that sends the offset of its device's clock gives
// the account its family day by it, which ends at a midnight of that clock; a
// request by an own key keeps nothing of its offset, and no line of the log
// names an offset (R11, R12).
func TestARequestOnPlatformAIGivesTheAccountItsFamilyDay(t *testing.T) {
	t.Parallel()

	ts := &tellers{}
	w := newWordsWorld(t, ts, nil)
	l := w.learner(t)
	ts.say("Which of the two numbers has more tens?", nil)
	trapped := []*v1.TappedChoice{tap(1, 1, "12"), tap(2, 1, "30")}

	own := w.begin(t, l, l.adult, "pairs").GetAttemptId()
	w.wrongs(t, l, l.adult, own, trapped)
	wrong, east := int32(1), int32(120)
	if _, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.adult, AttemptId: own,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, OwnKey: &v1.OwnKey{Provider: "anthropic", Key: "k"},
		ClockOffsetMinutes: &east}); err != nil {
		t.Fatal(err)
	}
	if offset, end := w.familyDayOf(t, l.accountID); offset != nil || end != nil {
		t.Errorf("a request by an own key kept %v, %v", offset, end)
	}

	a := w.begin(t, l, l.child, "pairs").GetAttemptId()
	w.wrongs(t, l, l.child, a, trapped)
	west := int32(-300)
	if _, err := w.attempts.GetRungWords(l.ctx, &v1.GetRungWordsRequest{ProfileId: l.child, AttemptId: a,
		IdempotencyKey: uuid.NewString(), WrongAnswers: &wrong, ClockOffsetMinutes: &west}); err != nil {
		t.Fatal(err)
	}
	offset, end := w.familyDayOf(t, l.accountID)
	if offset == nil || *offset != west || end == nil || !end.After(w.clock.now()) || end.Add(-5*time.Hour).UTC().Hour() != 0 {
		t.Errorf("the account keeps %v, %v; want −300 and a midnight at −5 h ahead", offset, end)
	}
	if log := w.log.String(); strings.Contains(log, "offset") || strings.Contains(log, "-300") {
		t.Errorf("the log names an offset: %s", log)
	}
}
