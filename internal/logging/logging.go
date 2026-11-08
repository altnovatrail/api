// Package logging is the service's log: JSON lines on one stream, in the keys
// Cloud Logging reads, held to the table of events in events.go.
//
// Every line goes through guard, which writes only what the table allows. So
// the rule that the log carries no direct identifier, IP address, work,
// reading or hint text (R09 spec; privacy 4 spec) does not rest on each line's
// author remembering it: a field the table does not give the event is dropped
// and counted, and an event the table does not know loses its message. The
// libraries in the process — tutor, module-kit and the rest — are handed this
// same *slog.Logger, and their lines pass the same guard.
package logging

import (
	"context"
	"io"
	"log/slog"
	"slices"
)

// New is the log of the process, writing to w the lines at level and above.
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(&guard{next: slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: cloudLogging,
	})})
}

// cloudLogging renames slog's keys to the ones Cloud Logging reads out of a
// JSON line on stdout: severity and message, with the time it already reads.
// Only the line's own keys are renamed: guard lets no group through, so a
// field never stands below one.
func cloudLogging(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
		a.Value = slog.StringValue(severity(a.Value.Any().(slog.Level)))
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// severity is the name Cloud Logging gives a level: a level between two of
// slog's own takes the name of the one below it.
func severity(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARNING"
	case level >= slog.LevelInfo:
		return "INFO"
	}
	return "DEBUG"
}

// guard is the handler that holds every line to the table. It keeps the
// fields given by With to check them on each line, against the event of that
// line, and hands next only lines it has built itself.
type guard struct {
	next slog.Handler
	// with is what With gave, in order, to be checked on each line.
	with []slog.Attr
	// grouped is set once WithGroup has been called: no field below a group
	// is written, since the table names fields of a line and not of a group.
	grouped bool
}

// Enabled is next's: the level is the only filter before the table.
func (g *guard) Enabled(ctx context.Context, level slog.Level) bool {
	return g.next.Enabled(ctx, level)
}

// Handle writes the line with the fields the table gives its event, and the
// count of those it dropped.
func (g *guard) Handle(ctx context.Context, r slog.Record) error {
	allowed, known := events[r.Message]
	message := r.Message
	if !known {
		message = unregisteredEvent
	}
	line := slog.NewRecord(r.Time, r.Level, message, 0)

	dropped := 0
	keep := func(a slog.Attr) bool {
		switch {
		case a.Equal(slog.Attr{}):
			// slog itself ignores an empty field, so it is no field at all.
		case known && !g.grouped && slices.Contains(allowed, a.Key) && permitted(a):
			line.AddAttrs(a)
		default:
			dropped++
		}
		return true
	}
	for _, a := range g.with {
		keep(a)
	}
	r.Attrs(keep)
	if dropped > 0 {
		line.AddAttrs(slog.Int(fieldDropped, dropped))
	}
	return g.next.Handle(ctx, line)
}

// WithAttrs keeps the fields to check them on each line.
func (g *guard) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &guard{next: g.next, with: slices.Concat(g.with, attrs), grouped: g.grouped}
}

// WithGroup drops every field written below the group.
func (g *guard) WithGroup(name string) slog.Handler {
	if name == "" {
		return g
	}
	return &guard{next: g.next, with: g.with, grouped: true}
}

// permitted says whether a field's value keeps its field's rule. A field the
// table does not know has no rule, and is not permitted.
func permitted(a slog.Attr) bool {
	rule, known := fields[a.Key]
	if !known {
		return false
	}
	// A kind is a value this package made, and only one: it is checked as it
	// is, before anything resolves it.
	if rule.kind == slog.KindLogValuer {
		kind, made := a.Value.Any().(Kind)
		return a.Value.Kind() == slog.KindLogValuer && made && kind.name != ""
	}
	value := a.Value.Resolve()
	if value.Kind() != rule.kind {
		return false
	}
	if rule.kind != slog.KindString {
		return true
	}
	switch {
	case rule.values != nil:
		return slices.Contains(rule.values, value.String())
	case rule.shape != nil:
		return rule.shape(value.String())
	}
	return false
}
