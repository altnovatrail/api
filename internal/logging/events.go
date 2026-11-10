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
	// EventStartup is the line that says a revision has started, which build it
	// runs, and what it declared for admission to AI (R25, R45). The delivery
	// finds a revision's release by it.
	EventStartup = "startup"
	// EventStopping is the line that says a stop was asked for.
	EventStopping = "stopping"
	// EventStopped is the line that says the server has drained.
	EventStopped = "stopped"
	// EventShutdown is the last line of a process that stopped as asked.
	EventShutdown = "shutdown"
	// EventFailure is a failure nobody asked for, at a moment of the closed set
	// the field moment names, and in the daily job, at which of its steps.
	EventFailure = "failure"
	// EventPanic is a panic a handler raised and the server recovered.
	EventPanic = "panic"
	// EventMigrated is the line that says the migrations of a database ran: how many
	// it applied, the version the schema is at, and, for the rights', whether it
	// made the store and wrote the model.
	EventMigrated = "migrated"
	// EventRequest is the line of a request of the protocol: its method, its code,
	// the cause of a refusal, the class of a refused token, how long it ran and,
	// when it failed, the kind of the failure — never its body, headers, address
	// or an ID (R18, R33). The path of a Connect GET holds the message, so the
	// method is written, not the path.
	EventRequest = "request"
	// EventSwept is the line of a step of the daily job (R21): which step, how many
	// it did and failed, and whether it went to its end — counts alone, never an
	// ID of what it did them to.
	EventSwept = "swept"
	// EventPackagesLaid is the line of api packages that laid the packages of the
	// pinned release of modules into a folder (R07, R41): the release, and how many
	// packages and files of their device parts it laid.
	EventPackagesLaid = "packages_laid"
	// EventPackageFile is the line of a request for a file of a device part, which
	// is no method of Connect (R18, R42): its HTTP status, how long it ran and, when
	// it failed, the kind of the failure — never its path, which names the package
	// and the file.
	EventPackageFile = "package_file"
	// EventAdmission is the line of api check that says what it found the
	// deployment to declare for admission to AI (R25, R45): the version of the
	// table, whether a file of facts was given, and the least stage and whether it
	// was named, so that the operator sees what is missing — never a provider, a
	// fact or a path.
	EventAdmission = "admission"
	// EventAIUsage is the line of what one model of a call of AI took, beside the
	// line of the request that made the call (R12, R46): the method, the provider
	// and the model, its tokens, how the call ended and how long it took — never
	// the work, what was read or what failed in its words.
	EventAIUsage = "ai_usage"
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
	FieldDatabase      = "database"
	FieldStoreCreated  = "store_created"
	FieldModelWritten  = "model_written"
	FieldMethod        = "method"
	FieldCode          = "code"
	FieldCause         = "cause"
	FieldTokenClass    = "token_class"
	FieldDurationMS    = "duration_ms"
	FieldStep          = "step"
	FieldDone          = "done"
	FieldFailed        = "failed"
	FieldFinished      = "finished"
	// FieldModulesRelease is the release of modules that modules.json pins.
	FieldModulesRelease = "modules_release"
	FieldPackages       = "packages"
	FieldFiles          = "files"
	// FieldStatus is the HTTP status of an answer.
	FieldStatus = "status"
	// FieldAdmissionTable is the version of the admission table, MAJOR.MINOR.
	FieldAdmissionTable = "admission_table"
	// FieldFactsFile says whether the deployment named a file of facts.
	FieldFactsFile = "facts_file"
	// FieldRung is the least stage of the rule of admission of models.
	FieldRung = "rung"
	// FieldRungSet says whether the deployment named the stage, rather than
	// taking full by default.
	FieldRungSet = "rung_set"
	// FieldPlatformKeys is how many providers the platform holds a key of.
	FieldPlatformKeys = "platform_keys"
	// FieldOutcomeCause is the cause of the outcome of an answer that was no
	// refusal: a call of AI that gave no reading.
	FieldOutcomeCause = "outcome_cause"
	// FieldProvider is a provider of AI, by its code in the admission table.
	FieldProvider = "provider"
	// FieldModel is a model of a provider, by the ID the provider answered with.
	FieldModel          = "model"
	FieldInputTokens    = "input_tokens"
	FieldOutputTokens   = "output_tokens"
	FieldThinkingTokens = "thinking_tokens"
	// FieldAIOutcome is how a call of AI ended, in a word of AIOutcomes.
	FieldAIOutcome = "ai_outcome"

	// fieldDropped is how many fields guard dropped from the line. guard
	// writes it itself, on every event; a caller cannot.
	fieldDropped = "dropped_fields"
)

// The moments of a failure: the value set of FieldMoment.
const (
	MomentStartup   = "startup"
	MomentServe     = "serve"
	MomentShutdown  = "shutdown"
	MomentMigrate   = "migrate"
	MomentInvite    = "invite"
	MomentCountries = "countries"
	MomentSweep     = "sweep"
	MomentPackages  = "packages"
)

// The steps of the daily job: the value set of FieldStep.
const (
	StepMarks             = "marks"
	StepIdleAccounts      = "idle_accounts"
	StepIdleProfiles      = "idle_profiles"
	StepAbandonedAttempts = "abandoned_attempts"
	StepEvidence          = "evidence"
	StepAbandonedRuns     = "abandoned_runs"
	StepUsers             = "users"
	StepTuples            = "tuples"
	StepConsentRecords    = "consent_records"
	StepKeys              = "keys"
	StepChangelog         = "changelog"
)

// Steps is every step of the daily job, in the order it takes them.
var Steps = []string{StepMarks, StepIdleAccounts, StepIdleProfiles, StepAbandonedAttempts, StepEvidence, StepAbandonedRuns, StepUsers, StepTuples, StepConsentRecords, StepKeys, StepChangelog}

// The databases api migrates: the value set of FieldDatabase.
const (
	DatabaseAPI     = "api"
	DatabaseOpenFGA = "openfga"
)

// events is every event and the fields it may carry.
var events = map[string][]string{
	EventStartup: {FieldVersion, FieldCommit, FieldDate, FieldPort, FieldDeployed, FieldModulesRelease, FieldPackages,
		FieldAdmissionTable, FieldFactsFile, FieldRung, FieldRungSet, FieldPlatformKeys},
	EventStopping:     {},
	EventStopped:      {FieldCutShort},
	EventShutdown:     {FieldStarted},
	EventFailure:      {FieldMoment, FieldErrorKind, FieldDatabase, FieldStep},
	EventPanic:        {FieldPanicKind},
	EventMigrated:     {FieldDatabase, FieldApplied, FieldSchemaVersion, FieldStoreCreated, FieldModelWritten},
	EventRequest:      {FieldMethod, FieldCode, FieldCause, FieldTokenClass, FieldDurationMS, FieldErrorKind, FieldOutcomeCause},
	EventSwept:        {FieldStep, FieldDone, FieldFailed, FieldFinished},
	EventPackagesLaid: {FieldModulesRelease, FieldPackages, FieldFiles},
	EventPackageFile:  {FieldStatus, FieldDurationMS, FieldErrorKind},
	EventAdmission:    {FieldAdmissionTable, FieldFactsFile, FieldRung, FieldRungSet, FieldPlatformKeys},
	EventAIUsage: {FieldMethod, FieldProvider, FieldModel, FieldInputTokens, FieldOutputTokens, FieldThinkingTokens,
		FieldAIOutcome, FieldDurationMS},
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
	FieldVersion:        {kind: slog.KindString, shape: matches(versionShape)},
	FieldCommit:         {kind: slog.KindString, shape: matches(commitShape)},
	FieldDate:           {kind: slog.KindString, shape: isBuildDate},
	FieldPort:           {kind: slog.KindInt64},
	FieldDeployed:       {kind: slog.KindBool},
	FieldStarted:        {kind: slog.KindBool},
	FieldCutShort:       {kind: slog.KindBool},
	FieldMoment:         {kind: slog.KindString, values: []string{MomentStartup, MomentServe, MomentShutdown, MomentMigrate, MomentInvite, MomentCountries, MomentSweep, MomentPackages}},
	FieldErrorKind:      {kind: slog.KindLogValuer},
	FieldPanicKind:      {kind: slog.KindLogValuer},
	FieldApplied:        {kind: slog.KindInt64},
	FieldSchemaVersion:  {kind: slog.KindInt64},
	FieldDatabase:       {kind: slog.KindString, values: []string{DatabaseAPI, DatabaseOpenFGA}},
	FieldStoreCreated:   {kind: slog.KindBool},
	FieldModelWritten:   {kind: slog.KindBool},
	FieldMethod:         {kind: slog.KindString, shape: matches(methodShape)},
	FieldCode:           {kind: slog.KindString, values: Codes},
	FieldCause:          {kind: slog.KindString, shape: matches(causeShape)},
	FieldTokenClass:     {kind: slog.KindString, values: TokenClasses},
	FieldDurationMS:     {kind: slog.KindInt64},
	FieldStep:           {kind: slog.KindString, values: Steps},
	FieldDone:           {kind: slog.KindInt64},
	FieldFailed:         {kind: slog.KindInt64},
	FieldFinished:       {kind: slog.KindBool},
	FieldModulesRelease: {kind: slog.KindString, shape: matches(releaseShape)},
	FieldPackages:       {kind: slog.KindInt64},
	FieldFiles:          {kind: slog.KindInt64},
	FieldStatus:         {kind: slog.KindInt64},
	FieldAdmissionTable: {kind: slog.KindString, shape: matches(tableShape)},
	FieldFactsFile:      {kind: slog.KindBool},
	FieldRung:           {kind: slog.KindString, values: Rungs},
	FieldRungSet:        {kind: slog.KindBool},
	FieldPlatformKeys:   {kind: slog.KindInt64},
	FieldOutcomeCause:   {kind: slog.KindString, shape: matches(causeShape)},
	FieldProvider:       {kind: slog.KindString, values: Providers},
	FieldModel:          {kind: slog.KindString, shape: matches(modelShape)},
	FieldInputTokens:    {kind: slog.KindInt64},
	FieldOutputTokens:   {kind: slog.KindInt64},
	FieldThinkingTokens: {kind: slog.KindInt64},
	FieldAIOutcome:      {kind: slog.KindString, values: AIOutcomes},
}

// The shapes of the build's identity, as the justfile's build stamps it: a
// version is "dev", a tag of semver — a prerelease too, v0.2.0-rc.1 — with how
// far past it the build is (git describe), or a commit when there is no tag
// yet; the last two may be dirty.
var (
	versionShape = regexp.MustCompile(`^(dev|v\d+\.\d+\.\d+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?(-\d+-g[0-9a-f]{7,40})?(-dirty)?|[0-9a-f]{7,40}(-dirty)?)$`)
	commitShape  = regexp.MustCompile(`^(unknown|[0-9a-f]{7,40})$`)
	// releaseShape is a tag of a release of modules, as modules.json pins it:
	// the form internal/packages holds it to, with no leading zeros.
	releaseShape = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	// tableShape is the version of the admission table, MAJOR.MINOR.
	tableShape = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
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

// The values of a request's line (R33). A method is one of the contract's,
// /novatrail.api.v1.<Service>/<Method>, and a cause one of its enum Cause; both
// are names of the contract, never a value of a request. rpc's tests hold these
// sets to the contract, to Connect's codes and to authn's classes.
var (
	methodShape = regexp.MustCompile(`^/novatrail\.api\.v1\.[A-Z][A-Za-z]*Service/[A-Z][A-Za-z]*$`)
	causeShape  = regexp.MustCompile(`^CAUSE_[A-Z_]+$`)
)

// Codes is the value set of FieldCode: Connect's codes in Connect's words, and
// ok for a request that was not refused.
var Codes = []string{
	"ok", "canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found",
	"already_exists", "permission_denied", "resource_exhausted", "failed_precondition",
	"aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss",
	"unauthenticated",
}

// TokenClasses is the value set of FieldTokenClass: the classes of a refused
// token, in authn's words (R20, R32).
var TokenClasses = []string{"expired", "invalid", "revoked", "no_user", "disabled", "failed"}

// Rungs is the value set of FieldRung: the stages of tutor's rule of admission
// of models, in tutor's codes (R25). admission's tests hold the set to config's.
var Rungs = []string{"author", "full"}

// Providers is the value set of FieldProvider: the providers of the admission
// table, by their codes (R96 spec). admission's tests hold the set to the table.
var Providers = []string{"anthropic", "google", "openai"}

// AIOutcomes is the value set of FieldAIOutcome: how a call of tutor ended, in
// tutor's kinds (R46).
var AIOutcomes = []string{"answered", "failed", "limit_reached", "not_admitted", "key_refused"}

// modelShape is a model's ID as a provider answers with it: lowercase letters,
// digits, dots, dashes and underscores, as "claude-opus-5-5" — no free text.
var modelShape = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
