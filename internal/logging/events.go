package logging

import (
	"log/slog"
	"regexp"
	"time"
)

// The table of the log: every event a line may be, and every field it may
// carry. A line's message is its event's name. A field is a key with a rule
// for its value, and an event names the fields it may carry; whatever a line
// carries beyond that is dropped before it is written (see guard).
//
// The table is what holds the log to privacy 4 spec, row "Журналы": events,
// codes, times and versions, and no direct identifier, IP address, work,
// reading or hint text (R09 spec). So a field holds a number, a flag, a
// duration, a word from a closed set, a build's identity in its own shape, or
// a kind this package made itself (see ErrorKind) — never free text. A new
// event, or a new field, comes with the code that writes it, as a row here,
// and the test of the table holds every row to its rules.

// The events of the service's own lines.
const (
	// EventStartup is the line that says a revision has started, and which
	// build it runs. The delivery finds a revision's release by it.
	EventStartup = "startup"
	// EventStopping is the line that says a stop was asked for.
	EventStopping = "stopping"
	// EventStopped is the line that says the server has drained.
	EventStopped = "stopped"
	// EventShutdown is the last line of a process that stopped as asked.
	EventShutdown = "shutdown"
	// EventFailure is a failure nobody asked for, at a moment of the closed set
	// the field moment names.
	EventFailure = "failure"
	// EventPanic is a panic a handler raised and the server recovered.
	EventPanic = "panic"
	// EventMigrated is the line that says the migrations ran: how many it applied,
	// and the version the schema is at.
	EventMigrated = "migrated"
)

// unregisteredEvent replaces the message of a line whose event the table does
// not know. The message itself is not written: it might be anything.
const unregisteredEvent = "unregistered_event"

// The fields.
const (
	FieldVersion       = "version"
	FieldCommit        = "commit"
	FieldDate          = "date"
	FieldPort          = "port"
	FieldDeployed      = "deployed"
	FieldStarted       = "started"
	FieldCutShort      = "cut_short"
	FieldMoment        = "moment"
	FieldErrorKind     = "error_kind"
	FieldPanicKind     = "panic_kind"
	FieldApplied       = "applied"
	FieldSchemaVersion = "schema_version"

	// fieldDropped is how many fields guard dropped from the line. guard
	// writes it itself, on every event; a caller cannot.
	fieldDropped = "dropped_fields"
)

// The moments of a failure: the value set of FieldMoment.
const (
	MomentStartup  = "startup"
	MomentServe    = "serve"
	MomentShutdown = "shutdown"
	MomentMigrate  = "migrate"
)

// events is every event and the fields it may carry.
var events = map[string][]string{
	EventStartup:  {FieldVersion, FieldCommit, FieldDate, FieldPort, FieldDeployed},
	EventStopping: {},
	EventStopped:  {FieldCutShort},
	EventShutdown: {FieldStarted},
	EventFailure:  {FieldMoment, FieldErrorKind},
	EventPanic:    {FieldPanicKind},
	EventMigrated: {FieldApplied, FieldSchemaVersion},
}

// rule is what a field's value must be: of a kind, and a string also of a
// closed set or of a shape.
type rule struct {
	kind slog.Kind
	// values is the closed set a string must be one of.
	values []string
	// shape is what a string must look like, when it is no word of a set but
	// a value of the build, such as its version.
	shape func(string) bool
}

// fields is every field and its rule.
var fields = map[string]rule{
	FieldVersion:       {kind: slog.KindString, shape: matches(versionShape)},
	FieldCommit:        {kind: slog.KindString, shape: matches(commitShape)},
	FieldDate:          {kind: slog.KindString, shape: isBuildDate},
	FieldPort:          {kind: slog.KindInt64},
	FieldDeployed:      {kind: slog.KindBool},
	FieldStarted:       {kind: slog.KindBool},
	FieldCutShort:      {kind: slog.KindBool},
	FieldMoment:        {kind: slog.KindString, values: []string{MomentStartup, MomentServe, MomentShutdown, MomentMigrate}},
	FieldErrorKind:     {kind: slog.KindLogValuer},
	FieldPanicKind:     {kind: slog.KindLogValuer},
	FieldApplied:       {kind: slog.KindInt64},
	FieldSchemaVersion: {kind: slog.KindInt64},
}

// The shapes of the build's identity, as the justfile's build stamps it: a
// version is "dev", a tag of semver — a prerelease too, v0.2.0-rc.1 — with how
// far past it the build is (git describe), or a commit when there is no tag
// yet; the last two may be dirty.
var (
	versionShape = regexp.MustCompile(`^(dev|v\d+\.\d+\.\d+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?(-\d+-g[0-9a-f]{7,40})?(-dirty)?|[0-9a-f]{7,40}(-dirty)?)$`)
	commitShape  = regexp.MustCompile(`^(unknown|[0-9a-f]{7,40})$`)
)

func matches(shape *regexp.Regexp) func(string) bool { return shape.MatchString }

// isBuildDate accepts what the build stamps, a time in RFC 3339, or "unknown".
func isBuildDate(value string) bool {
	if value == "unknown" {
		return true
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}
