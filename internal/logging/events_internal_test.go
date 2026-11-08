package logging

import (
	"log/slog"
	"regexp"
	"testing"
)

// Every row of the table keeps the table's rules, so that a field added in a
// hurry is still a field the log may carry.
func TestTheTableKeepsItsRules(t *testing.T) {
	t.Parallel()

	snakeCase := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	for event, carried := range events {
		if !snakeCase.MatchString(event) {
			t.Errorf("event %q is not snake_case", event)
		}
		if event == unregisteredEvent {
			t.Errorf("event %q is the name of an unknown event", event)
		}
		for _, field := range carried {
			if _, ruled := fields[field]; !ruled {
				t.Errorf("event %s carries %s, which has no rule", event, field)
			}
		}
	}
	for field, rule := range fields {
		if !snakeCase.MatchString(field) || field == fieldDropped {
			t.Errorf("field %q is not snake_case, or is the guard's own", field)
		}
		switch rule.kind {
		case slog.KindString:
			// Free text is what the log must not carry: a string is of a set
			// or of a shape.
			if (rule.values == nil) == (rule.shape == nil) {
				t.Errorf("string field %s has to have a set of values or a shape, and only one", field)
			}
		case slog.KindInt64, slog.KindBool, slog.KindDuration, slog.KindLogValuer:
			if rule.values != nil || rule.shape != nil {
				t.Errorf("field %s of kind %v has a set or a shape, which only a string uses", field, rule.kind)
			}
		default:
			t.Errorf("field %s is of kind %v, which the log does not carry", field, rule.kind)
		}
	}
}

// A rule of a kind the table cannot check permits nothing.
func TestAStringRuleWithNeitherSetNorShapePermitsNothing(t *testing.T) {
	fields["test_unruled"] = rule{kind: slog.KindString}
	t.Cleanup(func() { delete(fields, "test_unruled") })

	if permitted(slog.String("test_unruled", "anything")) {
		t.Error("a string field with neither set nor shape permitted a value")
	}
}

func TestADurationFieldIsKept(t *testing.T) {
	fields["test_took"] = rule{kind: slog.KindDuration}
	t.Cleanup(func() { delete(fields, "test_took") })

	if !permitted(slog.Duration("test_took", 1)) || permitted(slog.Int("test_took", 1)) {
		t.Error("a duration field is not held to its kind")
	}
}

// An empty group is no group, as slog.Handler has it: the handler itself.
func TestAnEmptyGroupIsTheHandlerItself(t *testing.T) {
	t.Parallel()

	g := &guard{next: slog.DiscardHandler}
	if g.WithGroup("") != slog.Handler(g) {
		t.Error("WithGroup(\"\") is another handler")
	}
}
